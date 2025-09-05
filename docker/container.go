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
	ID               ContainerID
	Service          string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	IsRunning        bool
	containerUpdated chan<- dto.ContainerDeletable
	Command          chan ContainerCommand
	Project          dto.Project
	cancel           context.CancelFunc
	logger           *slog.Logger
}

type ContainerCommand struct {
	functor func(*Container)
}

func NewContainer(
	ctx context.Context,
	dockerContainer apiContainer.Summary,
	project dto.Project,
	containerUpdated chan<- dto.ContainerDeletable,
	logger *slog.Logger,
) *Container {
	ctx, cancel := context.WithCancel(ctx)

	c := &Container{
		ID:               ContainerID(dockerContainer.ID),
		Service:          dockerContainer.Labels["com.docker.compose.service"],
		Name:             dockerContainer.Names[0],
		Command:          make(chan ContainerCommand),
		Project:          project,
		containerUpdated: containerUpdated,
		cancel:           cancel,
		logger:           logger,
	}

	c.setRunningStateFromState(dockerContainer.State)

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
		}
	}
}

func (c *Container) Delete() {
	c.cancel()
}

func (c *Container) setRunningStateFromState(containerState apiContainer.ContainerState) {
	c.IsRunning = containerState == apiContainer.StateRunning
	c.tryPublish()
}

func (c *Container) SetRunningStateFromAction(action events.Action) {
	c.IsRunning = IsRunningFromAction(action)
	c.tryPublish()
}

func IsRunningFromAction(action events.Action) bool {
	return action == events.ActionCreate || action == events.ActionStart || action == events.ActionUnPause || action == events.ActionRestart || action == events.ActionReload || action == events.ActionExecStart || action == events.ActionExecDie || action == events.ActionExecCreate || action == events.ActionExecDetach
}

func (c *Container) Update(stats apiContainer.StatsResponse) {
	c.updateCPUPercent(stats.CPUStats, stats.PreCPUStats)
	c.updateMemoryPercentage(stats.MemoryStats)
	c.tryPublish()
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

// non-blocking publish; drops if no reader
func (c *Container) tryPublish() {
	if c.containerUpdated == nil {
		return
	}

	select {
	case c.containerUpdated <- dto.Container{
		ID:                    dto.ContainerID(c.ID),
		Project:               c.Project,
		Service:               c.Service,
		Name:                  c.Name,
		CPUPercentage:         c.CPUPercentage,
		MemoryUsagePercentage: c.MemoryPercentage,
		IsRunning:             c.IsRunning,
	}:
	default:
		c.logger.Warn("dropped container publish", "container", c.ID)
	}
}

// non-blocking publish; drops if no reader
func (c *Container) tryUnpublish() {
	if c.containerUpdated == nil {
		return
	}

	select {
	case c.containerUpdated <- dto.ContainerDeleted{
		ID: dto.ContainerID(c.ID),
	}:
	default:
		c.logger.Warn("dropped container unpublish", "container", c.ID)
	}
}
