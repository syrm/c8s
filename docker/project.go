package docker

import (
	"log/slog"

	apiContainer "github.com/docker/docker/api/types/container"

	"github/syrm/c8s/dto"
)

type ContainerValueType int

const (
	ContainerValueTypeCPU ContainerValueType = iota
	ContainerValueTypeMemory
	ContainerValueState
)

type ContainerValue struct {
	ID        ContainerID
	Type      ContainerValueType
	Value     float64
	IsRunning bool
	Stats     apiContainer.StatsResponse
}

type Project struct {
	ID                      string
	Name                    string
	projectUpdateCh         chan<- dto.Project
	ContainersCPU           map[ContainerID]float64
	ContainersMemory        map[ContainerID]float64
	ContainersState         map[ContainerID]bool
	ContainerValueUpdatedCh chan ContainerValue
	Logger                  *slog.Logger
}

func NewProject(id string, name string, projectUpdateCh chan<- dto.Project, logger *slog.Logger) *Project {
	return &Project{
		ID:                      id,
		Name:                    name,
		projectUpdateCh:         projectUpdateCh,
		ContainersCPU:           make(map[ContainerID]float64),
		ContainersMemory:        make(map[ContainerID]float64),
		ContainersState:         make(map[ContainerID]bool),
		ContainerValueUpdatedCh: make(chan ContainerValue),
		Logger:                  logger,
	}
}

func (p *Project) GetContainerValueUpdatedCh() chan ContainerValue {
	return p.ContainerValueUpdatedCh
}

// HandleContainerValue is a blocking call that runs until ch is closed.
func (p *Project) HandleContainerValue() {
	for value := range p.ContainerValueUpdatedCh {
		switch value.Type {
		case ContainerValueTypeCPU:
			p.ContainersCPU[value.ID] = value.Value

		case ContainerValueTypeMemory:
			p.ContainersMemory[value.ID] = value.Value

		case ContainerValueState:
			p.ContainersState[value.ID] = value.Value == 1
		}

		containerRunning := 0
		for _, isRunning := range p.ContainersState {
			if isRunning {
				containerRunning++
			}
		}

		//if containerRunning == 0 {
		//	// No containers running, skip update
		//	continue
		//}

		dtoProject := dto.Project{
			ID:                    dto.ProjectID(p.ID),
			Name:                  p.Name,
			CPUPercentage:         p.cpuPercentage(),
			MemoryUsagePercentage: p.MemoryPercentage(),
			ContainersRunning:     containerRunning,
			ContainersTotal:       len(p.ContainersState),
		}

		//p.Logger.Info(
		//	"Project updated",
		//	slog.String("project_id", p.ID),
		//	slog.Float64("cpu", dtoProject.CPUPercentage),
		//	slog.Float64("memory", dtoProject.MemoryUsagePercentage),
		//	slog.Int("container_running", containerRunning),
		//	slog.Int("container_total", len(p.ContainersState)),
		//)

		p.projectUpdateCh <- dtoProject
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
