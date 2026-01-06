package dto

import "context"

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
	Logs             []string
	Status           string
	PendingAction    string // "starting", "stopping", "restarting", "removing" or ""
	LogCancel        context.CancelFunc
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
