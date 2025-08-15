package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
	"os"
	"log"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/rivo/tview"
)

type Container struct {
	ID               string
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	IsRunning        bool
	ProjectID string
}

type Project struct {
	Name             string
	CPUPercentage    float64
	MemoryPercentage float64
	RenderizedRow    int
	Containers       []*Container
}


var logger *log.Logger

func Init() {
	f, _ := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	logger = log.New(f, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)	
}

func NewProject(name string, app *tview.Application, table *tview.Table) *Project {
	p := &Project{
		Name:          name,
		RenderizedRow: table.GetRowCount(),
	}

	table.SetCell(p.RenderizedRow, 0, tview.NewTableCell(p.Name))

	return p
}

func (p *Project) RenderTo(app *tview.Application, table *tview.Table) {
	p.CPUPercentage = 0
	p.MemoryPercentage = 0
	containersRunning := 0
	for _, c := range p.Containers {
		p.CPUPercentage += c.CPUPercentage
		p.MemoryPercentage += c.MemoryPercentage
		if c.IsRunning {
			containersRunning += 1
		}
	}

	app.QueueUpdateDraw(func() {
		table.SetCell(p.RenderizedRow, 0, tview.NewTableCell(p.Name))
		table.SetCell(p.RenderizedRow, 1, tview.NewTableCell(fmt.Sprintf("%.2f%%", p.CPUPercentage)).SetAlign(tview.AlignRight))
		table.SetCell(p.RenderizedRow, 2, tview.NewTableCell(fmt.Sprintf("%.2f%%", p.MemoryPercentage)).SetAlign(tview.AlignRight))
		table.SetCell(p.RenderizedRow, 3, tview.NewTableCell(fmt.Sprintf("%d/%d", containersRunning, len(p.Containers))).SetAlign(tview.AlignRight))
	})
}

func NewContainer(c apiContainer.Summary, projectID string) *Container {
	return &Container{
		ID:        c.ID,
		Name:      c.Names[0],
		IsRunning: c.State == apiContainer.StateRunning,
		ProjectID: projectID,
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

func (c *Container) calculateMemUsageUnixNoCache(mem apiContainer.MemoryStats) float64 {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	// cgroup v1
	if v, isCgroup1 := mem.Stats["total_inactive_file"]; isCgroup1 && v < mem.Usage {
		return float64(mem.Usage - v)
	}
	// cgroup v2
	if v := mem.Stats["inactive_file"]; v < mem.Usage {
		return float64(mem.Usage - v)
	}
	return float64(mem.Usage)
}

func (c *Container) calculateMemPercentUnixNoCache(limit float64, usedNoCache float64) float64 {
	// https://github.com/docker/cli/blob/master/cli/command/container/stats_helpers.go
	if limit != 0 {
		return usedNoCache / limit * 100.0
	}
	return 0
}

type Containers struct {
	Client *client.Client
}

func (cs *Containers) GetProjects(ctx context.Context, app *tview.Application, table *tview.Table) (map[string]*Project, error) {
	start := time.Now()
	dockerContainers, err := cs.Client.ContainerList(ctx, apiContainer.ListOptions{All: true})
	fmt.Printf("time containerList %v\n", time.Since(start))

	if err != nil {
		return nil, err
	}

	composeContainers := make(map[string]*Project)
	containers := make(map[string]*Container, len(dockerContainers))
	for _, dockerContainer := range dockerContainers {
		projectID, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

		if !isProject {
			continue
		}

		c := NewContainer(dockerContainer, projectID)
		containers[c.ID] = c

		if _, ok := composeContainers[projectID]; !ok {
			composeContainers[projectID] = NewProject(dockerContainer.Labels["com.docker.compose.project"], app, table)
		}
		composeContainers[projectID].Containers = append(composeContainers[projectID].Containers, c)

		go cs.getContainerStats(ctx, composeContainers[projectID], c, app, table)
	}

		f := filters.NewArgs()
		f.Add("type", "container")
		msgs, errs := cs.Client.Events(ctx, events.ListOptions{Filters: f})
		go cs.handleEvents(composeContainers, containers, app, table, msgs, errs)

	return composeContainers, nil
}

func (cs *Containers) getContainerStats(ctx context.Context, p *Project, c *Container, app *tview.Application, table *tview.Table) {
	dockerContainerStats, err := cs.Client.ContainerStats(ctx, c.ID, true)
	if err != nil {
		println(err.Error())
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
				println(err.Error())
				// TODO log err
			}

			break
		}

		memUsage := c.calculateMemUsageUnixNoCache(stats.MemoryStats)
		c.MemoryPercentage = c.calculateMemPercentUnixNoCache(float64(stats.MemoryStats.Limit), memUsage)
		c.updateCPUPercent(stats)
		p.RenderTo(app, table)
	}
}

func (cs *Containers) handleEvents(projects map[string]*Project, containers map[string]*Container, app *tview.Application, table *tview.Table, msgs <-chan events.Message, errs <-chan error) {
	for {
		select {
		case msg := <-msgs:
    		logger.Println("action", msg.Action)
			isRunning := msg.Action == events.ActionStart || msg.Action == events.ActionUnPause || msg.Action == events.ActionRestart || msg.Action == events.ActionReload
			c := containers[msg.Actor.ID] 
			c.IsRunning = isRunning
			p := projects[c.ProjectID]
			p.RenderTo(app, table)
		case err := <-errs:
			panic(err)
		}
	}
}
