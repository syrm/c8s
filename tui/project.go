package tui

import (
	"github.com/syrm/c8s/dto"
)

type ProjectID string

type Project struct {
	ID                    ProjectID
	Name                  string
	CPUPercentage         float64
	MemoryUsagePercentage float64
	ContainersRunning     int
	ContainersCPU         map[dto.ContainerID]float64
	ContainersMemory      map[dto.ContainerID]float64
	ContainersState       map[dto.ContainerID]bool
}
