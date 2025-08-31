package docker

import (
	"log/slog"

	apiContainer "github.com/docker/docker/api/types/container"
)

type ContainerID string

type Container struct {
	ID               ContainerID
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	IsRunning        bool
	ProjectID        string
	valueUpdatedCh   chan<- ContainerValue
	updateValueCh    chan ContainerValue
	logger           *slog.Logger
}

//
//UpdateValue on envoie un message dans ce channel pour dire au container de mettre a jour cette valeur
//UpdatedValue le container pousse dans ce channel a d'autres personnes la valeur qu'il a mis a jour

func NewContainer(
	dockerContainer apiContainer.Summary,
	projectID string,
	valueUpdatedCh chan ContainerValue,
	logger *slog.Logger,
) *Container {
	c := &Container{
		ID:             ContainerID(dockerContainer.ID),
		Name:           dockerContainer.Names[0],
		ProjectID:      projectID,
		valueUpdatedCh: valueUpdatedCh,
		updateValueCh:  make(chan ContainerValue),
		logger:         logger,
	}

	return c
}

func (c *Container) ValueUpdated() chan<- ContainerValue {
	return c.updateValueCh
}

func (c *Container) updateState(isRunning bool) {
	c.IsRunning = isRunning
	c.valueUpdatedCh <- ContainerValue{
		ID:        c.ID,
		Type:      ContainerValueState,
		IsRunning: c.IsRunning,
	}
}

func (c *Container) updateMemoryPercentage(memoryStats apiContainer.MemoryStats) {
	memUsage := c.calculateMemUsageUnixNoCache(memoryStats)
	c.MemoryPercentage = c.calculateMemPercentUnixNoCache(float64(memoryStats.Limit), memUsage)

	if c.IsRunning {
		c.valueUpdatedCh <- ContainerValue{
			ID:    c.ID,
			Type:  ContainerValueTypeMemory,
			Value: c.MemoryPercentage,
		}
	}
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

	if c.IsRunning {
		c.valueUpdatedCh <- ContainerValue{
			ID:    c.ID,
			Type:  ContainerValueTypeCPU,
			Value: c.CPUPercentage,
		}
	}
}

// HandleUpdateValue is a blocking call that runs until ch is closed.
func (c *Container) HandleUpdateValue() {
	for value := range c.updateValueCh {
		switch value.Type {
		case ContainerValueTypeCPU:
			c.updateCPUPercent(value.Stats.CPUStats, value.Stats.PreCPUStats)

		case ContainerValueTypeMemory:
			c.updateMemoryPercentage(value.Stats.MemoryStats)

		case ContainerValueState:
			c.updateState(value.IsRunning)
		}
	}
}
