package docker

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/syrm/c8s/internal/model"
)

// handleRequests processes TUI requests in a dedicated goroutine.
func (d *Docker) handleRequests(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleRequests context is done")
			return
		case req, ok := <-d.requestData:
			if !ok {
				// Channel closed, exit gracefully
				d.logger.DebugContext(ctx, "handleRequests channel closed")
				return
			}
			switch r := req.(type) {

			case *model.RequestContainerLog:
				d.handleRequestContainerLog(ctx, r)

			case *model.RequestProject:
				d.handleRequestContainerProject(ctx, r)

			case *model.RequestSetPendingAction:
				d.handleRequestSetPendingAction(ctx, r)

			case *model.RequestProjectList:
				d.handleRequestProjectList(ctx, r)

			case *model.RequestStopLogCollection:
				d.handleRequestStopLogCollection(ctx, r)

			default:
				d.logger.Warn("unknown request type", slog.String("type", fmt.Sprintf("%T", req)))
			}
		}
	}
}

func (d *Docker) handleRequestContainerLog(ctx context.Context, r *model.RequestContainerLog) {
	c := d.getContainer(r.ContainerID)
	if c == nil {
		r.Response <- model.Container{}
		return
	}

	snap := c.Snapshot()

	// Check and set log collection flag atomically
	c.mu.Lock()
	needStart := !c.LogCollectionActive
	if needStart {
		c.LogCollectionActive = true
	}
	c.mu.Unlock()

	if needStart {
		d.startLogCollection(c, r.LogChan)
	}

	r.Response <- snap
}

func (d *Docker) handleRequestContainerProject(ctx context.Context, r *model.RequestProject) {
	containers := d.getContainersList(func(c *Container) bool {
		return r.ProjectID == c.Project.ID
	})

	result := make([]model.Container, 0, len(containers))
	for _, c := range containers {
		result = append(result, c.Snapshot())
	}

	r.Response <- result
}

func (d *Docker) handleRequestSetPendingAction(ctx context.Context, r *model.RequestSetPendingAction) {
	c := d.getContainer(r.ContainerID)
	if c == nil {
		r.Response <- false
		return
	}

	c.SetPendingAction(r.PendingAction)
	r.Response <- true
}

func (d *Docker) handleRequestProjectList(ctx context.Context, r *model.RequestProjectList) {
	containers := d.getContainersList(nil)
	projects := make(map[model.ProjectID]model.Project)

	for _, c := range containers {
		snap := c.Snapshot()

		projectID := snap.Project.ID
		project, projectExist := projects[projectID]

		if !projectExist {
			project = model.Project{
				ID:               projectID,
				Name:             snap.Project.Name,
				ContainersCPU:    make(map[model.ContainerID]float64),
				ContainersMemory: make(map[model.ContainerID]float64),
				ContainersState:  make(map[model.ContainerID]string),
			}
		}

		project.CPUPercentage += snap.CPUPercentage
		project.ContainersCPU[snap.ID] = snap.CPUPercentage
		project.MemoryPercentage += snap.MemoryPercentage
		project.ContainersMemory[snap.ID] = snap.MemoryPercentage

		isRunning := 0
		if snap.Status == model.StatusRunning {
			isRunning = 1
		}

		project.ContainersRunning += isRunning
		project.ContainersState[snap.ID] = snap.Status

		projects[projectID] = project
	}

	r.Response <- slices.Collect(maps.Values(projects))
}

func (d *Docker) handleRequestStopLogCollection(ctx context.Context, r *model.RequestStopLogCollection) {
	d.logContextsLock.Lock()
	defer d.logContextsLock.Unlock()

	if cancel, exists := d.logContexts[r.ContainerID]; exists {
		cancel()
		delete(d.logContexts, r.ContainerID)
		d.logger.DebugContext(ctx, "stopped log collection", slog.String("container_id", string(r.ContainerID)))
	}
}

// stopStatsCollection stops stats collection for a container.
// This function ensures the stats goroutine is properly cancelled and cleaned up.
func (d *Docker) stopStatsCollection(containerID model.ContainerID) {
	d.statsContextsLock.Lock()
	defer d.statsContextsLock.Unlock()

	if cancel, exists := d.statsContexts[containerID]; exists {
		cancel()
		delete(d.statsContexts, containerID)
		d.logger.Debug("stopped stats collection", slog.String("container_id", string(containerID)))
	}
}

// startLogCollection starts log collection for a container if not already active.
func (d *Docker) startLogCollection(c *Container, logChan chan<- []string) {
	d.logContextsLock.Lock()
	// Cancel any existing log collection for this container
	if oldCancel, exists := d.logContexts[c.ID]; exists {
		oldCancel()
		delete(d.logContexts, c.ID)
	}
	// Create new log context using parentCtx to prevent cascading cancellations
	// Use getParentCtx() for thread-safe access
	ctxLog, cancel := context.WithCancel(d.getParentCtx())
	d.logContexts[c.ID] = cancel
	d.logContextsLock.Unlock()

	// Start log collection goroutine with WaitGroup tracking
	d.logCollectorsWG.Add(1)
	go func() {
		defer d.logCollectorsWG.Done()
		d.collectContainerLogs(ctxLog, c, logChan)
	}()
}
