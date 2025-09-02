package docker

import (
	"log/slog"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/dto"
)

type ContainerValueType int

const (
	ContainerValueCPU ContainerValueType = iota
	ContainerValueMemory
	ContainerValueState
)

type ContainerID string

type ContainerValue struct {
	ID        ContainerID
	Type      ContainerValueType
	Value     float64
	IsRunning bool
}

type Container struct {
	ID               ContainerID
	Service          string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	IsRunning        bool
	valueUpdatedCh   chan<- ContainerValue
	containerUpdated chan<- dto.Container
	ProjectID        ProjectID
	logger           slog.Logger
}

func NewContainer(
	dockerContainer apiContainer.Summary,
	projectID ProjectID,
	valueUpdatedCh chan<- ContainerValue,
	containerUpdated chan<- dto.Container,
	logger slog.Logger,
) *Container {
	c := &Container{
		ID:               ContainerID(dockerContainer.ID),
		Service:          dockerContainer.Labels["com.docker.compose.service"],
		Name:             dockerContainer.Names[0],
		ProjectID:        projectID,
		valueUpdatedCh:   valueUpdatedCh,
		containerUpdated: containerUpdated,
		logger:           logger,
	}

	c.setRunningStateFromState(dockerContainer.State)
	return c
}

func (c *Container) setRunningStateFromState(containerState apiContainer.ContainerState) {
	c.IsRunning = containerState == apiContainer.StateRunning
	c.valueUpdatedCh <- ContainerValue{ID: c.ID, Type: ContainerValueState, IsRunning: c.IsRunning}
}

func (c *Container) SetRunningStateFromAction(action events.Action) {
	c.IsRunning = action == events.ActionStart || action == events.ActionUnPause || action == events.ActionRestart || action == events.ActionReload
	c.valueUpdatedCh <- ContainerValue{ID: c.ID, Type: ContainerValueState, IsRunning: c.IsRunning}
}

func (c *Container) Update(stats apiContainer.StatsResponse) {
	c.updateCPUPercent(stats.CPUStats, stats.PreCPUStats)
	c.updateMemoryPercentage(stats.MemoryStats)
	c.containerUpdated <- dto.Container{
		ID:                    dto.ContainerID(c.ID),
		ProjectID:             dto.ProjectID(c.ProjectID),
		Service:               c.Service,
		Name:                  c.Name,
		CPUPercentage:         c.CPUPercentage,
		MemoryUsagePercentage: c.MemoryPercentage,
	}
}

func (c *Container) updateMemoryPercentage(memoryStats apiContainer.MemoryStats) {
	memUsage := c.calculateMemUsageUnixNoCache(memoryStats)
	c.MemoryPercentage = c.calculateMemPercentUnixNoCache(float64(memoryStats.Limit), memUsage)
	c.valueUpdatedCh <- ContainerValue{ID: c.ID, Type: ContainerValueMemory, Value: c.MemoryPercentage}
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
	c.valueUpdatedCh <- ContainerValue{ID: c.ID, Type: ContainerValueCPU, Value: c.CPUPercentage}
}
