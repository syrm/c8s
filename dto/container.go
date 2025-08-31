package dto

type ContainerID string

type Container struct {
	ID                    ContainerID
	ProjectID             ProjectID
	Service               string
	Name                  string
	CPUPercentage         float64
	MemoryUsagePercentage float64
}
