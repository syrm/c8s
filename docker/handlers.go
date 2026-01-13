package docker

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/syrm/c8s/dto"
	ch "github.com/syrm/c8s/internal/channel"
	"github.com/syrm/c8s/internal/timer"
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

			case *dto.RequestContainerLog:
				d.handleRequestContainerLog(ctx, r)

			case *dto.RequestProject:
				d.handleRequestContainerProject(ctx, r)

			case *dto.RequestSetPendingAction:
				d.handleRequestSetPendingAction(ctx, r)

			case *dto.RequestProjectList:
				d.handleRequestProjectList(ctx, r)

			case *dto.RequestStopLogCollection:
				d.handleRequestStopLogCollection(ctx, r)

			default:
				d.logger.Warn("unknown request type", slog.String("type", fmt.Sprintf("%T", req)))
			}
		}
	}
}

// logRequestResult holds the combined result from container log request functor.
type logRequestResult struct {
	dto       dto.Container
	needStart bool
}

func (d *Docker) handleRequestContainerLog(ctx context.Context, r *dto.RequestContainerLog) {
	// Helper to send empty response
	sendEmpty := func() { r.Response <- dto.Container{} }

	// Step 1: Get container from map
	c := d.getContainer(ctx, r.ContainerID)
	if c == nil {
		sendEmpty()
		return
	}

	// Step 2: Get container state and logs via serialized command
	// Use single channel for combined result to avoid ordering issues
	resultChan := make(chan logRequestResult, 1)

	cmd := ContainerCommand{
		functor: func(container *Container) {
			if container.deleted.Load() {
				resultChan <- logRequestResult{}
				return
			}

			// Build DTO with logs copy
			dtoContainer := dto.Container{
				ID:               container.ID,
				Project:          container.Project,
				Service:          container.Service,
				Name:             container.Name,
				CPUPercentage:    container.CPUPercentage,
				MemoryPercentage: container.MemoryPercentage,
				Status:           container.Status,
				PendingAction:    container.PendingAction,
				Logs:             container.GetLogsCopy(),
			}

			// Check and set log collection flag atomically
			needStart := !container.LogCollectionActive
			if needStart {
				container.LogCollectionActive = true
			}

			resultChan <- logRequestResult{dto: dtoContainer, needStart: needStart}
		},
	}

	if ch.Send(ctx, c.Command, cmd, dto.ChannelTimeout) != ch.SendOK {
		d.logger.Warn("timeout sending to container command in handleRequestContainerLog")
		sendEmpty()
		return
	}

	// Step 3: Wait for result
	result, recvResult := ch.Receive(ctx, resultChan, dto.ChannelTimeout)
	if recvResult != ch.ReceiveOK {
		d.logger.Warn("timeout waiting for result in handleRequestContainerLog")
		sendEmpty()
		return
	}

	// Step 4: Start log collection if needed
	if result.needStart {
		d.startLogCollection(c)
	}

	r.Response <- result.dto
}

func (d *Docker) handleRequestContainerProject(ctx context.Context, r *dto.RequestProject) {
	// Get containers matching the project filter
	containers := d.getContainersList(ctx, func(c *Container) bool {
		return r.ProjectID == c.Project.ID
	})

	if containers == nil {
		r.Response <- nil
		return
	}

	// Query each container for its data
	result := make([]dto.Container, 0, len(containers))
	for _, c := range containers {
		response := make(chan ContainerResponse, 1)

		cmd := ContainerCommand{response: response}
		if ch.Send(ctx, c.Command, cmd, dto.ChannelTimeout) != ch.SendOK {
			continue // Skip this container if timeout
		}

		container, recvResult := ch.Receive(ctx, response, dto.ChannelTimeout)
		if recvResult == ch.ReceiveOK {
			result = append(result, containerResponseToDTO(container))
		}
	}

	r.Response <- result
}

func (d *Docker) handleRequestSetPendingAction(ctx context.Context, r *dto.RequestSetPendingAction) {
	// Get the container reference
	c := d.getContainer(ctx, r.ContainerID)
	if c == nil {
		r.Response <- false
		return
	}

	// Send command to container
	cmd := ContainerCommand{
		functor: func(container *Container) {
			container.SetPendingAction(r.PendingAction)
		},
	}

	if ch.Send(ctx, c.Command, cmd, dto.ChannelTimeout) == ch.SendOK {
		r.Response <- true
	} else {
		d.logger.Warn("timeout sending to container command in handleRequestSetPendingAction")
		r.Response <- false
	}
}

func (d *Docker) handleRequestProjectList(ctx context.Context, r *dto.RequestProjectList) {
	// Get all containers
	containers := d.getContainersList(ctx, nil)
	if containers == nil {
		r.Response <- nil
		return
	}

	// Query each container for its data
	projects := make(map[dto.ProjectID]dto.Project)

	for _, c := range containers {
		response := make(chan ContainerResponse, 1)

		cmd := ContainerCommand{response: response}
		if ch.Send(ctx, c.Command, cmd, dto.ChannelTimeout) != ch.SendOK {
			continue // Skip this container if timeout
		}

		container, recvResult := ch.Receive(ctx, response, dto.ChannelTimeout)
		if recvResult != ch.ReceiveOK {
			continue // Skip this container if timeout
		}

		// Aggregate project data
		projectID := container.Project.ID
		project, projectExist := projects[projectID]

		if !projectExist {
			project = dto.Project{
				ID:               projectID,
				Name:             container.Project.Name,
				ContainersCPU:    make(map[dto.ContainerID]float64),
				ContainersMemory: make(map[dto.ContainerID]float64),
				ContainersState:  make(map[dto.ContainerID]string),
			}
		}

		project.CPUPercentage += container.CPUPercentage
		project.ContainersCPU[container.ID] = container.CPUPercentage
		project.MemoryPercentage += container.MemoryPercentage
		project.ContainersMemory[container.ID] = container.MemoryPercentage

		isRunning := 0
		if container.Status == dto.StatusRunning {
			isRunning = 1
		}

		project.ContainersRunning += isRunning
		project.ContainersState[container.ID] = container.Status

		projects[projectID] = project
	}

	r.Response <- slices.Collect(maps.Values(projects))
}

func (d *Docker) handleRequestStopLogCollection(ctx context.Context, r *dto.RequestStopLogCollection) {
	d.logContextsLock.Lock()
	defer d.logContextsLock.Unlock()

	if cancel, exists := d.logContexts[r.ContainerID]; exists {
		cancel()
		delete(d.logContexts, r.ContainerID)
		d.logger.DebugContext(ctx, "stopped log collection", slog.String("container_id", string(r.ContainerID)))
	}
}

// startLogCollection starts log collection for a container if not already active.
func (d *Docker) startLogCollection(c *Container) {
	d.logContextsLock.Lock()
	// Cancel any existing log collection for this container
	if oldCancel, exists := d.logContexts[c.ID]; exists {
		oldCancel()
		delete(d.logContexts, c.ID)
	}
	// Create new log context using parentCtx to prevent cascading cancellations
	ctxLog, cancel := context.WithCancel(d.parentCtx)
	d.logContexts[c.ID] = cancel
	d.logContextsLock.Unlock()

	// Start log collection goroutine with WaitGroup tracking
	d.logCollectorsWG.Add(1)
	go func() {
		defer d.logCollectorsWG.Done()
		d.collectContainerLogs(ctxLog, c)
	}()
}

// resetLogCollectionFlag resets the LogCollectionActive flag on a container.
func (d *Docker) resetLogCollectionFlag(c *Container) {
	resetTimer := time.NewTimer(time.Second)
	defer timer.Stop(resetTimer)
	select {
	case c.Command <- ContainerCommand{
		functor: func(container *Container) {
			container.LogCollectionActive = false
		},
	}:
	case <-resetTimer.C:
		// Timeout waiting to reset flag, container might be deleted
	}
}
