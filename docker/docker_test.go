package docker

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/internal/model"
)

// mockDockerAPI implements DockerAPI for testing.
type mockDockerAPI struct {
	containers      []apiContainer.Summary
	statsResponses  map[string][]apiContainer.StatsResponse
	logContent      map[string]string
	events          chan events.Message
	errors          chan error
	statsStreamOpen map[string]bool
	mu              sync.Mutex
}

func newMockDockerAPI() *mockDockerAPI {
	return &mockDockerAPI{
		containers:      []apiContainer.Summary{},
		statsResponses:  make(map[string][]apiContainer.StatsResponse),
		logContent:      make(map[string]string),
		events:          make(chan events.Message, 100),
		errors:          make(chan error, 10),
		statsStreamOpen: make(map[string]bool),
	}
}

func (m *mockDockerAPI) ContainerList(ctx context.Context, options apiContainer.ListOptions) ([]apiContainer.Summary, error) {
	return m.containers, nil
}

type mockStatsReader struct {
	data    string
	reader  *strings.Reader
	closeCh chan struct{}
}

func (r *mockStatsReader) Read(p []byte) (n int, err error) {
	select {
	case <-r.closeCh:
		return 0, io.EOF
	default:
		return r.reader.Read(p)
	}
}

func (r *mockStatsReader) Close() error {
	select {
	case <-r.closeCh:
	default:
		close(r.closeCh)
	}
	return nil
}

func (m *mockDockerAPI) ContainerStats(ctx context.Context, containerID string, stream bool) (apiContainer.StatsResponseReader, error) {
	m.mu.Lock()
	m.statsStreamOpen[containerID] = true
	m.mu.Unlock()

	// Return a mock stats response that provides JSON-encoded stats
	statsJSON := `{"read":"2024-01-01T00:00:00Z","cpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":1000},"precpu_stats":{"cpu_usage":{"total_usage":90},"system_cpu_usage":900},"memory_stats":{"usage":1024,"limit":2048}}`

	return apiContainer.StatsResponseReader{
		Body: &mockStatsReader{
			data:    statsJSON,
			reader:  strings.NewReader(statsJSON + "\n"),
			closeCh: make(chan struct{}),
		},
		OSType: "linux",
	}, nil
}

func (m *mockDockerAPI) ContainerLogs(ctx context.Context, containerID string, options apiContainer.LogsOptions) (io.ReadCloser, error) {
	content := m.logContent[containerID]
	if content == "" {
		content = "test log line\n"
	}
	return io.NopCloser(strings.NewReader(content)), nil
}

func (m *mockDockerAPI) Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error) {
	return m.events, m.errors
}

func (m *mockDockerAPI) Close() error {
	return nil
}

// Helper to create test containers
func createTestContainerSummary(id, name, projectID, projectName string) apiContainer.Summary {
	return apiContainer.Summary{
		ID:    id,
		Names: []string{"/" + name},
		Labels: map[string]string{
			"com.docker.compose.project.working_dir": projectID,
			"com.docker.compose.project":             projectName,
			"com.docker.compose.service":             name,
		},
		State: "running",
	}
}

// TestContainersCommandConcurrency tests that multiple goroutines can safely
// send commands to containersCommand channel without race conditions.
func TestContainersCommandConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := &Docker{
		containers:        make(map[model.ContainerID]*Container),
		containersCommand: make(chan ContainersCommand, 16),
		logger:            logger,
		statsContexts:     make(map[model.ContainerID]context.CancelFunc),
		logContexts:       make(map[model.ContainerID]context.CancelFunc),
	}

	// Start the command handler
	go func() {
		d.handleContainersCommand(ctx)
	}()

	// Run multiple goroutines that concurrently access containers
	const numGoroutines = 50
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				containerID := model.ContainerID("container-" + string(rune('A'+gid%26)))

				// Randomly perform add or get operations
				if j%2 == 0 {
					// Add container
					c := &Container{
						ID:      containerID,
						Command: make(chan ContainerCommand, 16),
					}
					d.addContainer(ctx, c)
				} else {
					// Get container
					d.getContainer(ctx, containerID)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestGetContainersList tests concurrent access to container list retrieval.
func TestGetContainersListConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := &Docker{
		containers:        make(map[model.ContainerID]*Container),
		containersCommand: make(chan ContainersCommand, 16),
		logger:            logger,
		statsContexts:     make(map[model.ContainerID]context.CancelFunc),
		logContexts:       make(map[model.ContainerID]context.CancelFunc),
	}

	// Pre-populate with containers
	for i := 0; i < 10; i++ {
		id := model.ContainerID("container-" + string(rune('A'+i)))
		d.containers[id] = &Container{
			ID:      id,
			Project: model.ContainerProject{ID: model.ProjectID("/test"), Name: "test"},
			Command: make(chan ContainerCommand, 16),
		}
	}

	// Start the command handler
	go func() {
		d.handleContainersCommand(ctx)
	}()

	// Run multiple goroutines that concurrently list containers
	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				containers := d.getContainersList(ctx, nil)
				// Just verify we get a result without panicking
				_ = containers
			}
		}()
	}

	wg.Wait()
}

// TestChannelHelpers tests the channel helper functions under concurrent load.
func TestChannelHelpersConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := make(chan int, 10)
	const numSenders = 50
	const numReceivers = 50

	var wg sync.WaitGroup
	wg.Add(numSenders + numReceivers)

	// Senders
	for i := 0; i < numSenders; i++ {
		go func(val int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case ch <- val:
				case <-ctx.Done():
					return
				default:
					// Channel full, skip
				}
			}
		}(i)
	}

	// Receivers
	for i := 0; i < numReceivers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				select {
				case <-ch:
				case <-ctx.Done():
					return
				default:
					// Channel empty, skip
				}
			}
		}()
	}

	wg.Wait()
}

// TestContainerCommandConcurrency tests concurrent access to container commands.
func TestContainerCommandConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_ = logger // Used for docker instance only
	c := &Container{
		ID:      "test-container",
		Command: make(chan ContainerCommand, 16),
	}

	// Start the command handler in a separate context
	containerCtx, containerCancel := context.WithCancel(ctx)
	defer containerCancel()
	go func() {
		c.handleCommands(containerCtx)
	}()

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				response := make(chan ContainerResponse, 1)
				select {
				case c.Command <- ContainerCommand{response: response}:
					select {
					case <-response:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestLogCollectionFlagConcurrency tests that LogCollectionActive flag is properly handled.
func TestLogCollectionFlagConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := &Container{
		ID:      "test-container",
		Command: make(chan ContainerCommand, 16),
	}

	// Start the command handler
	go func() {
		c.handleCommands(ctx)
	}()

	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	startedCount := 0
	var countMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			resultCh := make(chan bool, 1)
			select {
			case c.Command <- ContainerCommand{
				functor: func(container *Container) {
					needStart := !container.LogCollectionActive
					if needStart {
						container.LogCollectionActive = true
					}
					resultCh <- needStart
				},
			}:
				select {
				case needStart := <-resultCh:
					if needStart {
						countMu.Lock()
						startedCount++
						countMu.Unlock()
					}
				case <-ctx.Done():
				}
			case <-ctx.Done():
			}
		}()
	}

	wg.Wait()

	// Only one goroutine should have started collection
	countMu.Lock()
	defer countMu.Unlock()
	if startedCount != 1 {
		t.Errorf("Expected exactly 1 log collection start, got %d", startedCount)
	}
}

// TestStatsGenConcurrency tests that statsGen atomic operations work correctly.
func TestStatsGenConcurrency(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := &Container{
		ID:      "test-container",
		Command: make(chan ContainerCommand, 16),
	}

	// Start the command handler
	go func() {
		c.handleCommands(ctx)
	}()

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(gid int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if j%10 == 0 {
					// Increment generation
					c.statsGen.Add(1)
				} else {
					// Read generation
					_ = c.statsGen.Load()
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify final count
	finalGen := c.statsGen.Load()
	expectedGen := uint64(numGoroutines * 10) // Each goroutine increments 10 times
	if finalGen != expectedGen {
		t.Errorf("Expected generation %d, got %d", expectedGen, finalGen)
	}
}
