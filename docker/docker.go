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
	projectUpdated   chan<- dto.Project
	projects         map[ProjectID]*Project
	containerUpdated chan<- dto.Container
	containers       map[ContainerID]*Container
	containersLock   sync.RWMutex
	logger           slog.Logger
}

func NewDocker(ctx context.Context, projectUpdated chan<- dto.Project, containerUpdated chan<- dto.Container, logger slog.Logger) *Docker {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		os.Exit(1)
	}

	return &Docker{
		client:           cli,
		projectUpdated:   projectUpdated,
		projects:         make(map[ProjectID]*Project),
		containerUpdated: containerUpdated,
		containers:       make(map[ContainerID]*Container),
		logger:           logger,
	}
}

func (d *Docker) Run(ctx context.Context) {
	eg, errCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.collectContainers(errCtx)
	})

	eg.Go(func() error {
		d.handleEvents(errCtx)
		return nil
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

	var wg sync.WaitGroup
	for _, dockerContainer := range dockerContainers {
		projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

		if !isProject {
			continue
		}
		projectID := ProjectID(projectIDraw)

		if _, ok := d.projects[projectID]; !ok {
			d.projects[projectID] = NewProject(
				projectID,
				dockerContainer.Labels["com.docker.compose.project"],
				d.projectUpdated,
				make(chan ContainerValue),
			)

			go d.projects[projectID].HandleContainerValue()
		}

		c := NewContainer(dockerContainer, projectID, d.projects[projectID].GetUpdatedContainerValueCh(), d.containerUpdated, d.logger)
		d.containersLock.Lock()
		d.containers[c.ID] = c
		d.containersLock.Unlock()
		d.projects[projectID].Containers = append(d.projects[projectID].Containers, c)

		wg.Go(func() {
			d.getContainerStats(ctx, c)
		})
	}

	wg.Wait()

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) getContainerStats(ctx context.Context, c *Container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))
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

func (d *Docker) handleEvents(ctx context.Context) {
	f := filters.NewArgs()
	f.Add("type", "container")
	msgs, errs := d.client.Events(ctx, events.ListOptions{Filters: f})

	d.logger.DebugContext(ctx, "handleEvents")

outer:
	for {
		select {
		case msg := <-msgs:
			d.logger.DebugContext(ctx, "event", slog.String("action", string(msg.Action)), slog.String("container_id", msg.Actor.ID))
			d.containersLock.RLock()
			c, ok := d.containers[ContainerID(msg.Actor.ID)]
			d.containersLock.RUnlock()

			if !ok {
				continue
			}

			c.SetRunningStateFromAction(msg.Action)
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "context is done")
			break outer
		case err := <-errs:
			d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
		}
	}
}
