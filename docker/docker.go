package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	dockerClient "github.com/docker/docker/client"

	"github.com/syrm/c8s/internal/model"
)

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

// Docker manages the connection to the Docker daemon and container monitoring.
// It provides real-time statistics and log streaming for Docker Compose projects.
type Docker struct {
	client            DockerAPI
	containers        map[model.ContainerID]*Container
	mu                sync.RWMutex // Protects containers map
	
	requestData       <-chan model.RequestData
	logger            *slog.Logger
	done              chan struct{} // Signals when Run() has completed
	// parentCtx is the context passed to Run(), used for stats/logs goroutines.
	// This is separate from errgroup's context to prevent cascading cancellations
	// when one goroutine in the errgroup fails.
	parentCtx    context.Context
	parentCancel context.CancelFunc
	// statsContexts tracks active stats goroutines to prevent leaks
	// Key: container ID, Value: cancel function for the stats goroutine
	statsContexts     map[model.ContainerID]context.CancelFunc
	statsContextsLock sync.Mutex
	// logContexts tracks active log collection contexts to allow cancellation
	// Key: container ID, Value: cancel function for the log collection
	logContexts     map[model.ContainerID]context.CancelFunc
	logContextsLock sync.Mutex
	// logCollectorsWG tracks log collection goroutines for clean shutdown
	logCollectorsWG sync.WaitGroup
	// statsWG tracks stats goroutines for clean shutdown
	statsWG sync.WaitGroup
}

// findPodmanSocket runs "podman system info" to get the socket path.
// Returns the socket URL (e.g., "unix:///path/to/socket") if found, empty string otherwise.
func findPodmanSocket() string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "podman", "system", "info", "--format", "unix://{{.Host.RemoteSocket.Path}}")
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
	requestData <-chan model.RequestData,
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
		containers:        make(map[model.ContainerID]*Container, initialContainerMapSize),
		requestData:       requestData,
		logger:            logger,
		done:              make(chan struct{}),
		statsContexts:     make(map[model.ContainerID]context.CancelFunc),
		logContexts:       make(map[model.ContainerID]context.CancelFunc),
	}, nil
}

// Close closes the Docker client and releases resources.
func (d *Docker) Close() error {
	return d.client.Close()
}

// Run starts the Docker monitoring goroutines.
func (d *Docker) Run(ctx context.Context) {
	defer close(d.done) // Signal completion when Run exits

	// Store parent context for stats/logs goroutines.
	// This prevents cascading cancellations when errgroup fails.
	d.parentCtx, d.parentCancel = context.WithCancel(ctx)
	defer d.parentCancel()

	eg, errCtx := errgroup.WithContext(ctx)

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

// Wait blocks until Run() has completed.
func (d *Docker) Wait() {
	<-d.done
}

// Helper methods replacing docker/commands.go

func (d *Docker) getContainer(ctx context.Context, id model.ContainerID) *Container {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.containers[id]
}

func (d *Docker) getContainersList(ctx context.Context, filter func(*Container) bool) []*Container {
	d.mu.RLock()
	defer d.mu.RUnlock()
	
	list := make([]*Container, 0, len(d.containers))
	for _, c := range d.containers {
		if filter == nil || filter(c) {
			list = append(list, c)
		}
	}
	return list
}

func (d *Docker) addContainer(ctx context.Context, c *Container) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers[c.ID] = c
	return true
}

func (d *Docker) removeContainer(ctx context.Context, id model.ContainerID) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.containers, id)
	return true
}