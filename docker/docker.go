package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync"

	apiContainer "github.com/docker/docker/api/types/container"
	"golang.org/x/sync/errgroup"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"

	"github/syrm/c8s/dto"
)

type Docker struct {
	client          *dockerClient.Client
	logger          *slog.Logger
	projectUpdateCh chan<- dto.Project
	projects        map[string]*Project
	containers      map[ContainerID]*Container
}

func NewDocker(ctx context.Context, logger *slog.Logger, projectUpdateCh chan<- dto.Project) *Docker {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		os.Exit(1)
	}

	return &Docker{
		client:          cli,
		logger:          logger,
		projectUpdateCh: projectUpdateCh,
		projects:        make(map[string]*Project),
		containers:      make(map[ContainerID]*Container),
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

	for _, dockerContainer := range dockerContainers {
		projectID, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

		if !isProject {
			continue
		}

		p := NewProject(
			projectID,
			dockerContainer.Labels["com.docker.compose.project"],
			d.projectUpdateCh,
			d.logger,
		)

		go p.HandleContainerValue()

		c := NewContainer(dockerContainer, projectID, p.GetContainerValueCh(), d.logger)

		d.containers[c.ID] = c

		if _, ok := d.projects[projectID]; !ok {
			d.projects[projectID] = p
		}
	}

	var wg sync.WaitGroup
	for _, dockerContainer := range d.containers {
		wg.Go(func() {
			d.getContainerStats(ctx, d.projects[dockerContainer.ProjectID], dockerContainer)
		})
	}

	wg.Wait()

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) getContainerStats(ctx context.Context, p *Project, c *Container) {
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
			//d.logger.DebugContext(ctx, "event", slog.String("action", string(msg.Action)), slog.String("container_id", msg.Actor.ID))
			c, ok := d.containers[ContainerID(msg.Actor.ID)]

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
