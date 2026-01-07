package docker

import (
	"github.com/syrm/c8s/dto"
)

// containerResponseToDTO converts a ContainerResponse to dto.Container.
func containerResponseToDTO(c ContainerResponse) dto.Container {
	return dto.Container{
		ID:               c.ID,
		Project:          c.Project,
		Service:          c.Service,
		Name:             c.Name,
		CPUPercentage:    c.CPUPercentage,
		MemoryPercentage: c.MemoryPercentage,
		Status:           c.Status,
		PendingAction:    c.PendingAction,
	}
}
