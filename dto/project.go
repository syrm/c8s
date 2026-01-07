package dto

// ProjectID is a unique identifier for a Docker Compose project.
type ProjectID string

// Project represents a Docker Compose project with aggregated container metrics.
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
