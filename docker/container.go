package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/internal/model"
	"github.com/syrm/c8s/internal/pool"
)

// Container represents a Docker container with its state and metrics.
// Access is protected by mu for thread-safety.
type Container struct {
	ID                  model.ContainerID
	Service             string
	Name                string
	Project             model.ContainerProject
	CPUPercentage       float64
	MemoryPercentage    float64
	Status              string
	PendingAction       string
	LogCollectionActive bool

	mu sync.RWMutex

	// ctx is the container's context, derived from the parent context.
	// Used for cancellation propagation.
	ctx    context.Context
	cancel context.CancelFunc

	// statsGen tracks the current generation of stats goroutine
	statsGen atomic.Uint64

	// logs are protected by mu as well now, simplifying synchronization
	logs        []string
	maxLogLines int
}

// NewContainer creates a new Container from a Docker API container summary.
// Returns an error if the context is already cancelled.
func NewContainer(
	ctx context.Context,
	dockerContainer apiContainer.Summary,
	action events.Action,
	project model.ContainerProject,
	maxLogLines int,
) (*Container, error) {
	// Don't create container if context is already cancelled
	if ctx.Err() != nil {
		return nil, fmt.Errorf("cannot create container: %w", ctx.Err())
	}

	// Create a child context for the container
	// This allows proper cancellation propagation when the parent context is cancelled
	childCtx, cancel := context.WithCancel(ctx)

	status := dockerContainer.State
	if status == "" {
		status = statusFromAction(action)
	}
	if status == "" {
		status = model.StatusCreated
	}

	containerName := ""
	if len(dockerContainer.Names) > 0 {
		containerName = strings.TrimPrefix(dockerContainer.Names[0], "/")
	}

	return &Container{
		ID:          model.ContainerID(dockerContainer.ID),
		Service:     dockerContainer.Labels["com.docker.compose.service"],
		Name:        containerName,
		Project:     project,
		ctx:         childCtx,
		cancel:      cancel,
		Status:      status,
		maxLogLines: maxLogLines,
	}, nil
}

// Snapshot returns a copy of the container state as a model.Container.
func (c *Container) Snapshot() model.Container {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Copy logs
	logsCopy := make([]string, len(c.logs))
	copy(logsCopy, c.logs)

	return model.Container{
		ID:               c.ID,
		Project:          c.Project,
		Service:          c.Service,
		Name:             c.Name,
		CPUPercentage:    c.CPUPercentage,
		MemoryPercentage: c.MemoryPercentage,
		Status:           c.Status,
		PendingAction:    c.PendingAction,
		Logs:             logsCopy,
	}
}

// AppendLog adds a log line to the container's log buffer.
// It uses a pool to reduce memory allocations and GC pressure.
func (c *Container) AppendLog(line string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get a slice from the pool if we need to allocate
	if c.logs == nil {
		c.logs = pool.StringSlicePool().Get()
	}

	c.logs = append(c.logs, line)

	// Truncate if we exceed max lines
	if len(c.logs) > c.maxLogLines {
		// Keep only the last maxLogLines
		copy(c.logs, c.logs[len(c.logs)-c.maxLogLines:])
		c.logs = c.logs[:c.maxLogLines]
	}
}

// GetLogsCopy returns a copy of the logs slice.
func (c *Container) GetLogsCopy() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.logs == nil {
		return nil
	}
	result := make([]string, len(c.logs))
	copy(result, c.logs)
	return result
}

// LogsLen returns the number of log lines.
func (c *Container) LogsLen() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.logs)
}

// Delete cancels the container's context and clears resources.
// It returns the log slice to the pool for reuse.
func (c *Container) Delete() {
	if c.cancel != nil {
		c.cancel()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Return the log slice to the pool
	if c.logs != nil {
		pool.StringSlicePool().Put(c.logs)
	}
	c.logs = nil
}

func (c *Container) SetStatusFromAction(action events.Action) {
	newStatus := statusFromAction(action)
	if newStatus != "" {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.Status = newStatus
		c.PendingAction = ""
	}
}

func (c *Container) SetPendingAction(action string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Only allow valid pending actions
	switch action {
	case "", "starting", "stopping", "restarting", "removing":
		c.PendingAction = action
	}
}

func (c *Container) SetStatus(status string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Status = status
}

// statusFromAction returns the container status based on a Docker event action.
func statusFromAction(action events.Action) string {
	switch action {
	case events.ActionStart, events.ActionUnPause, events.ActionReload:
		return model.StatusRunning
	case events.ActionStop, events.ActionDie, events.ActionKill, events.ActionOOM:
		return model.StatusExited
	case events.ActionPause:
		return model.StatusPaused
	case events.ActionRestart:
		return model.StatusRestarting
	case events.ActionCreate:
		return model.StatusCreated
	case events.ActionRemove, events.ActionDelete, events.ActionDestroy:
		return model.StatusRemoving
	default:
		return ""
	}
}

func (c *Container) Update(stats apiContainer.StatsResponse) {
	cpu, mem := c.calculateStats(stats)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.CPUPercentage = cpu
	c.MemoryPercentage = mem
}

func (c *Container) calculateStats(stats apiContainer.StatsResponse) (float64, float64) {
	// CPU
	cpuPercent := 0.0
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0.0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	// Memory
	memUsage := calculateMemUsageUnixNoCache(stats.MemoryStats)
	memPercent := 0.0
	if stats.MemoryStats.Limit != 0 {
		memPercent = memUsage / float64(stats.MemoryStats.Limit) * 100.0
	}

	return cpuPercent, memPercent
}

func calculateMemUsageUnixNoCache(mem apiContainer.MemoryStats) float64 {
	if v, isCgroup1 := mem.Stats["total_inactive_file"]; isCgroup1 && v < mem.Usage {
		return float64(mem.Usage - v)
	}
	if v := mem.Stats["inactive_file"]; v < mem.Usage {
		return float64(mem.Usage - v)
	}
	return float64(mem.Usage)
}
