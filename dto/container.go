package dto

type ContainerID string

type ContainerDeletable interface {
	Deleted() bool
}

type Container struct {
	ID                    ContainerID
	Project               Project
	Service               string
	Name                  string
	CPUPercentage         float64
	MemoryUsagePercentage float64
	IsRunning             bool
}

func (c Container) Deleted() bool {
	return false
}

type ContainerDeleted struct {
	ID ContainerID
}

func (c ContainerDeleted) Deleted() bool {
	return true
}
