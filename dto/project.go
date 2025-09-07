package dto

type ProjectID string

type Project struct {
	ID                ProjectID
	Name              string
	CPUPercentage     float64
	MemoryPercentage  float64
	ContainersRunning int
	ContainersCPU     map[ContainerID]float64
	ContainersMemory  map[ContainerID]float64
	ContainersState   map[ContainerID]bool
}
