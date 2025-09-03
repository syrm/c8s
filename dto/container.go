package dto

type ContainerID string

type Container struct {
	ID                    ContainerID
	Project               Project
	Service               string
	Name                  string
	CPUPercentage         float64
	MemoryUsagePercentage float64
	IsRunning             bool
	Deleted               bool
}
