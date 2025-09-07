package dto

type ContainerID string

type ContainerDeletable interface {
	Deleted() bool
}

type Container struct {
	ID               ContainerID
	Project          ContainerProject
	Service          string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	IsRunning        bool
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

type ContainerProject struct {
	ID   ProjectID
	Name string
}
