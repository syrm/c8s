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
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/syrm/c8s/dto"
)

// ContainersCommand represents a command to be executed on the containers map.
// It uses a functor pattern to serialize access to the containers.
type ContainersCommand struct {
	functor  func(*Docker) *Container
	response chan *Container
}

// Docker manages the connection to the Docker daemon and container monitoring.
// It provides real-time statistics and log streaming for Docker Compose projects.
type Docker struct {
	client            *dockerClient.Client
	containers        map[dto.ContainerID]*Container
	containersCommand chan ContainersCommand
	requestData       <-chan dto.RequestData
	logger            *slog.Logger
	done              chan struct{} // Signals when Run() has completed
	// statsContexts tracks active stats goroutines to prevent leaks
	// Key: container ID, Value: cancel function for the stats goroutine
	statsContexts     map[dto.ContainerID]context.CancelFunc
	statsContextsLock sync.Mutex
	// logContexts tracks active log collection contexts to allow cancellation
	// Key: container ID, Value: cancel function for the log collection
	logContexts       map[dto.ContainerID]context.CancelFunc
	logContextsLock   sync.Mutex
}

// NewDocker creates a new Docker client and initializes monitoring infrastructure.
// It returns an error if the Docker client cannot be created.
func NewDocker(
	ctx context.Context,
	requestData <-chan dto.RequestData,
	logger *slog.Logger,
) (*Docker, error) {
	cli, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &Docker{
		client:            cli,
		// Pre-allocate map for typical Docker Compose setups
		containers:        make(map[dto.ContainerID]*Container, initialContainerMapSize),
		containersCommand: make(chan ContainersCommand, 16), // Buffered to prevent blocking senders
		requestData:       requestData,
		logger:            logger,
		done:              make(chan struct{}),
		statsContexts:     make(map[dto.ContainerID]context.CancelFunc),
		logContexts:       make(map[dto.ContainerID]context.CancelFunc),
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
				d.handleRequestContainerProject(ctx, r)

			case *dto.RequestSetPendingAction:
				d.handleRequestSetPendingAction(ctx, r)

			case *dto.RequestProjectList:
				d.handleRequestProjectList(ctx, r)

			case *dto.RequestStopLogCollection:
				d.handleRequestStopLogCollection(ctx, r)

			default:
				d.logger.Warn("unknown request type", slog.String("type", fmt.Sprintf("%T", req)))
			}
		}
	}
}

func (d *Docker) handleRequestContainerLog(ctx context.Context, r *dto.RequestContainerLog) {
	// Get container via containersCommand
	response := make(chan *Container, 1)
	timer1 := time.NewTimer(dto.ChannelTimeout)
	defer timer1.Stop()

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[r.ContainerID]
		},
		response: response,
	}:
	case <-timer1.C:
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
	case <-timer1.C:
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

	// Container exists - create or get cancel context for logs
	// First, cancel any existing log collection for this container
	d.logContextsLock.Lock()
	if oldCancel, exists := d.logContexts[c.ID]; exists {
		oldCancel()
		delete(d.logContexts, c.ID)
	}
	// Create new log context
	ctxLog, cancel := context.WithCancel(ctx)
	d.logContexts[c.ID] = cancel
	d.logContextsLock.Unlock()

	// Atomically: check LogCollectionActive, start collection if needed, build DTO with logs
	// All done inside the functor which is executed by handleCommands (serialized access)
	dtoResponse := make(chan dto.Container, 1)
	timer2 := time.NewTimer(dto.ChannelTimeout)
	defer timer2.Stop()

	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			if container.deleted.Load() {
				dtoResponse <- dto.Container{}
				return
			}

			// Start log collection if not active (synchronized access via handleCommands)
			if !container.LogCollectionActive {
				container.LogCollectionActive = true
				go d.collectContainerLogs(ctxLog, container)
			}

			// Build DTO with logs copy
			// No lock needed because handleCommands serializes all access
			dtoContainer := dto.Container{
				ID:               container.ID,
				Project:          container.Project,
				Service:          container.Service,
				Name:             container.Name,
				CPUPercentage:    container.CPUPercentage,
				MemoryPercentage: container.MemoryPercentage,
				Status:           container.Status,
				PendingAction:    container.PendingAction,
				Logs:             make([]string, len(container.Logs)),
			}
			copy(dtoContainer.Logs, container.Logs)
			dtoResponse <- dtoContainer
		},
	}:
	case <-timer2.C:
		d.logger.Warn("timeout sending to container command in handleRequestContainerLog")
		d.logContextsLock.Lock()
		delete(d.logContexts, c.ID)
		d.logContextsLock.Unlock()
		cancel()
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		d.logContextsLock.Lock()
		delete(d.logContexts, c.ID)
		d.logContextsLock.Unlock()
		cancel()
		r.Response <- dto.Container{}
		return
	}

	// Wait for DTO and send response
	timer3 := time.NewTimer(dto.ChannelTimeout)
	defer timer3.Stop()
	select {
	case dtoContainer := <-dtoResponse:
		r.Response <- dtoContainer
	case <-timer3.C:
		d.logger.Warn("timeout waiting for dto in handleRequestContainerLog")
		cancel()
		r.Response <- dto.Container{}
	case <-ctx.Done():
		cancel()
		r.Response <- dto.Container{}
	}
}

func (d *Docker) handleRequestContainerProject(ctx context.Context, r *dto.RequestProject) {
	// Use a channel to collect results from the functor to avoid data race
	resultsChan := make(chan dto.Container, 10) // Buffered to prevent blocking
	timer1 := time.NewTimer(dto.ChannelTimeout)
	defer timer1.Stop()

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			for _, c := range docker.containers {
				if r.ProjectID == c.Project.ID {
					response := make(chan ContainerResponse, 1) // Buffered!
					timer2 := time.NewTimer(dto.ChannelTimeout)
					timer3 := time.NewTimer(dto.ChannelTimeout)
					timer2.Stop()
					timer3.Stop()
					select {
					case c.Command <- ContainerCommand{
						response: response,
					}:
						timer2.Reset(dto.ChannelTimeout)
						select {
						case container := <-response:
							resultsChan <- containerResponseToDTO(container)
						case <-timer2.C:
							// Skip this container if timeout
						}
					case <-timer3.C:
						// Skip this container if timeout
					}
					timer2.Stop()
					timer3.Stop()
				}
			}
			close(resultsChan)
			return nil
		},
	}:
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestContainerProject")
		close(resultsChan)
		r.Response <- nil
		return
	case <-ctx.Done():
		close(resultsChan)
		r.Response <- nil
		return
	}

	// Collect results from the channel
	var containers []dto.Container
	for container := range resultsChan {
		containers = append(containers, container)
	}
	r.Response <- containers
}

func (d *Docker) handleRequestSetPendingAction(ctx context.Context, r *dto.RequestSetPendingAction) {
	timer1 := time.NewTimer(dto.ChannelTimeout)
	timer2 := time.NewTimer(dto.ChannelTimeout)
	defer timer1.Stop()
	defer timer2.Stop()

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			c, ok := docker.containers[r.ContainerID]
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
			case <-timer2.C:
				r.Response <- false
			}
			return nil
		},
	}:
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestSetPendingAction")
		r.Response <- false
	case <-ctx.Done():
		r.Response <- false
	}
}

func (d *Docker) handleRequestProjectList(ctx context.Context, r *dto.RequestProjectList) {
	timer1 := time.NewTimer(dto.ChannelTimeout)
	defer timer1.Stop()

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			projects := make(map[dto.ProjectID]dto.Project)

			for _, c := range docker.containers {
				response := make(chan ContainerResponse, 1) // Buffered!
				timer2 := time.NewTimer(dto.ChannelTimeout)
				timer3 := time.NewTimer(dto.ChannelTimeout)
				defer timer2.Stop()
				defer timer3.Stop()

				select {
				case c.Command <- ContainerCommand{
					response: response,
				}:
				case <-timer2.C:
					// Skip this container if timeout
					continue
				}

				var container ContainerResponse
				select {
				case container = <-response:
				case <-timer3.C:
					// Skip this container if timeout
					continue
				}

				// We should copy the data to avoid data race
				projectID := container.Project.ID

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
				project.ContainersCPU[container.ID] = container.CPUPercentage
				project.MemoryPercentage += container.MemoryPercentage
				project.ContainersMemory[container.ID] = container.MemoryPercentage

				isRunning := 0
				if container.Status == StatusRunning {
					isRunning = 1
				}

				project.ContainersRunning += isRunning
				project.ContainersState[container.ID] = container.Status

				projects[projectID] = project
			}

			r.Response <- slices.Collect(maps.Values(projects))

			return nil
		},
	}:
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestProjectList")
		r.Response <- nil
	case <-ctx.Done():
		r.Response <- nil
	}
}

func (d *Docker) handleRequestStopLogCollection(ctx context.Context, r *dto.RequestStopLogCollection) {
	d.logContextsLock.Lock()
	defer d.logContextsLock.Unlock()

	if cancel, exists := d.logContexts[r.ContainerID]; exists {
		cancel()
		delete(d.logContexts, r.ContainerID)
		d.logger.DebugContext(ctx, "stopped log collection", slog.String("container_id", string(r.ContainerID)))
	}
}

func (d *Docker) collectContainerLogs(ctx context.Context, c *Container) {
	if c == nil {
		return
	}
	defer func() {
		// Reset flag when collection ends
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.LogCollectionActive = false
			},
		}:
		case <-timer.C:
			// Timeout waiting to reset flag, container might be deleted
		}
	}()

	since := time.Now().Add(-logHistoryDuration).Format(time.RFC3339)
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

	// Use a pipe to demultiplex the Docker log stream
	pr, pw := io.Pipe()

	// Track cleanup to avoid double close
	var outClosed, prClosed bool
	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			if !outClosed {
				out.Close()
				outClosed = true
			}
			if !prClosed {
				pr.Close()
				prClosed = true
			}
		})
	}

	// Channel to receive lines from the reader goroutine
	lines := make(chan string, logLineBufferSize)

	// Use errgroup to track all goroutines
	g, ctx := errgroup.WithContext(ctx)

	// Goroutine 1: Handle stdcopy
	g.Go(func() error {
		defer pw.Close()
		defer closeResources()
		_, err := stdcopy.StdCopy(pw, pw, out)
		if err != nil && !errors.Is(err, io.EOF) {
			d.logger.DebugContext(ctx, "stdcopy finished", slog.String("container_id", string(c.ID)), slog.Any("error", err))
		}
		return nil
	})

	// Goroutine 2: Read lines (this is the blocking I/O)
	g.Go(func() error {
		defer close(lines)
		defer closeResources()
		reader := bufio.NewReader(pr)
		for {
			line, errReader := reader.ReadString('\n')
			if errReader != nil {
				if !errors.Is(errReader, io.EOF) {
					d.logger.DebugContext(ctx, "end of container logs", slog.String("container_id", string(c.ID)), slog.Any("error", errReader))
				}
				return nil
			}
			select {
			case lines <- line:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})

	// Goroutine 3: Main loop - receive lines or handle context cancellation
	g.Go(func() error {
		// Use a reusable timer to prevent resource leaks
		sendTimer := startTimer(time.Second)
		defer stopTimer(sendTimer)

		for {
			select {
			case <-ctx.Done():
				d.logger.DebugContext(ctx, "collectContainerLogs context is done", slog.String("container_id", string(c.ID)))
				closeResources() // Unblock stdcopy and reader goroutines
				return ctx.Err()
			case line, ok := <-lines:
				if !ok {
					// Channel closed, reader finished
					return nil
				}
				// Use select with timeout to avoid blocking forever on container command
				// Reset timer for this iteration
				stopTimer(sendTimer)
				sendTimer.Reset(time.Second)

				select {
				case c.Command <- ContainerCommand{
					functor: func(container *Container) {
						container.AppendLog(line)
					},
				}:
				case <-sendTimer.C:
					// Timeout sending log, container might be busy or deleted
				case <-ctx.Done():
					closeResources()
					return ctx.Err()
				}
			}
		}
	})

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.DebugContext(ctx, "collectContainerLogs errgroup finished", slog.String("container_id", string(c.ID)), slog.Any("error", err))
	}
}

const (
	// dockerAPITimeout is the timeout for one-shot Docker API calls.
	dockerAPITimeout = 30 * time.Second
	// logHistoryDuration is how far back to fetch logs when starting collection.
	logHistoryDuration = 1 * time.Hour
	// logLineBufferSize is the buffer size for the log line channel.
	logLineBufferSize = 100
	// initialContainerMapSize is the initial capacity for the containers map.
	initialContainerMapSize = 256
)

// startTimer creates a new timer with proper cleanup to prevent resource leaks.
// Always call defer stopTimer() on the returned timer.
func startTimer(duration time.Duration) *time.Timer {
	t := time.NewTimer(duration)
	return t
}

// stopTimer stops a timer if it hasn't already fired, preventing resource leaks.
// Safe to call multiple times on the same timer.
func stopTimer(t *time.Timer) {
	if t != nil {
		if !t.Stop() {
			// If the timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.C:
			default:
			}
		}
	}
}

func (d *Docker) collectContainers(ctx context.Context) error {
	listCtx, cancel := context.WithTimeout(ctx, dockerAPITimeout)
	defer cancel()

	dockerContainers, err := d.client.ContainerList(listCtx, apiContainer.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
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

	response := make(chan *Container, 1)
	timer1 := time.NewTimer(dto.ChannelTimeout)
	timer2 := time.NewTimer(dto.ChannelTimeout)
	timer3 := time.NewTimer(dto.ChannelTimeout)
	defer timer1.Stop()
	defer timer2.Stop()
	defer timer3.Stop()

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[dto.ContainerID(dockerContainer.ID)]
		},
		response: response,
	}:
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in createContainer (check)")
		return
	case <-ctx.Done():
		return
	}

	var c *Container
	select {
	case c = <-response:
	case <-timer2.C:
		d.logger.Warn("timeout waiting for response in createContainer (check)")
		return
	case <-ctx.Done():
		return
	}

	if c != nil {
		// Container already exists
		return
	}

	c = NewContainer(ctx, dockerContainer, action, project)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			docker.containers[c.ID] = c
			return nil
		},
	}:
	case <-timer3.C:
		d.logger.Warn("timeout sending to containersCommand in createContainer (insert)")
		return
	case <-ctx.Done():
		return
	}

	// Create a dedicated context for stats collection and track it
	statsCtx, statsCancel := context.WithCancel(ctx)

	d.statsContextsLock.Lock()
	d.statsContexts[c.ID] = statsCancel
	d.statsContextsLock.Unlock()

	go d.getContainerStatsRealtime(statsCtx, c)
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container) {
	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))

		// Mark container as exited instead of deleting
		timer1 := time.NewTimer(dto.ChannelTimeout)
		defer timer1.Stop()
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = StatusExited
			},
		}:
		case <-timer1.C:
			d.logger.Warn("timeout sending exited status in getContainerStatsRealtime")
		case <-ctx.Done():
		}

		return
	}

	defer dockerContainerStats.Body.Close()
	defer func() {
		// Mark container as exited when stats streaming ends
		timer2 := time.NewTimer(dto.ChannelTimeout)
		defer timer2.Stop()
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = StatusExited
			},
		}:
		case <-timer2.C:
			d.logger.Warn("timeout sending exited status in getContainerStatsRealtime defer")
		}
		d.logger.DebugContext(ctx, "container stopped (stats ended)", slog.String("container_id", string(c.ID)))
	}()

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse
		errDecode := dec.Decode(&stats)
		if errDecode != nil {
			if !errors.Is(errDecode, io.EOF) && !errors.Is(errDecode, context.DeadlineExceeded) {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
				break
			}

			d.logger.DebugContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			break
		}

		s := stats
		timer3 := time.NewTimer(dto.ChannelTimeout)
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Update(s)
			},
		}:
		case <-timer3.C:
			// Skip this stats update if timeout
		case <-ctx.Done():
			timer3.Stop()
			return
		}
		timer3.Stop()
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
		case msg, ok := <-msgs:
			if !ok {
				d.logger.DebugContext(ctx, "events channel closed")
				return
			}
			d.logger.DebugContext(ctx, "event", slog.String("action", string(msg.Action)), slog.String("container_id", msg.Actor.ID))

			response := make(chan *Container, 1)
			timer1 := time.NewTimer(dto.ChannelTimeout)
			select {
			case d.containersCommand <- ContainersCommand{
				functor: func(docker *Docker) *Container {
					return docker.containers[dto.ContainerID(msg.Actor.ID)]
				},
				response: response,
			}:
			case <-timer1.C:
				d.logger.Warn("timeout sending to containersCommand in handleEvents")
				timer1.Stop()
				continue
			case <-ctx.Done():
				timer1.Stop()
				return
			}
			timer1.Stop()

			var c *Container
			timer2 := time.NewTimer(dto.ChannelTimeout)
			select {
			case c = <-response:
			case <-timer2.C:
				d.logger.Warn("timeout waiting for response in handleEvents")
				timer2.Stop()
				continue
			case <-ctx.Done():
				timer2.Stop()
				return
			}
			timer2.Stop()

			if c != nil {
				timer3 := time.NewTimer(dto.ChannelTimeout)
				select {
				case c.Command <- ContainerCommand{
					functor: func(container *Container) {
						container.SetStatusFromAction(msg.Action)
					},
				}:
				case <-timer3.C:
					d.logger.Warn("timeout sending status update in handleEvents")
				case <-ctx.Done():
					timer3.Stop()
					return
				}
				timer3.Stop()

				if msg.Action == events.ActionDestroy {
					// Cancel the stats goroutine before deleting the container
					d.statsContextsLock.Lock()
					if statsCancel, exists := d.statsContexts[c.ID]; exists {
						statsCancel()
						delete(d.statsContexts, c.ID)
					}
					d.statsContextsLock.Unlock()

					// Cancel the log collection goroutine before deleting the container
					d.logContextsLock.Lock()
					if logCancel, exists := d.logContexts[c.ID]; exists {
						logCancel()
						delete(d.logContexts, c.ID)
					}
					d.logContextsLock.Unlock()

					timer4 := time.NewTimer(dto.ChannelTimeout)
					select {
					case d.containersCommand <- ContainersCommand{
						functor: func(docker *Docker) *Container {
							delete(docker.containers, c.ID)
							c.Delete()
							return nil
						},
					}:
					case <-timer4.C:
						d.logger.Warn("timeout sending destroy command in handleEvents")
					case <-ctx.Done():
						timer4.Stop()
						return
					}
					timer4.Stop()
				}

				// Restart stats streaming when container starts or unpauses
				// First cancel the old stats goroutine to prevent leaks
				if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
					d.statsContextsLock.Lock()
					// Cancel old stats goroutine if it exists
					if oldCancel, exists := d.statsContexts[c.ID]; exists {
						oldCancel()
					}
					// Create new context for stats goroutine
					statsCtx, statsCancel := context.WithCancel(ctx)
					d.statsContexts[c.ID] = statsCancel
					d.statsContextsLock.Unlock()

					go d.getContainerStatsRealtime(statsCtx, c)
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

		case err, ok := <-errs:
			if !ok {
				d.logger.DebugContext(ctx, "error channel closed")
				return
			}
			if err != nil {
				d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
			}
		}
	}
}
