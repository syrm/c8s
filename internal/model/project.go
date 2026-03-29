package model

import "maps"

// ProjectID is a unique identifier for a Docker Compose project.
type ProjectID string

// Project represents a Docker Compose project with aggregated container metrics.
// Note: The internal maps (ContainersCPU, ContainersMemory, ContainersState) should be
// treated as immutable after creation. Use Copy() if you need to pass a Project to
// another goroutine safely.
type Project struct {
	ID                ProjectID
	Name              string
	CPUPercentage     float64
	MemoryPercentage  float64
	ContainersRunning int
	ContainersCPU     map[ContainerID]float64
	ContainersMemory  map[ContainerID]float64
	ContainersState   map[ContainerID]string
}

// Copy returns a deep copy of the Project with all maps copied.
// This ensures thread-safe access when passing Project between goroutines.
func (p Project) Copy() Project {
	return Project{
		ID:                p.ID,
		Name:              p.Name,
		CPUPercentage:     p.CPUPercentage,
		MemoryPercentage:  p.MemoryPercentage,
		ContainersRunning: p.ContainersRunning,
		ContainersCPU:     maps.Clone(p.ContainersCPU),
		ContainersMemory:  maps.Clone(p.ContainersMemory),
		ContainersState:   maps.Clone(p.ContainersState),
	}
}
