package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/syrm/c8s/dto"
)

type ContainersCommand struct {
	functor  func(*Docker) *Container
	response chan *Container
}

type Docker struct {
	client            *dockerClient.Client
	containers        map[ContainerID]*Container
	containersCommand chan ContainersCommand
	requestData       <-chan dto.RequestData
	logger            *slog.Logger
	done              chan struct{} // Signals when Run() has completed
}

func NewDocker(
	ctx context.Context,
	requestData <-chan dto.RequestData,
	logger *slog.Logger,
) (*Docker, error) {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		logger.ErrorContext(ctx, "error creating docker client", slog.Any("error", err))
		return nil, err
	}

	return &Docker{
		client:            cli,
		// Pre-allocate map for typical Docker Compose setups (usually < 256 containers)
		containers:        make(map[ContainerID]*Container, 256),
		containersCommand: make(chan ContainersCommand),
		requestData:       requestData,
		logger:            logger,
		done:              make(chan struct{}),
	}, nil
}

// Close closes the Docker client and releases resources.
func (d *Docker) Close() error {
	return d.client.Close()
}

func (d *Docker) Run(ctx context.Context) {
	defer close(d.done) // Signal completion when Run exits

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

// Wait blocks until Run() has completed.
func (d *Docker) Wait() {
	<-d.done
}

func (d *Docker) handleRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleRequests context is done")
			return
		case req, ok := <-d.requestData:
			if !ok {
				// Channel closed, exit gracefully
				d.logger.DebugContext(ctx, "handleRequests channel closed")
				return
			}
			switch r := req.(type) {

			case *dto.RequestContainerLog:
				d.handleRequestContainerLog(ctx, r)

			case *dto.RequestProject:
				d.handleRequestContainerProject(r)

			case *dto.RequestSetPendingAction:
				d.handleRequestSetPendingAction(r)

			case *dto.RequestProjectList:
				d.handleRequestProjectList(r)
			}
		}
	}
}

func (d *Docker) handleRequestContainerLog(ctx context.Context, r *dto.RequestContainerLog) {
	// Get container via containersCommand
	response := make(chan *Container)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[ContainerID(r.ContainerID)]
		},
		response: response,
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in handleRequestContainerLog")
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		r.Response <- dto.Container{}
		return
	}

	var c *Container
	select {
	case c = <-response:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout waiting for response in handleRequestContainerLog")
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		r.Response <- dto.Container{}
		return
	}

	if c == nil {
		r.Response <- dto.Container{}
		return
	}

	// Container exists - create cancel context for logs
	ctxLog, cancel := context.WithCancel(ctx)

	// Atomically: check LogCollectionActive, start collection if needed, build DTO with logs
	// All done inside the functor to avoid data races
	dtoResponse := make(chan dto.Container, 1)
	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			// Start log collection if not active (synchronized access)
			if !container.LogCollectionActive {
				container.LogCollectionActive = true
				go d.collectContainerLogs(ctxLog, container)
			}

			// Build DTO with logs copy (synchronized access to Logs)
			dtoContainer := dto.Container{
				ID:               dto.ContainerID(container.ID),
				Project:          container.Project,
				Service:          container.Service,
				Name:             container.Name,
				CPUPercentage:    container.CPUPercentage,
				MemoryPercentage: container.MemoryPercentage,
				Status:           container.Status,
				PendingAction:    container.PendingAction,
				Logs:             make([]string, len(container.Logs)),
				LogCancel:        cancel,
			}
			copy(dtoContainer.Logs, container.Logs)
			dtoResponse <- dtoContainer
		},
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to container command in handleRequestContainerLog")
		cancel()
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		cancel()
		r.Response <- dto.Container{}
		return
	}

	// Wait for DTO and send response
	select {
	case dtoContainer := <-dtoResponse:
		r.Response <- dtoContainer
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout waiting for dto in handleRequestContainerLog")
		cancel()
		r.Response <- dto.Container{}
	case <-ctx.Done():
		cancel()
		r.Response <- dto.Container{}
	}
}

func (d *Docker) handleRequestContainerProject(r *dto.RequestProject) {
	var containers []dto.Container

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			for _, c := range docker.containers {
				if r.ProjectID == c.Project.ID {
					response := make(chan ContainerResponse)
					select {
					case c.Command <- ContainerCommand{
						response: response,
					}:
						select {
						case container := <-response:
							containers = append(containers, containerResponseToDTO(container))
						case <-time.After(channelTimeout):
							// Skip this container if timeout
						}
					case <-time.After(channelTimeout):
						// Skip this container if timeout
					}
				}
			}
			r.Response <- containers
			return nil
		},
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in handleRequestContainerProject")
		r.Response <- containers
	}
}

func (d *Docker) handleRequestSetPendingAction(r *dto.RequestSetPendingAction) {
	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			c, ok := docker.containers[ContainerID(r.ContainerID)]
			if !ok {
				r.Response <- false
				return nil
			}

			select {
			case c.Command <- ContainerCommand{
				functor: func(container *Container) {
					container.SetPendingAction(r.PendingAction)
				},
			}:
				r.Response <- true
			case <-time.After(channelTimeout):
				r.Response <- false
			}
			return nil
		},
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in handleRequestSetPendingAction")
		r.Response <- false
	}
}

func (d *Docker) handleRequestProjectList(r *dto.RequestProjectList) {
	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			projects := make(map[dto.ProjectID]dto.Project)

			for _, c := range docker.containers {
				response := make(chan ContainerResponse)
				select {
				case c.Command <- ContainerCommand{
					response: response,
				}:
				case <-time.After(channelTimeout):
					// Skip this container if timeout
					continue
				}

				var container ContainerResponse
				select {
				case container = <-response:
				case <-time.After(channelTimeout):
					// Skip this container if timeout
					continue
				}

				// We should copy the data to avoid data race
				projectID := dto.ProjectID(container.Project.ID)

				project, projectExist := projects[projectID]

				if !projectExist {
					project = dto.Project{
						ID:               projectID,
						Name:             container.Project.Name,
						ContainersCPU:    make(map[dto.ContainerID]float64),
						ContainersMemory: make(map[dto.ContainerID]float64),
						ContainersState:  make(map[dto.ContainerID]string),
					}
				}

				project.CPUPercentage += container.CPUPercentage
				project.ContainersCPU[dto.ContainerID(container.ID)] = container.CPUPercentage
				project.MemoryPercentage += container.MemoryPercentage
				project.ContainersMemory[dto.ContainerID(container.ID)] = container.MemoryPercentage

				isRunning := 0
				if container.Status == "running" {
					isRunning = 1
				}

				project.ContainersRunning += isRunning
				project.ContainersState[dto.ContainerID(container.ID)] = container.Status

				projects[projectID] = project
			}

			r.Response <- slices.Collect(maps.Values(projects))

			return nil
		},
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in handleRequestProjectList")
		r.Response <- nil
	}
}

func (d *Docker) collectContainerLogs(ctx context.Context, c *Container) {
	if c == nil {
		return
	}
	defer func() {
		// Reset flag when collection ends
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.LogCollectionActive = false
			},
		}:
		case <-time.After(time.Second):
			// Timeout waiting to reset flag, container might be deleted
		}
	}()

	since := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	out, err := d.client.ContainerLogs(ctx, string(c.ID), apiContainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Since:      since,
	})
	if err != nil {
		d.logger.ErrorContext(ctx, "container logs failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))
		return
	}

	defer out.Close()

	// Use a pipe to demultiplex the Docker log stream
	pr, pw := io.Pipe()

	// Channel to receive lines from the reader goroutine
	lines := make(chan string, 100)

	// Goroutine to handle stdcopy
	go func() {
		defer pw.Close()
		_, err := stdcopy.StdCopy(pw, pw, out)
		if err != nil && err != io.EOF {
			d.logger.DebugContext(ctx, "stdcopy finished", slog.String("container_id", string(c.ID)), slog.Any("error", err))
		}
	}()

	// Goroutine to read lines (this is the blocking I/O)
	go func() {
		defer close(lines)
		reader := bufio.NewReader(pr)
		for {
			line, errReader := reader.ReadString('\n')
			if errReader != nil {
				if errReader != io.EOF {
					d.logger.DebugContext(ctx, "end of container logs", slog.String("container_id", string(c.ID)), slog.Any("error", errReader))
				}
				return
			}
			select {
			case lines <- line:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Main loop: receive lines or handle context cancellation
	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "collectContainerLogs context is done", slog.String("container_id", string(c.ID)))
			out.Close() // Unblock stdcopy goroutine
			pr.Close()  // Unblock reader goroutine
			return
		case line, ok := <-lines:
			if !ok {
				// Channel closed, reader finished
				return
			}
			// Use select with timeout to avoid blocking forever on container command
			select {
			case c.Command <- ContainerCommand{
				functor: func(container *Container) {
					container.AppendLog(line)
				},
			}:
			case <-time.After(time.Second):
				// Timeout sending log, container might be busy or deleted
			case <-ctx.Done():
				out.Close()
				pr.Close()
				return
			}
		}
	}
}

// dockerAPITimeout is the timeout for one-shot Docker API calls
const dockerAPITimeout = 30 * time.Second

// channelTimeout is the timeout for channel operations to prevent deadlock
const channelTimeout = 5 * time.Second

func (d *Docker) collectContainers(ctx context.Context) error {
	listCtx, cancel := context.WithTimeout(ctx, dockerAPITimeout)
	defer cancel()

	dockerContainers, err := d.client.ContainerList(listCtx, apiContainer.ListOptions{All: true})
	if err != nil {
		return err
	}

	d.logger.DebugContext(ctx, "CollectContainers started")

	for _, dockerContainer := range dockerContainers {
		d.createContainer(ctx, dockerContainer, events.ActionCreate)
	}

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

func (d *Docker) createContainer(ctx context.Context, dockerContainer apiContainer.Summary, action events.Action) {
	projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

	if !isProject {
		return
	}

	project := dto.ContainerProject{
		ID:   dto.ProjectID(projectIDraw),
		Name: dockerContainer.Labels["com.docker.compose.project"],
	}

	response := make(chan *Container)
	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[ContainerID(dockerContainer.ID)]
		},
		response: response,
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in createContainer (check)")
		return
	case <-ctx.Done():
		return
	}

	var c *Container
	select {
	case c = <-response:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout waiting for response in createContainer (check)")
		return
	case <-ctx.Done():
		return
	}

	if c != nil {
		// Container already exists
		return
	}

	c = NewContainer(ctx, dockerContainer, action, project, d.logger)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			docker.containers[c.ID] = c
			return nil
		},
	}:
	case <-time.After(channelTimeout):
		d.logger.Warn("timeout sending to containersCommand in createContainer (insert)")
		return
	case <-ctx.Done():
		return
	}

	go d.getContainerStatsRealtime(ctx, c)
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))

		// Mark container as exited instead of deleting
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = "exited"
			},
		}:
		case <-time.After(channelTimeout):
			d.logger.Warn("timeout sending exited status in getContainerStatsRealtime")
		case <-ctx.Done():
		}

		return
	}

	defer dockerContainerStats.Body.Close()
	defer func() {
		// Mark container as exited when stats streaming ends
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = "exited"
			},
		}:
		case <-time.After(channelTimeout):
			d.logger.Warn("timeout sending exited status in getContainerStatsRealtime defer")
		}
		d.logger.DebugContext(ctx, "container stopped (stats ended)", slog.String("container_id", string(c.ID)))
	}()

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
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Update(s)
			},
		}:
		case <-time.After(channelTimeout):
			// Skip this stats update if timeout
		case <-ctx.Done():
			return
		}
	}
}

func (d *Docker) handleContainersCommand(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			d.logger.ErrorContext(ctx, "panic in handleContainersCommand", slog.Any("recover", r))
			// Convert panic to error to signal critical failure
			err = fmt.Errorf("panic in handleContainersCommand: %v", r)
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
			select {
			case d.containersCommand <- ContainersCommand{
				functor: func(docker *Docker) *Container {
					return docker.containers[ContainerID(msg.Actor.ID)]
				},
				response: response,
			}:
			case <-time.After(channelTimeout):
				d.logger.Warn("timeout sending to containersCommand in handleEvents")
				continue
			case <-ctx.Done():
				return
			}

			var c *Container
			select {
			case c = <-response:
			case <-time.After(channelTimeout):
				d.logger.Warn("timeout waiting for response in handleEvents")
				continue
			case <-ctx.Done():
				return
			}

			if c != nil {
				select {
				case c.Command <- ContainerCommand{
					functor: func(container *Container) {
						container.SetStatusFromAction(msg.Action)
					},
				}:
				case <-time.After(channelTimeout):
					d.logger.Warn("timeout sending status update in handleEvents")
				case <-ctx.Done():
					return
				}

				if msg.Action == events.ActionDestroy {
					select {
					case d.containersCommand <- ContainersCommand{
						functor: func(docker *Docker) *Container {
							delete(docker.containers, c.ID)
							c.Delete()
							return nil
						},
					}:
					case <-time.After(channelTimeout):
						d.logger.Warn("timeout sending destroy command in handleEvents")
					case <-ctx.Done():
						return
					}
				}

				// Restart stats streaming when container starts
				if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
					go d.getContainerStatsRealtime(ctx, c)
				}
				continue
			}

			d.createContainer(
				ctx,
				apiContainer.Summary{
					ID:     msg.Actor.ID,
					Names:  []string{msg.Actor.Attributes["name"]},
					Labels: msg.Actor.Attributes,
				},
				msg.Action,
			)

		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleEvents context is done")
			return

		case err := <-errs:
			if err != nil {
				d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
			}
		}
	}
}
