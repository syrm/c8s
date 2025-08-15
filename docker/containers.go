package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/rivo/tview"
)

type Container struct {
	ID            string
	Name          string
	CPUPercentage float64
}

type Project struct {
	Name          string
	CPUPercentage float64
	RenderizedRow int
	Containers    []*Container
}

func NewProject(name string) *Project {
	return &Project{
		Name: name,
		RenderizedRow: -1,
	}
}

func (p *Project) RenderTo(app *tview.Application, table *tview.Table) {
	if p.RenderizedRow == -1 {
		p.RenderizedRow = table.GetRowCount()
	}

	p.CPUPercentage = 0
	for _, c := range p.Containers {
		p.CPUPercentage += c.CPUPercentage
	}

	app.QueueUpdateDraw(func() {
		table.SetCell(p.RenderizedRow, 0, tview.NewTableCell(p.Name))
		table.SetCell(p.RenderizedRow, 1, tview.NewTableCell(fmt.Sprintf("%.2f", p.CPUPercentage)))
	})
}

func NewContainer(c apiContainer.Summary) *Container {
	return &Container{
		ID:   c.ID,
		Name: c.Names[0],
	}
}

func (c *Container) updateCPUPercent(s apiContainer.StatsResponse) {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	var (
		cpuPercent = 0.0
		// calculate the change for the cpu usage of the container in between readings
		cpuDelta = float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
		// calculate the change for the entire system between readings
		systemDelta = float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
		onlineCPUs  = float64(s.CPUStats.OnlineCPUs)
	)

	if onlineCPUs == 0.0 {
		onlineCPUs = float64(len(s.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0.0 && cpuDelta > 0.0 {
		cpuPercent = (cpuDelta / systemDelta) * onlineCPUs * 100.0
	}

	c.CPUPercentage = cpuPercent
}

type Containers struct {
	Client *client.Client
}

func (cs *Containers) GetProjects(ctx context.Context, app *tview.Application, table *tview.Table) (map[string]*Project, error) {
	start := time.Now()
	containers, err := cs.Client.ContainerList(ctx, apiContainer.ListOptions{All: true})
	fmt.Printf("time containerList %v\n", time.Since(start))

	if err != nil {
		return nil, err
	}

	composeContainers := make(map[string]*Project)
	for _, dockerContainer := range containers {
		projectName, isProject := dockerContainer.Labels["com.docker.compose.project"]

		if !isProject {
			continue
		}

		c := NewContainer(dockerContainer)

		if dockerContainer.State != apiContainer.StateRunning {
			continue
		}

		if _, ok := composeContainers[projectName]; !ok {
			composeContainers[projectName] = NewProject(projectName)
			composeContainers[projectName].Containers = append(composeContainers[projectName].Containers, c)
		}
		
		go cs.getContainerStats(ctx, composeContainers[projectName], c, app, table)
	}

	return composeContainers, nil
}

func (cs *Containers) getContainerStats(ctx context.Context, p *Project, c *Container, app *tview.Application, table *tview.Table) {
	dockerContainerStats, err := cs.Client.ContainerStats(ctx, c.ID, true)
	if err != nil {
		// TODO log error
		return
	}

	defer dockerContainerStats.Body.Close()

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse
		err := dec.Decode(&stats)
		if err != nil {
			if err != io.EOF {
				// TODO log err
			}

			break
		}

		c.updateCPUPercent(stats)
		p.RenderTo(app, table)
	}
}
