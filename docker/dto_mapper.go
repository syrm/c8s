package docker

import (
	"context"

	"github.com/syrm/c8s/dto"
)

// containerToDTO converts a Container to dto.Container.
func containerToDTO(c *Container, includeLogs bool, logCancel context.CancelFunc) dto.Container {
	d := dto.Container{
		ID:               dto.ContainerID(c.ID),
		Project:          c.Project,
		Service:          c.Service,
		Name:             c.Name,
		CPUPercentage:    c.CPUPercentage,
		MemoryPercentage: c.MemoryPercentage,
		Status:           c.Status,
		PendingAction:    c.PendingAction,
	}

	if includeLogs {
		d.Logs = c.Logs
		d.LogCancel = logCancel
	}

	return d
}

// containerResponseToDTO converts a ContainerResponse to dto.Container.
func containerResponseToDTO(c ContainerResponse) dto.Container {
	return dto.Container{
		ID:               dto.ContainerID(c.ID),
		Project:          c.Project,
		Service:          c.Service,
		Name:             c.Name,
		CPUPercentage:    c.CPUPercentage,
		MemoryPercentage: c.MemoryPercentage,
		Status:           c.Status,
		PendingAction:    c.PendingAction,
	}
}
