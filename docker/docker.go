// Package docker provides a client for interacting with Docker/Podman daemons.
// It handles container monitoring, log streaming, and real-time statistics collection.
//
// The main type is Docker, which manages connections and coordinates monitoring
// goroutines for containers belonging to Docker Compose projects.
package docker

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	dockerClient "github.com/docker/docker/client"

	"github.com/syrm/c8s/internal/config"
	"github.com/syrm/c8s/internal/model"
)

// Docker manages the connection to the Docker daemon and container monitoring.
// It provides real-time statistics and log streaming for Docker Compose projects.
//
// The type maintains thread-safe state for all tracked containers and coordinates
// multiple goroutines for event handling, stats collection, and log streaming.
type Docker struct {
	client     DockerAPI
	containers map[model.ContainerID]*Container
	mu         sync.RWMutex // Protects containers map
	cfg        *config.Config

	requestData <-chan model.RequestData
	logger      *slog.Logger
	done        chan struct{} // Signals when Run() has completed
	// parentCtx is the context used for stats/logs goroutines.
	// This is separate from errgroup's context to prevent cascading cancellations
	// when one goroutine in the errgroup fails.
	// Protected by parentCtxMu for thread-safe access.
	parentCtx    context.Context
	parentCancel context.CancelFunc
	parentCtxMu  sync.RWMutex // Protects parentCtx and parentCancel
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
//
// If DOCKER_HOST is not set, it attempts to use the Podman socket if available.
// The function automatically detects the container runtime and configures the client accordingly.
//
// Parameters:
//   - ctx: Context for initialization
//   - requestData: Channel for receiving requests from the TUI layer
//   - logger: Structured logger for debugging and monitoring
//
// Returns:
//   - *Docker: Configured Docker client ready for monitoring
//   - error: Any error encountered during initialization
func NewDocker(
	ctx context.Context,
	requestData <-chan model.RequestData,
	logger *slog.Logger,
	cfg *config.Config,
) (*Docker, error) {
	// Check if DOCKER_HOST is set
	dockerHost := cfg.DockerHost

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
		return nil, fmt.Errorf("failed to create docker client with opts %v: %w", opts, err)
	}

	// Initialize parentCtx with a background context that will be replaced in Run()
	// This prevents nil pointer panic if startLogCollection is called before Run()
	parentCtx, parentCancel := context.WithCancel(context.Background())

	return &Docker{
		client: cli,
		cfg:    cfg,
		// Pre-allocate map for typical Docker Compose setups
		containers:    make(map[model.ContainerID]*Container, cfg.InitialContainerMapSize),
		requestData:   requestData,
		logger:        logger,
		done:          make(chan struct{}),
		parentCtx:     parentCtx,
		parentCancel:  parentCancel,
		statsContexts: make(map[model.ContainerID]context.CancelFunc),
		logContexts:   make(map[model.ContainerID]context.CancelFunc),
	}, nil
}

// Close closes the Docker client and releases resources.
func (d *Docker) Close() error {
	return d.client.Close()
}

// getParentCtx returns the parent context in a thread-safe manner.
// This should be used instead of directly accessing d.parentCtx.
func (d *Docker) getParentCtx() context.Context {
	d.parentCtxMu.RLock()
	defer d.parentCtxMu.RUnlock()
	return d.parentCtx
}

// Run starts the Docker monitoring goroutines.
func (d *Docker) Run(ctx context.Context) {
	defer close(d.done) // Signal completion when Run exits

	// Update parent context for stats/logs goroutines.
	// This replaces the initial background context with the actual run context.
	// Protected by mutex to prevent race conditions with startLogCollection.
	d.parentCtxMu.Lock()
	// Cancel the initial background context
	if d.parentCancel != nil {
		d.parentCancel()
	}
	d.parentCtx, d.parentCancel = context.WithCancel(ctx)
	d.parentCtxMu.Unlock()

	// Ensure cleanup happens even if one goroutine fails
	defer func() {
		d.parentCtxMu.Lock()
		if d.parentCancel != nil {
			d.parentCancel() // Cancel all stats/logs goroutines
		}
		d.parentCtxMu.Unlock()

		// Wait for all goroutines to finish cleanly
		d.logCollectorsWG.Wait()
		d.statsWG.Wait()
	}()

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

	// Wait for all goroutines to complete
	if err := eg.Wait(); err != nil {
		d.logger.ErrorContext(errCtx, "error in Docker Run", slog.Any("error", err))
		// Context is already cancelled by defer, ensuring cleanup
	}
}

// Wait blocks until Run() has completed.
func (d *Docker) Wait() {
	<-d.done
}

// Helper methods replacing docker/commands.go

func (d *Docker) getContainer(id model.ContainerID) *Container {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.containers[id]
}

func (d *Docker) getContainersList(filter func(*Container) bool) []*Container {
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

func (d *Docker) addContainer(c *Container) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.containers[c.ID] = c
}

func (d *Docker) removeContainer(id model.ContainerID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.containers, id)
}
