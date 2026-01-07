package docker

import (
	"context"
	"log/slog"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/dto"
)

type ContainerID string

type Container struct {
	ID                  ContainerID
	Service             string
	Name                string
	Project             dto.ContainerProject
	CPUPercentage       float64
	MemoryPercentage    float64
	Logs                []string
	Status              string
	PendingAction       string // "starting", "stopping", "restarting", "removing" or ""
	LogCollectionActive bool
	Command             chan ContainerCommand
	cancel              context.CancelFunc
	logger              *slog.Logger
}

type ContainerResponse struct {
	ID               ContainerID
	Project          dto.ContainerProject
	Service          string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	Status           string
	PendingAction    string
}

type ContainerCommand struct {
	functor  func(*Container)
	response chan ContainerResponse
}

func NewContainer(
	ctx context.Context,
	dockerContainer apiContainer.Summary,
	action events.Action,
	project dto.ContainerProject,
	logger *slog.Logger,
) *Container {
	ctx, cancel := context.WithCancel(ctx)

	status := dockerContainer.State
	// Derive status from action if State is empty (for events)
	if status == "" {
		status = statusFromAction(action)
	}
	// Default to "created" if status is still unknown
	// (container must exist to receive events)
	if status == "" {
		status = "created"
	}

	c := &Container{
		ID:      ContainerID(dockerContainer.ID),
		Service: dockerContainer.Labels["com.docker.compose.service"],
		Name:    dockerContainer.Names[0],
		Command: make(chan ContainerCommand),
		Project: project,
		cancel:  cancel,
		logger:  logger,
		Status:  status,
	}

	go c.handleCommands(ctx)

	return c
}

func (c *Container) handleCommands(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case cmd := <-c.Command:
			if cmd.functor != nil {
				cmd.functor(c)
			}

			if cmd.response != nil {
				cmd.response <- ContainerResponse{
					ID:               c.ID,
					Project:          c.Project,
					Name:             c.Name,
					Service:          c.Service,
					CPUPercentage:    c.CPUPercentage,
					MemoryPercentage: c.MemoryPercentage,
					Status:           c.Status,
					PendingAction:    c.PendingAction,
				}
			}
		}
	}
}

const maxLogLines = 1000

func (c *Container) AppendLog(line string) {
	c.Logs = append(c.Logs, line)
	// Limit log size to prevent memory leak
	if len(c.Logs) > maxLogLines {
		c.Logs = c.Logs[len(c.Logs)-maxLogLines:]
	}
}

func (c *Container) Delete() {
	c.cancel()
	c.Logs = nil // Clear logs to free memory
}

func (c *Container) SetStatusFromAction(action events.Action) {
	newStatus := statusFromAction(action)
	// Only update status if the action represents a known state change
	if newStatus != "" {
		c.Status = newStatus
		// Clear pending action when status actually changes
		c.PendingAction = ""
	}
}

func (c *Container) SetPendingAction(action string) {
	c.PendingAction = action
}

// statusFromAction returns the container status based on a Docker event action.
// Returns empty string for actions that don't represent a state change
// (e.g., exec_create, exec_start, health_status, top, rename, etc.)
func statusFromAction(action events.Action) string {
	switch action {
	case events.ActionStart, events.ActionUnPause, events.ActionReload:
		return "running"
	case events.ActionStop, events.ActionDie, events.ActionKill, events.ActionOOM:
		return "exited"
	case events.ActionPause:
		return "paused"
	case events.ActionRestart:
		return "restarting"
	case events.ActionCreate:
		return "created"
	case events.ActionRemove, events.ActionDelete, events.ActionDestroy:
		return "removing"
	default:
		// Unknown actions (exec_*, health_status, top, rename, etc.)
		// should not change the container status
		return ""
	}
}


func (c *Container) Update(stats apiContainer.StatsResponse) {
	c.updateCPUPercent(stats.CPUStats, stats.PreCPUStats)
	c.updateMemoryPercentage(stats.MemoryStats)
}

func (c *Container) updateMemoryPercentage(memoryStats apiContainer.MemoryStats) {
	memUsage := c.calculateMemUsageUnixNoCache(memoryStats)
	c.MemoryPercentage = c.calculateMemPercentUnixNoCache(float64(memoryStats.Limit), memUsage)
}

func (c *Container) calculateMemUsageUnixNoCache(mem apiContainer.MemoryStats) float64 {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	// cgroup v1
	if v, isCgroup1 := mem.Stats["total_inactive_file"]; isCgroup1 && v < mem.Usage {
		return float64(mem.Usage - v)
	}
	// cgroup v2
	if v := mem.Stats["inactive_file"]; v < mem.Usage {
		return float64(mem.Usage - v)
	}
	return float64(mem.Usage)
}

func (c *Container) calculateMemPercentUnixNoCache(limit float64, usedNoCache float64) float64 {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	if limit != 0 {
		return usedNoCache / limit * 100.0
	}
	return 0
}

func (c *Container) updateCPUPercent(cpuStats apiContainer.CPUStats, preCPUStats apiContainer.CPUStats) {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(cpuStats.CPUUsage.TotalUsage) - float64(preCPUStats.CPUUsage.TotalUsage)
		// calculate the change for the entire system between readings
		systemDelta = float64(cpuStats.SystemUsage) - float64(preCPUStats.SystemUsage)
		onlineCPUs  = float64(cpuStats.OnlineCPUs)
	)

	if onlineCPUs == 0.0 {
		onlineCPUs = float64(len(cpuStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	c.CPUPercentage = cpuPercent
}
