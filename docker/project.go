package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/syrm/c8s/dto"
)

type ProjectID string

type Project struct {
	ID                    ProjectID
	Name                  string
	projectUpdated        chan<- dto.Project
	updatedContainerValue chan ContainerValue
	Containers            []*Container
	ContainersCPU         map[ContainerID]float64
	ContainersMemory      map[ContainerID]float64
	ContainersState       map[ContainerID]bool
	logger                *slog.Logger
}

func NewProject(id ProjectID, name string, projectUpdated chan<- dto.Project, updatedContainerValue chan ContainerValue, logger *slog.Logger) *Project {
	return &Project{
		ID:                    id,
		Name:                  name,
		projectUpdated:        projectUpdated,
		updatedContainerValue: updatedContainerValue,
		ContainersCPU:         make(map[ContainerID]float64),
		ContainersMemory:      make(map[ContainerID]float64),
		ContainersState:       make(map[ContainerID]bool),
		Containers:            []*Container{},
		logger:                logger,
	}
}

func (p *Project) GetUpdatedContainerValue() chan<- ContainerValue {
	return p.updatedContainerValue
}

// HandleContainerValue is a blocking call that runs until ch is closed.
func (p *Project) HandleContainerValue(ctx context.Context) {
	p.tryPublishProject()

	tickerUpdate := time.NewTicker(2 * time.Second)

	for {
		select {
		case value := <-p.updatedContainerValue:
			switch value.Type {
			case ContainerValueCPU:
				p.ContainersCPU[value.ID] = value.Value

			case ContainerValueMemory:
				p.ContainersMemory[value.ID] = value.Value

			case ContainerValueState:
				p.ContainersState[value.ID] = value.IsRunning

			}

		case <-tickerUpdate.C:
			p.tryPublishProject()

		case <-ctx.Done():
			p.logger.DebugContext(ctx, "HandleContainerValue context is done")
			return
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

// non-blocking publish; drops if no reader
func (p *Project) tryPublishProject() {
	if p.projectUpdated == nil {
		return
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

	select {
	case p.projectUpdated <- dtoProject:
	default:
		p.logger.Warn("dropped project publish", "project", p.ID, "type")
	}
}
