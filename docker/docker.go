package docker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"os"
	"slices"

	"golang.org/x/sync/errgroup"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"

	"github.com/syrm/c8s/dto"
	"github.com/syrm/c8s/tui"
)

type ContainersCommand struct {
	functor  func(*Docker) *Container
	response chan *Container
}

type Docker struct {
	client            *dockerClient.Client
	containers        map[ContainerID]*Container
	containersCommand chan ContainersCommand
	requestData       <-chan tui.RequestData
	logger            *slog.Logger
}

func NewDocker(
	ctx context.Context,
	requestData <-chan tui.RequestData,
	logger *slog.Logger,
) *Docker {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		os.Exit(1)
	}

	return &Docker{
		client:            cli,
		containers:        make(map[ContainerID]*Container, 256),
		containersCommand: make(chan ContainersCommand),
		requestData:       requestData,
		logger:            logger,
	}
}

func (d *Docker) Run(ctx context.Context) {
	eg, errCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.handleContainersCommand(errCtx)
	})

	eg.Go(func() error {
		d.handleEvents(errCtx)
		return nil
	})

	eg.Go(func() error {
		return d.collectContainers(errCtx)
	})

	eg.Go(func() error {
		d.handleRequests(errCtx)
		return nil
	})

	if err := eg.Wait(); err != nil {
		d.logger.ErrorContext(errCtx, "error in Docker Run", slog.Any("error", err))
	}
}

func (d *Docker) handleRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleRequests context is done")
			return
		case req := <-d.requestData:
			switch r := req.(type) {

			case *tui.RequestProject:
				d.handleRequestProject(r)

			case *tui.RequestProjectList:
				d.handleRequestProjectList(r)
			}
		}
	}
}

func (d *Docker) handleRequestProject(r *tui.RequestProject) {
	var containers []dto.Container

	for _, c := range d.containers {
		if r.ProjectID == c.Project.ID {
			response := make(chan ContainerResponse)
			c.Command <- ContainerCommand{
				response: response,
			}

			container := <-response

			containers = append(containers, dto.Container{
				ID:               dto.ContainerID(container.ID),
				Project:          container.Project,
				Service:          container.Service,
				Name:             container.Name,
				CPUPercentage:    container.CPUPercentage,
				MemoryPercentage: container.MemoryPercentage,
				IsRunning:        container.IsRunning,
			})
		}
	}

	r.Response <- containers
}

func (d *Docker) handleRequestProjectList(r *tui.RequestProjectList) {
	d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			projects := make(map[dto.ProjectID]dto.Project)

			for _, c := range d.containers {
				response := make(chan ContainerResponse)
				c.Command <- ContainerCommand{
					response: response,
				}

				container := <-response

				// We should copy the data to avoid data race
				projectID := dto.ProjectID(container.Project.ID)

				project, projectExist := projects[projectID]

				if !projectExist {
					project = dto.Project{
						ID:               projectID,
						Name:             container.Project.Name,
						ContainersCPU:    make(map[dto.ContainerID]float64),
						ContainersMemory: make(map[dto.ContainerID]float64),
						ContainersState:  make(map[dto.ContainerID]bool),
					}
				}

				project.CPUPercentage += container.CPUPercentage
				project.ContainersCPU[dto.ContainerID(container.ID)] = container.CPUPercentage
				project.MemoryPercentage += container.MemoryPercentage
				project.ContainersMemory[dto.ContainerID(container.ID)] = container.MemoryPercentage

				isRunning := 0
				if container.IsRunning {
					isRunning = 1
				}

				project.ContainersRunning += isRunning
				project.ContainersState[dto.ContainerID(container.ID)] = container.IsRunning

				projects[projectID] = project
			}

			r.Response <- slices.Collect(maps.Values(projects))

			return nil
		},
	}
}

func (d *Docker) collectContainers(ctx context.Context) error {
	dockerContainers, err := d.client.ContainerList(ctx, apiContainer.ListOptions{All: true})
	if err != nil {
		return err
	}

	d.logger.DebugContext(ctx, "CollectContainers started")

	for _, dockerContainer := range dockerContainers {
		d.createContainer(ctx, dockerContainer)
	}

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) createContainer(ctx context.Context, dockerContainer apiContainer.Summary) {
	projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

	if !isProject {
		return
	}

	project := dto.ContainerProject{
		ID:   dto.ProjectID(projectIDraw),
		Name: dockerContainer.Labels["com.docker.compose.project"],
	}

	response := make(chan *Container)
	d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[ContainerID(dockerContainer.ID)]
		},
		response: response,
	}

	c := <-response
	if c != nil {
		// Container already exists
		return
	}

	c = NewContainer(ctx, dockerContainer, project, d.logger)

	d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			docker.containers[c.ID] = c

			return nil
		},
	}

	go d.getContainerStatsRealtime(ctx, c)
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))

		d.containersCommand <- ContainersCommand{
			functor: func(docker *Docker) *Container {
				delete(docker.containers, c.ID)
				c.Delete()

				return nil
			},
		}

		return
	}

	defer dockerContainerStats.Body.Close()

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse
		errDecode := dec.Decode(&stats)
		if errDecode != nil {
			if errDecode != io.EOF && !errors.Is(errDecode, context.DeadlineExceeded) {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
				break
			}

			d.logger.InfoContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			break
		}

		s := stats
		c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Update(s)
			},
		}
	}
}

func (d *Docker) handleContainersCommand(ctx context.Context) error {
	// @TODO a tester
	defer func() {
		if r := recover(); r != nil {
			d.logger.ErrorContext(ctx, "panic in handleContainersCommand", slog.Any("recover", r))
		}
	}()

	for {
		select {
		case cmd := <-d.containersCommand:
			var c *Container
			if cmd.functor != nil {
				c = cmd.functor(d)
			}

			if cmd.response != nil {
				cmd.response <- c
			}
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleContainersCommand context is done")
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

			response := make(chan *Container)
			d.containersCommand <- ContainersCommand{
				functor: func(docker *Docker) *Container {
					return docker.containers[ContainerID(msg.Actor.ID)]
				},
				response: response,
			}

			c := <-response

			if c != nil {
				c.Command <- ContainerCommand{
					functor: func(container *Container) {
						container.SetRunningStateFromAction(msg.Action)
					},
				}

				if msg.Action == events.ActionDestroy {
					d.containersCommand <- ContainersCommand{
						functor: func(docker *Docker) *Container {
							delete(docker.containers, c.ID)
							c.Delete()

							return nil
						},
					}
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
