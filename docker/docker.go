package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"

	"github.com/syrm/c8s/dto"
)

type Docker struct {
	client           *dockerClient.Client
	containerUpdated chan<- dto.ContainerDeletable
	containers       map[ContainerID]*Container
	containersLock   sync.RWMutex
	newContainer     chan apiContainer.Summary
	logger           *slog.Logger
}

func NewDocker(ctx context.Context, containerUpdated chan<- dto.ContainerDeletable, logger *slog.Logger) *Docker {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		os.Exit(1)
	}

	return &Docker{
		client:           cli,
		containerUpdated: containerUpdated,
		containers:       make(map[ContainerID]*Container, 256),
		newContainer:     make(chan apiContainer.Summary, 256),
		logger:           logger,
	}
}

func (d *Docker) Run(ctx context.Context) {
	eg, errCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		d.handleEvents(errCtx)
		return nil
	})

	eg.Go(func() error {
		return d.collectContainers(errCtx)
	})

	eg.Go(func() error {
		return d.handleContainerStats(errCtx)
	})

	if err := eg.Wait(); err != nil {
		d.logger.ErrorContext(errCtx, "error in Docker Run", slog.Any("error", err))
	}
}

func (d *Docker) collectContainers(ctx context.Context) error {
	dockerContainers, err := d.client.ContainerList(ctx, apiContainer.ListOptions{All: true})
	if err != nil {
		return err
	}

	d.logger.DebugContext(ctx, "CollectContainers started")

	for _, dockerContainer := range dockerContainers {
		d.newContainer <- dockerContainer
	}

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) createContainer(ctx context.Context, dockerContainer apiContainer.Summary) {
	projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

	if !isProject {
		return
	}

	project := dto.Project{
		ID:   dto.ProjectID(projectIDraw),
		Name: dockerContainer.Labels["com.docker.compose.project"],
	}

	d.containersLock.Lock()
	// Check if container was already created by another event
	if _, exists := d.containers[ContainerID(dockerContainer.ID)]; exists {
		d.containersLock.Unlock()
		return
	}
	c := NewContainer(dockerContainer, project, d.containerUpdated, d.logger)
	d.containers[c.ID] = c
	d.containersLock.Unlock()

	go d.getContainerStatsRealtime(ctx, c)
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))

		d.containersLock.Lock()
		delete(d.containers, c.ID)
		d.containersLock.Unlock()

		c.tryUnpublish()

		return
	}

	defer dockerContainerStats.Body.Close()

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse
		err := dec.Decode(&stats)
		if err != nil {
			if err != io.EOF && !errors.Is(err, context.DeadlineExceeded) {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", err))
				break
			}

			d.logger.InfoContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", err))
			break
		}

		c.Update(stats)
	}
}

func (d *Docker) handleContainerStats(ctx context.Context) error {
	for {
		select {
		case dockerContainer := <-d.newContainer:
			d.createContainer(ctx, dockerContainer)
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleContainerStats context is done")
			return nil
		}
	}
}

func (d *Docker) handleEvents(ctx context.Context) {
	f := filters.NewArgs()
	f.Add("type", "container")
	msgs, errs := d.client.Events(ctx, events.ListOptions{Filters: f})

	d.logger.DebugContext(ctx, "handleEvents")

	for {
		select {
		case msg := <-msgs:
			d.logger.DebugContext(ctx, "event", slog.String("action", string(msg.Action)), slog.String("container_id", msg.Actor.ID))
			d.containersLock.RLock()
			c, ok := d.containers[ContainerID(msg.Actor.ID)]
			d.containersLock.RUnlock()

			if ok {
				c.SetRunningStateFromAction(msg.Action)

				if msg.Action == events.ActionDestroy {
					d.containersLock.Lock()
					delete(d.containers, ContainerID(msg.Actor.ID))
					d.containersLock.Unlock()

					c.tryUnpublish()
				}
				continue
			}

			state := apiContainer.StateExited

			if IsRunningFromAction(msg.Action) {
				state = apiContainer.StateRunning
			}

			d.createContainer(ctx, apiContainer.Summary{
				ID:     msg.Actor.ID,
				Names:  []string{msg.Actor.Attributes["name"]},
				Labels: msg.Actor.Attributes,
				State:  state,
			})

		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleEvents context is done")
			return

		case err := <-errs:
			d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
		}
	}
}
