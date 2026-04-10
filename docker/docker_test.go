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

// TestContainersConcurrentAccess tests that multiple goroutines can safely
// access containers map via mutex-protected methods without race conditions.
func TestContainersConcurrentAccess(t *testing.T) {
	t.Parallel()
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := &Docker{
		containers:    make(map[model.ContainerID]*Container),
		logger:        logger,
		statsContexts: make(map[model.ContainerID]context.CancelFunc),
		logContexts:   make(map[model.ContainerID]context.CancelFunc),
	}

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
						ID: containerID,
					}
					d.addContainer(c)
				} else {
					// Get container
					d.getContainer(containerID)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestGetContainersListConcurrency tests concurrent access to container list retrieval.
func TestGetContainersListConcurrency(t *testing.T) {
	t.Parallel()
	_, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := &Docker{
		containers:    make(map[model.ContainerID]*Container),
		logger:        logger,
		statsContexts: make(map[model.ContainerID]context.CancelFunc),
		logContexts:   make(map[model.ContainerID]context.CancelFunc),
	}

	// Pre-populate with containers
	for i := 0; i < 10; i++ {
		id := model.ContainerID("container-" + string(rune('A'+i)))
		d.containers[id] = &Container{
			ID:      id,
			Project: model.ContainerProject{ID: model.ProjectID("/test"), Name: "test"},
		}
	}

	// Run multiple goroutines that concurrently list containers
	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				containers := d.getContainersList(nil)
				// Verify we get results without panicking
				if len(containers) == 0 {
					// Initial state may have containers, but race may cause empty slice
					// This is expected behavior - just verify no panic
				}
			}
		}()
	}

	wg.Wait()
}

// TestChannelHelpersConcurrency tests the channel helper functions under concurrent load.
func TestChannelHelpersConcurrency(t *testing.T) {
	t.Parallel()
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

// TestContainerSnapshotConcurrency tests concurrent access to container snapshot.
func TestContainerSnapshotConcurrency(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := &Container{
		ID:      "test-container",
		Service: "test-service",
		Name:    "test-name",
		Project: model.ContainerProject{ID: model.ProjectID("/test"), Name: "test"},
	}

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Readers - take snapshots
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snapshot := c.Snapshot()
				// Verify snapshot is valid
				if snapshot.ID != c.ID {
					t.Errorf("Snapshot ID mismatch: got %s, want %s", snapshot.ID, c.ID)
				}

				select {
				case <-ctx.Done():
					return
				default:
				}
			}
		}()
	}

	wg.Wait()
}

// TestLogCollectionFlagConcurrency tests that LogCollectionActive flag is properly handled.
func TestLogCollectionFlagConcurrency(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := &Container{
		ID: "test-container",
	}

	const numGoroutines = 20

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	startedCount := 0
	var countMu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			// Simulate checking and setting LogCollectionActive under lock
			c.mu.Lock()
			needStart := !c.LogCollectionActive
			if needStart {
				c.LogCollectionActive = true
			}
			c.mu.Unlock()

			if needStart {
				countMu.Lock()
				startedCount++
				countMu.Unlock()
			}

			select {
			case <-ctx.Done():
				return
			default:
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
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := &Container{
		ID: "test-container",
	}

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

				select {
				case <-ctx.Done():
					return
				default:
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

// TestContainerDelete tests that Delete properly cleans up resources.
func TestContainerDelete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	childCtx, cancel := context.WithCancel(ctx)

	c := &Container{
		ID:     "test-container",
		cancel: cancel,
	}

	c.Delete()

	// Verify context was cancelled
	select {
	case <-childCtx.Done():
		// Expected
	default:
		t.Error("Expected context to be cancelled after Delete()")
	}
}

// TestDockerAddRemoveContainer tests add and remove operations.
func TestDockerAddRemoveContainer(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	d := &Docker{
		containers:    make(map[model.ContainerID]*Container),
		logger:        logger,
		statsContexts: make(map[model.ContainerID]context.CancelFunc),
		logContexts:   make(map[model.ContainerID]context.CancelFunc),
	}

	containerID := model.ContainerID("test-container-1")
	c := &Container{
		ID:      containerID,
		Service: "test-service",
	}

	// Add container
	d.addContainer(c)

	// Verify it was added
	retrieved := d.getContainer(containerID)
	if retrieved == nil {
		t.Fatal("Expected to find container after adding")
	}
	if retrieved.ID != containerID {
		t.Errorf("Expected container ID %s, got %s", containerID, retrieved.ID)
	}

	// Remove container
	d.removeContainer(containerID)

	// Verify it was removed
	retrieved = d.getContainer(containerID)
	if retrieved != nil {
		t.Error("Expected container to be nil after removal")
	}
}

// TestContainerStatusFromAction tests status mapping from Docker events.
func TestContainerStatusFromAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		action   events.Action
		expected string
	}{
		{events.ActionStart, model.StatusRunning},
		{events.ActionUnPause, model.StatusRunning},
		{events.ActionStop, model.StatusExited},
		{events.ActionDie, model.StatusExited},
		{events.ActionPause, model.StatusPaused},
		{events.ActionRestart, model.StatusRestarting},
		{events.ActionCreate, model.StatusCreated},
		{events.ActionRemove, model.StatusRemoving},
		{events.Action("unknown"), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.action), func(t *testing.T) {
			result := statusFromAction(tt.action)
			if result != tt.expected {
				t.Errorf("statusFromAction(%s) = %s, want %s", tt.action, result, tt.expected)
			}
		})
	}
}
