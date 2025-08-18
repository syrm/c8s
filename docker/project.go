package docker

import (
	"log/slog"

	"github/syrm/c8s/dto"
)

type ContainerValueType int

const (
	ContainerValueTypeCPU ContainerValueType = iota
	ContainerValueTypeMemory
)

type ContainerValue struct {
	ID    ContainerID
	Type  ContainerValueType
	Value float64
}

type Project struct {
	ID               string
	Name             string
	projectUpdateCh  chan<- dto.Project
	ContainersCPU    map[ContainerID]float64
	ContainersMemory map[ContainerID]float64
	ContainerValueCh chan ContainerValue
	Logger           *slog.Logger
}

func NewProject(id string, name string, projectUpdateCh chan<- dto.Project, logger *slog.Logger) *Project {
	return &Project{
		ID:               id,
		Name:             name,
		projectUpdateCh:  projectUpdateCh,
		ContainersCPU:    make(map[ContainerID]float64),
		ContainersMemory: make(map[ContainerID]float64),
		ContainerValueCh: make(chan ContainerValue),
		Logger:           logger,
	}
}

func (p *Project) GetContainerValueCh() chan ContainerValue {
	return p.ContainerValueCh
}

// HandleContainerValue is a blocking call that runs until ch is closed.
func (p *Project) HandleContainerValue() {
	for value := range p.ContainerValueCh {
		switch value.Type {
		case ContainerValueTypeCPU:
			p.ContainersCPU[value.ID] = value.Value

		case ContainerValueTypeMemory:
			p.ContainersMemory[value.ID] = value.Value
		}

		if value.Type == ContainerValueTypeCPU || value.Type == ContainerValueTypeMemory {
			dtoProject := dto.Project{
				ID:                    dto.ProjectID(p.ID),
				Name:                  p.Name,
				CPUPercentage:         p.cpuPercentage(),
				MemoryUsagePercentage: p.MemoryPercentage(),
				ContainersRunning:     0,
				ContainersTotal:       0,
			}

			p.Logger.Info(
				"Project updated",
				slog.String("project_id", p.ID),
				slog.Float64("cpu", dtoProject.CPUPercentage),
				slog.Float64("memory", dtoProject.MemoryUsagePercentage),
			)

			p.projectUpdateCh <- dtoProject
		}
	}

}

func (p *Project) cpuPercentage() float64 {
	var totalCPU float64
	for _, cpu := range p.ContainersCPU {
		totalCPU += cpu
	}
	return totalCPU
}

func (p *Project) MemoryPercentage() float64 {
	var totalMemory float64
	for _, memory := range p.ContainersMemory {
		totalMemory += memory
	}
	return totalMemory
}
