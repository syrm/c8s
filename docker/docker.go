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
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	dockerClient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/syrm/c8s/dto"
	"github.com/syrm/c8s/internal/timer"
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
	// logCollectorsWG tracks log collection goroutines for clean shutdown
	logCollectorsWG   sync.WaitGroup
	// statsWG tracks stats goroutines for clean shutdown
	statsWG           sync.WaitGroup
}

// findPodmanSocket runs "podman system info" to get the socket path.
// Returns the socket URL (e.g., "unix:///path/to/socket") if found, empty string otherwise.
func findPodmanSocket() string {
	cmd := exec.Command("podman", "system", "info", "--format", "unix://{{.Host.RemoteSocket.Path}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	socketPath := string(output)
	// Trim whitespace and newlines
	socketPath = strings.TrimSpace(socketPath)
	// Verify it's a valid unix socket path
	if socketPath != "" && strings.HasPrefix(socketPath, "unix://") {
		return socketPath
	}
	return ""
}

// NewDocker creates a new Docker client and initializes monitoring infrastructure.
// It returns an error if the Docker client cannot be created.
// If DOCKER_HOST is not set, it attempts to use the Podman socket if available.
func NewDocker(
	ctx context.Context,
	requestData <-chan dto.RequestData,
	logger *slog.Logger,
) (*Docker, error) {
	// Check if DOCKER_HOST is set
	dockerHost := os.Getenv("DOCKER_HOST")

	var opts []dockerClient.Opt
	if dockerHost == "" {
		// DOCKER_HOST not set, try to find Podman socket
		if podmanSocket := findPodmanSocket(); podmanSocket != "" {
			logger.Info("using Podman socket", "socket", podmanSocket)
			opts = []dockerClient.Opt{
				dockerClient.WithHost(podmanSocket),
				dockerClient.WithAPIVersionNegotiation(),
			}
		} else {
			// No Podman socket found, use default (FromEnv)
			opts = []dockerClient.Opt{
				dockerClient.FromEnv,
				dockerClient.WithAPIVersionNegotiation(),
			}
		}
	} else {
		// DOCKER_HOST is set, use it
		opts = []dockerClient.Opt{
			dockerClient.FromEnv,
			dockerClient.WithAPIVersionNegotiation(),
		}
	}

	cli, err := dockerClient.NewClientWithOpts(opts...)
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

// backoffDurations defines exponential backoff intervals for reconnection attempts.
var backoffDurations = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
}

func (d *Docker) Run(ctx context.Context) {
	defer close(d.done) // Signal completion when Run exits

	eg, errCtx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		return d.handleContainersCommand(errCtx)
	})

	eg.Go(func() error {
		d.handleEventsWithBackoff(errCtx)
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

	// Wait for all log collection goroutines to finish
	d.logCollectorsWG.Wait()

	// Wait for all stats goroutines to finish
	d.statsWG.Wait()
}

// handleEventsWithBackoff wraps handleEvents with exponential backoff for reconnection.
// If the events stream fails, it will retry with increasing delays.
func (d *Docker) handleEventsWithBackoff(ctx context.Context) {
	attempt := 0
	for {
		d.handleEvents(ctx)

		// Check if context was cancelled (graceful shutdown)
		if ctx.Err() != nil {
			return
		}

		// Calculate backoff duration
		backoffIdx := attempt
		if backoffIdx >= len(backoffDurations) {
			backoffIdx = len(backoffDurations) - 1
		}
		backoff := backoffDurations[backoffIdx]

		d.logger.WarnContext(ctx, "Docker events stream ended, reconnecting",
			slog.Int("attempt", attempt+1),
			slog.Duration("backoff", backoff))

		// Wait before reconnecting
		backoffTimer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop(backoffTimer)
			return
		case <-backoffTimer.C:
		}

		attempt++
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
	defer timer.Stop(timer1)

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

	// Check if log collection is already active and get current logs
	// Use functor for serialized access to container state
	dtoResponse := make(chan dto.Container, 1)
	startCollection := make(chan bool, 1)
	timer2 := time.NewTimer(dto.ChannelTimeout)
	defer timer.Stop(timer2)

	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			if container.deleted.Load() {
				dtoResponse <- dto.Container{}
				return
			}

			// Build DTO with logs copy - MUST SEND THIS FIRST to avoid deadlock
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

			// Check if log collection is already active and signal AFTER sending response
			needStart := !container.LogCollectionActive
			if needStart {
				container.LogCollectionActive = true
			}
			// This send won't block because channel is buffered
			startCollection <- needStart
		},
	}:
	case <-timer2.C:
		d.logger.Warn("timeout sending to container command in handleRequestContainerLog")
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		r.Response <- dto.Container{}
		return
	}

	// Wait for DTO response
	var dtoContainer dto.Container
	timer3 := time.NewTimer(dto.ChannelTimeout)
	defer timer.Stop(timer3)
	select {
	case dtoContainer = <-dtoResponse:
	case <-timer3.C:
		d.logger.Warn("timeout waiting for dto in handleRequestContainerLog")
		r.Response <- dto.Container{}
		return
	case <-ctx.Done():
		r.Response <- dto.Container{}
		return
	}

	// Check if we need to start log collection
	timer4 := time.NewTimer(dto.ChannelTimeout)
	defer timer.Stop(timer4)
	select {
	case needStart := <-startCollection:
		if needStart {
			// Only create log context when we're actually starting collection
			// This prevents context leaks when collection is already active
			d.logContextsLock.Lock()
			// Cancel any existing log collection for this container
			if oldCancel, exists := d.logContexts[c.ID]; exists {
				oldCancel()
				delete(d.logContexts, c.ID)
			}
			// Create new log context
			ctxLog, cancel := context.WithCancel(ctx)
			d.logContexts[c.ID] = cancel
			d.logContextsLock.Unlock()

			// Start log collection goroutine with WaitGroup tracking
			d.logCollectorsWG.Add(1)
			go func() {
				defer d.logCollectorsWG.Done()
				d.collectContainerLogs(ctxLog, c)
			}()
		}
	case <-timer4.C:
		d.logger.Warn("timeout waiting for startCollection signal")
		// Continue anyway, we have the DTO
	}

	r.Response <- dtoContainer
}

func (d *Docker) handleRequestContainerProject(ctx context.Context, r *dto.RequestProject) {
	// First, get the list of containers for this project synchronously
	// This avoids the deadlock that can occur with channel-based collection
	// when the buffer is too small for the number of containers
	containersChan := make(chan []*Container, 1)
	timer1 := time.NewTimer(dto.ChannelTimeout)
	defer timer.Stop(timer1)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			// Collect matching containers into a slice (no blocking channel sends)
			var matched []*Container
			for _, c := range docker.containers {
				if r.ProjectID == c.Project.ID {
					matched = append(matched, c)
				}
			}
			containersChan <- matched
			return nil
		},
	}:
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestContainerProject")
		r.Response <- nil
		return
	case <-ctx.Done():
		r.Response <- nil
		return
	}

	// Wait for the list of containers
	var matchedContainers []*Container
	timer2 := time.NewTimer(dto.ChannelTimeout)
	defer timer.Stop(timer2)
	select {
	case matchedContainers = <-containersChan:
	case <-timer2.C:
		d.logger.Warn("timeout waiting for containers list in handleRequestContainerProject")
		r.Response <- nil
		return
	case <-ctx.Done():
		r.Response <- nil
		return
	}

	// Now query each container for its data (outside the containersCommand functor)
	containers := make([]dto.Container, 0, len(matchedContainers))
	for _, c := range matchedContainers {
		response := make(chan ContainerResponse, 1)
		cmdTimer := time.NewTimer(dto.ChannelTimeout)

		select {
		case c.Command <- ContainerCommand{
			response: response,
		}:
			timer.Stop(cmdTimer)
			// Wait for response with timeout
			respTimer := time.NewTimer(dto.ChannelTimeout)
			select {
			case container := <-response:
				timer.Stop(respTimer)
				containers = append(containers, containerResponseToDTO(container))
			case <-respTimer.C:
				timer.Stop(respTimer)
				// Skip this container if timeout
			case <-ctx.Done():
				timer.Stop(respTimer)
				r.Response <- containers
				return
			}
		case <-cmdTimer.C:
			timer.Stop(cmdTimer)
			// Skip this container if timeout
		case <-ctx.Done():
			timer.Stop(cmdTimer)
			r.Response <- containers
			return
		}
	}

	r.Response <- containers
}

func (d *Docker) handleRequestSetPendingAction(ctx context.Context, r *dto.RequestSetPendingAction) {
	// First, get the container reference via containersCommand
	// We do NOT send to c.Command inside the functor to avoid blocking handleContainersCommand
	response := make(chan *Container, 1)
	timer1 := time.NewTimer(dto.ChannelTimeout)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[r.ContainerID]
		},
		response: response,
	}:
		timer.Stop(timer1)
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestSetPendingAction")
		r.Response <- false
		return
	case <-ctx.Done():
		timer.Stop(timer1)
		r.Response <- false
		return
	}

	// Wait for container reference
	timer2 := time.NewTimer(dto.ChannelTimeout)
	var c *Container
	select {
	case c = <-response:
		timer.Stop(timer2)
	case <-timer2.C:
		d.logger.Warn("timeout waiting for container in handleRequestSetPendingAction")
		r.Response <- false
		return
	case <-ctx.Done():
		timer.Stop(timer2)
		r.Response <- false
		return
	}

	if c == nil {
		r.Response <- false
		return
	}

	// Now send command to container OUTSIDE the functor
	// This prevents blocking handleContainersCommand
	timer3 := time.NewTimer(dto.ChannelTimeout)
	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			container.SetPendingAction(r.PendingAction)
		},
	}:
		timer.Stop(timer3)
		r.Response <- true
	case <-timer3.C:
		d.logger.Warn("timeout sending to container command in handleRequestSetPendingAction")
		r.Response <- false
	case <-ctx.Done():
		timer.Stop(timer3)
		r.Response <- false
	}
}

func (d *Docker) handleRequestProjectList(ctx context.Context, r *dto.RequestProjectList) {
	// First, get the list of all containers via containersCommand
	// This avoids blocking handleContainersCommand while querying each container
	containersChan := make(chan []*Container, 1)
	timer1 := time.NewTimer(dto.ChannelTimeout)

	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			// Collect all container pointers (no blocking operations here)
			containers := make([]*Container, 0, len(docker.containers))
			for _, c := range docker.containers {
				containers = append(containers, c)
			}
			containersChan <- containers
			return nil
		},
	}:
		timer.Stop(timer1)
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in handleRequestProjectList")
		r.Response <- nil
		return
	case <-ctx.Done():
		timer.Stop(timer1)
		r.Response <- nil
		return
	}

	// Wait for the list of containers
	timer2 := time.NewTimer(dto.ChannelTimeout)
	var containers []*Container
	select {
	case containers = <-containersChan:
		timer.Stop(timer2)
	case <-timer2.C:
		d.logger.Warn("timeout waiting for containers in handleRequestProjectList")
		r.Response <- nil
		return
	case <-ctx.Done():
		timer.Stop(timer2)
		r.Response <- nil
		return
	}

	// Now query each container for its data OUTSIDE the functor
	// This prevents blocking handleContainersCommand
	projects := make(map[dto.ProjectID]dto.Project)

	for _, c := range containers {
		response := make(chan ContainerResponse, 1)
		cmdTimer := time.NewTimer(dto.ChannelTimeout)

		select {
		case c.Command <- ContainerCommand{
			response: response,
		}:
			timer.Stop(cmdTimer)
		case <-cmdTimer.C:
			// Skip this container if timeout
			continue
		case <-ctx.Done():
			timer.Stop(cmdTimer)
			r.Response <- slices.Collect(maps.Values(projects))
			return
		}

		respTimer := time.NewTimer(dto.ChannelTimeout)
		var container ContainerResponse
		select {
		case container = <-response:
			timer.Stop(respTimer)
		case <-respTimer.C:
			// Skip this container if timeout
			continue
		case <-ctx.Done():
			timer.Stop(respTimer)
			r.Response <- slices.Collect(maps.Values(projects))
			return
		}

		// Aggregate project data
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
		if container.Status == dto.StatusRunning {
			isRunning = 1
		}

		project.ContainersRunning += isRunning
		project.ContainersState[container.ID] = container.Status

		projects[projectID] = project
	}

	r.Response <- slices.Collect(maps.Values(projects))
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

	// CRITICAL: Always clean up the log context entry when this goroutine exits
	defer d.cleanupLogContext(c.ID)
	defer d.resetLogCollectionFlag(c)

	// Open the log stream from Docker
	logStream, err := d.openLogStream(ctx, c)
	if err != nil {
		return
	}
	defer logStream.Close()

	// Process the log stream
	d.processLogStream(ctx, c, logStream)
}

// cleanupLogContext removes the log context entry for a container.
// This prevents memory leaks when log collection ends.
func (d *Docker) cleanupLogContext(containerID dto.ContainerID) {
	d.logContextsLock.Lock()
	delete(d.logContexts, containerID)
	d.logContextsLock.Unlock()
}

// resetLogCollectionFlag resets the LogCollectionActive flag on a container.
func (d *Docker) resetLogCollectionFlag(c *Container) {
	resetTimer := time.NewTimer(time.Second)
	defer timer.Stop(resetTimer)
	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			container.LogCollectionActive = false
		},
	}:
	case <-resetTimer.C:
		// Timeout waiting to reset flag, container might be deleted
	}
}

// openLogStream opens a log stream for a container.
func (d *Docker) openLogStream(ctx context.Context, c *Container) (io.ReadCloser, error) {
	since := time.Now().Add(-logHistoryDuration).Format(time.RFC3339)
	out, err := d.client.ContainerLogs(ctx, string(c.ID), apiContainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Since:      since,
	})
	if err != nil {
		d.logger.ErrorContext(ctx, "container logs failed",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
		if out != nil {
			out.Close()
		}
		return nil, err
	}
	return out, nil
}

// processLogStream processes a Docker log stream and sends lines to the container.
func (d *Docker) processLogStream(ctx context.Context, c *Container, logStream io.ReadCloser) {
	// Use a pipe to demultiplex the Docker log stream
	pr, pw := io.Pipe()

	// closeResources closes both the Docker log stream and the pipe reader.
	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			logStream.Close()
			pr.Close()
		})
	}

	lines := make(chan string, logLineBufferSize)
	g, egCtx := errgroup.WithContext(ctx)

	// Goroutine 1: Demultiplex Docker log stream
	g.Go(func() error {
		return d.demuxLogStream(egCtx, c, pw, logStream, closeResources)
	})

	// Goroutine 2: Read lines from pipe
	g.Go(func() error {
		return d.readLogLines(egCtx, c, pr, lines, closeResources)
	})

	// Goroutine 3: Send lines to container
	g.Go(func() error {
		return d.sendLogLinesToContainer(egCtx, c, lines, closeResources)
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.ErrorContext(ctx, "collectContainerLogs errgroup finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
	}
}

// demuxLogStream demultiplexes the Docker log stream using stdcopy.
func (d *Docker) demuxLogStream(ctx context.Context, c *Container, pw *io.PipeWriter, logStream io.ReadCloser, closeResources func()) error {
	defer pw.Close()
	defer closeResources()
	_, err := stdcopy.StdCopy(pw, pw, logStream)
	if err != nil && !errors.Is(err, io.EOF) {
		d.logger.ErrorContext(ctx, "stdcopy finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
		return err
	}
	return nil
}

// readLogLines reads lines from the pipe and sends them to the lines channel.
func (d *Docker) readLogLines(ctx context.Context, c *Container, pr *io.PipeReader, lines chan<- string, closeResources func()) error {
	defer close(lines)
	defer closeResources()
	reader := bufio.NewReader(pr)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				d.logger.ErrorContext(ctx, "end of container logs with error",
					slog.String("container_id", string(c.ID)),
					slog.Any("error", err))
				return err
			}
			return nil
		}
		select {
		case lines <- line:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// sendLogLinesToContainer receives log lines and sends them to the container.
func (d *Docker) sendLogLinesToContainer(ctx context.Context, c *Container, lines <-chan string, closeResources func()) error {
	sendTimer := timer.New(time.Second)
	defer timer.Stop(sendTimer)

	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "collectContainerLogs context is done",
				slog.String("container_id", string(c.ID)))
			closeResources()
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			timer.Stop(sendTimer)
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

// isValidProjectID validates that a project ID (working directory path) is reasonable.
// It checks that the path is not empty and doesn't contain null bytes or other suspicious characters.
func isValidProjectID(projectID string) bool {
	if projectID == "" {
		return false
	}
	// Check for null bytes or control characters (potential injection)
	for _, c := range projectID {
		if c == 0 || (c < 32 && c != '\t') {
			return false
		}
	}
	return true
}

func (d *Docker) createContainer(ctx context.Context, dockerContainer apiContainer.Summary, action events.Action) {
	projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

	if !isProject {
		return
	}

	// Validate project ID before use
	if !isValidProjectID(projectIDraw) {
		d.logger.WarnContext(ctx, "invalid project ID, skipping container",
			slog.String("container_id", dockerContainer.ID),
			slog.String("project_id", projectIDraw))
		return
	}

	project := dto.ContainerProject{
		ID:   dto.ProjectID(projectIDraw),
		Name: dockerContainer.Labels["com.docker.compose.project"],
	}

	response := make(chan *Container, 1)

	// Create timers only when needed to avoid premature expiration
	// If we created all timers at once, timer2 and timer3 would start counting
	// before they're actually used, causing unexpected timeouts.
	timer1 := time.NewTimer(dto.ChannelTimeout)
	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[dto.ContainerID(dockerContainer.ID)]
		},
		response: response,
	}:
		timer.Stop(timer1)
	case <-timer1.C:
		d.logger.Warn("timeout sending to containersCommand in createContainer (check)")
		return
	case <-ctx.Done():
		timer.Stop(timer1)
		return
	}

	var c *Container
	timer2 := time.NewTimer(dto.ChannelTimeout)
	select {
	case c = <-response:
		timer.Stop(timer2)
	case <-timer2.C:
		d.logger.Warn("timeout waiting for response in createContainer (check)")
		return
	case <-ctx.Done():
		timer.Stop(timer2)
		return
	}

	if c != nil {
		// Container already exists
		return
	}

	c = NewContainer(ctx, dockerContainer, action, project)
	if c == nil {
		// Context was cancelled, don't create container
		return
	}

	timer3 := time.NewTimer(dto.ChannelTimeout)
	select {
	case d.containersCommand <- ContainersCommand{
		functor: func(docker *Docker) *Container {
			docker.containers[c.ID] = c
			return nil
		},
	}:
		timer.Stop(timer3)
	case <-timer3.C:
		d.logger.Warn("timeout sending to containersCommand in createContainer (insert)")
		return
	case <-ctx.Done():
		timer.Stop(timer3)
		return
	}

	// Create a dedicated context for stats collection and track it
	statsCtx, statsCancel := context.WithCancel(ctx)

	d.statsContextsLock.Lock()
	d.statsContexts[c.ID] = statsCancel
	d.statsContextsLock.Unlock()

	d.statsWG.Add(1)
	go func() {
		defer d.statsWG.Done()
		d.getContainerStatsRealtime(statsCtx, c)
	}()
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container) {
	// Capture the generation of this stats goroutine to check for restarts
	myGen := c.statsGen.Load()

	// CRITICAL: Always clean up the stats context entry when this goroutine exits
	// This prevents memory leaks regardless of how we exit (error, EOF, context cancel)
	defer func() {
		d.statsContextsLock.Lock()
		// Only delete if we're still the current stats goroutine (same generation)
		// A new stats goroutine might have replaced us already
		if _, exists := d.statsContexts[c.ID]; exists {
			currentGen := c.statsGen.Load()
			if currentGen == myGen {
				delete(d.statsContexts, c.ID)
			}
		}
		d.statsContextsLock.Unlock()
	}()

	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))

		// Mark container as exited instead of deleting
		timer1 := time.NewTimer(dto.ChannelTimeout)
		defer timer.Stop(timer1)
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = dto.StatusExited
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
		defer timer.Stop(timer2)
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				container.Status = dto.StatusExited
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

		// Decode synchronously - the Body will be closed by defer when context is cancelled,
		// which will unblock the Decode() call. This avoids goroutine leaks.
		errDecode := dec.Decode(&stats)
		if errDecode != nil {
			// Check if context was cancelled (Body was closed)
			if ctx.Err() != nil {
				d.logger.DebugContext(ctx, "container stats cancelled", slog.String("container_id", string(c.ID)))
				return
			}
			if !errors.Is(errDecode, io.EOF) && !errors.Is(errDecode, context.DeadlineExceeded) {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			} else {
				d.logger.DebugContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			}
			return
		}

		// Check if this goroutine is still the current generation (container hasn't restarted)
		currentGen := c.statsGen.Load()
		if currentGen != myGen {
			// Container has restarted, this goroutine should exit
			d.logger.DebugContext(ctx, "container stats goroutine outdated", slog.String("container_id", string(c.ID)), slog.Uint64("old_gen", myGen), slog.Uint64("new_gen", currentGen))
			return
		}

		s := stats
		timer3 := time.NewTimer(dto.ChannelTimeout)
		select {
		case c.Command <- ContainerCommand{
			functor: func(container *Container) {
				// Double-check generation before updating to prevent race condition
				if container.statsGen.Load() == myGen {
					container.Update(s)
				}
			},
		}:
			timer.Stop(timer3)
		case <-timer3.C:
			// Skip this stats update if timeout
			timer.Stop(timer3)
		case <-ctx.Done():
			timer.Stop(timer3)
			return
		}
	}
}

func (d *Docker) handleContainersCommand(ctx context.Context) error {
	for {
		select {
		case cmd := <-d.containersCommand:
			// Process each command with panic recovery to prevent deadlocks.
			// If a functor panics, we still send a nil response to unblock the caller.
			d.executeContainersCommand(ctx, cmd)
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleContainersCommand context is done")
			return nil
		}
	}
}

// executeContainersCommand executes a single command with panic recovery.
// This ensures that even if a functor panics, the response channel is written to,
// preventing deadlocks in callers waiting for a response.
func (d *Docker) executeContainersCommand(ctx context.Context, cmd ContainersCommand) {
	var c *Container

	// Recover from panic in functor to prevent deadlock
	defer func() {
		if r := recover(); r != nil {
			d.logger.ErrorContext(ctx, "panic in containersCommand functor", slog.Any("recover", r))
			c = nil // Ensure we send nil on panic
		}
		// Always send response if channel is provided, even on panic
		if cmd.response != nil {
			cmd.response <- c
		}
	}()

	if cmd.functor != nil {
		c = cmd.functor(d)
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
				timer.Stop(timer1)
				continue
			case <-ctx.Done():
				timer.Stop(timer1)
				return
			}
			timer.Stop(timer1)

			var c *Container
			timer2 := time.NewTimer(dto.ChannelTimeout)
			select {
			case c = <-response:
			case <-timer2.C:
				d.logger.Warn("timeout waiting for response in handleEvents")
				timer.Stop(timer2)
				continue
			case <-ctx.Done():
				timer.Stop(timer2)
				return
			}
			timer.Stop(timer2)

			if c != nil {
				timer3 := time.NewTimer(dto.ChannelTimeout)
				select {
				case c.Command <- ContainerCommand{
					functor: func(container *Container) {
						container.SetStatusFromAction(msg.Action)
					},
				}:
					timer.Stop(timer3)
				case <-timer3.C:
					d.logger.Warn("timeout sending status update in handleEvents")
					timer.Stop(timer3)
				case <-ctx.Done():
					timer.Stop(timer3)
					return
				}

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
						timer.Stop(timer4)
						return
					}
					timer.Stop(timer4)
				}

				// Restart stats streaming when container starts or unpauses
				// First cancel the old stats goroutine to prevent leaks
				if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
					d.statsContextsLock.Lock()
					// Cancel old stats goroutine if it exists
					if oldCancel, exists := d.statsContexts[c.ID]; exists {
						oldCancel()
					}
					// Increment stats generation to invalidate old stats goroutines
					c.statsGen.Add(1)
					// Create new context for stats goroutine
					statsCtx, statsCancel := context.WithCancel(ctx)
					d.statsContexts[c.ID] = statsCancel
					d.statsContextsLock.Unlock()

					d.statsWG.Add(1)
					go func() {
						defer d.statsWG.Done()
						d.getContainerStatsRealtime(statsCtx, c)
					}()
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
