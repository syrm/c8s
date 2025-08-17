package docker

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github/syrm/c8s/dto"
	"github/syrm/c8s/tui"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
)

type Docker struct {
	client     *dockerClient.Client
	logger     slog.Logger
	projectMsg chan<- dto.Project
	projects   map[string]*project
	containers map[string]*container
	tui        tui.Tui
}

func NewDocker(ctx context.Context, logger slog.Logger, projectMsg chan<- dto.Project) *Docker {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		os.Exit(1)
	}

	return &Docker{
		client:     cli,
		logger:     logger,
		projectMsg: projectMsg,
		projects:   make(map[string]*project),
		containers: make(map[string]*container),
	}
}

func (d *Docker) Run(ctx context.Context) {
	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.collectContainers(ctx)
	})

	eg.Go(func() error {
		d.handleEvents(ctx)
		return nil
	})

	if err := eg.Wait(); err != nil {
		d.logger.ErrorContext(ctx, "error in Docker Run", slog.Any("error", err))
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
		projectID, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

		if !isProject {
			continue
		}

		c := NewContainer(dockerContainer, projectID, d.logger)
		d.containers[c.ID] = c

		if _, ok := d.projects[projectID]; !ok {
			d.projects[projectID] = NewProject(
				dockerContainer.Labels["com.docker.compose.project.working_dir"],
				dockerContainer.Labels["com.docker.compose.project"],
			)
		}
		d.projects[projectID].Containers = append(d.projects[projectID].Containers, c)

		wg.Go(func() {
			d.getContainerStats(ctx, d.projects[projectID], c)
		})
	}

	wg.Wait()

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) getContainerStats(ctx context.Context, p *project, c *container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, c.ID, true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", c.ID), slog.Any("error", err))
		return
	}

	defer dockerContainerStats.Body.Close()

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse
		err := dec.Decode(&stats)
		if err != nil {
			if err != io.EOF && err != context.DeadlineExceeded {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", c.ID), slog.Any("error", err))
				break
			}

			d.logger.InfoContext(ctx, "end of container stats", slog.String("container_id", c.ID), slog.Any("error", err))
			break
		}

		c.Update(stats)
		project := d.projects[c.ProjectID]
		var projectCPUPercentage float64
		var projectMemoryPercentage float64
		var containersRunning int

		for _, c := range project.Containers {
			projectCPUPercentage += c.CPUPercentage
			projectMemoryPercentage += c.MemoryPercentage

			if c.IsRunning {
				containersRunning += 1
			}
		}

		p := dto.Project{
			ID:                    dto.ProjectID(project.ID),
			Name:                  project.Name,
			CPUPercentage:         projectCPUPercentage,
			MemoryUsagePercentage: projectMemoryPercentage,
			ContainersRunning:     containersRunning,
			ContainersTotal:       len(project.Containers),
		}
		d.projectMsg <- p
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
			c, ok := d.containers[msg.Actor.ID]

			if !ok {
				continue
			}

			c.SetRunninStateFromAction(msg.Action)
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "context is done")
			break outer
		case err := <-errs:
			d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
		}
	}
}
