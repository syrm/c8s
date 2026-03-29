package docker

import (
	"context"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"

	"github.com/syrm/c8s/internal/model"
	"github.com/syrm/c8s/internal/timer"
)

// backoffDurations defines exponential backoff intervals for reconnection attempts.
var backoffDurations = []time.Duration{
	100 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
}

// handleEventsWithBackoff wraps handleEvents with exponential backoff for reconnection.
// If the events stream fails, it will retry with increasing delays.
func (d *Docker) handleEventsWithBackoff(ctx context.Context) {
	attempt := 0
	for {
		d.handleEvents(ctx)

		// Check if context was cancelled (graceful shutdown)
		if ctx.Err() != nil {
			return
		}

		// Calculate backoff duration
		backoffIdx := attempt
		if backoffIdx >= len(backoffDurations) {
			backoffIdx = len(backoffDurations) - 1
		}
		backoff := backoffDurations[backoffIdx]

		d.logger.WarnContext(ctx, "Docker events stream ended, reconnecting",
			slog.Int("attempt", attempt+1),
			slog.Duration("backoff", backoff))

		// Wait before reconnecting
		backoffTimer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop(backoffTimer)
			return
		case <-backoffTimer.C:
		}

		attempt++
	}
}

func (d *Docker) handleEvents(ctx context.Context) {
	f := filters.NewArgs()
	f.Add("type", "container")
	msgs, errs := d.client.Events(ctx, events.ListOptions{Filters: f})

	d.logger.DebugContext(ctx, "handleEvents")

	for {
		select {
		case msg, ok := <-msgs:
			if !ok {
				d.logger.DebugContext(ctx, "events channel closed")
				return
			}
			d.processEvent(ctx, msg)

		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleEvents context is done")
			return

		case err, ok := <-errs:
			if !ok {
				d.logger.DebugContext(ctx, "error channel closed")
				return
			}
			if err != nil {
				d.logger.ErrorContext(ctx, "event", slog.Any("error", err))
			}
		}
	}
}

// processEvent handles a single Docker event.
func (d *Docker) processEvent(ctx context.Context, msg events.Message) {
	d.logger.DebugContext(ctx, "event",
		slog.String("action", string(msg.Action)),
		slog.String("container_id", msg.Actor.ID))

	// Get container from map
	c := d.getContainer(ctx, model.ContainerID(msg.Actor.ID))

	if c != nil {
		d.handleExistingContainerEvent(ctx, c, msg)
		return
	}

	// Container doesn't exist, create it
	d.createContainerFromEvent(ctx, msg)
}

// handleExistingContainerEvent processes events for existing containers.
func (d *Docker) handleExistingContainerEvent(ctx context.Context, c *Container, msg events.Message) {
	// Update container status
	c.SetStatusFromAction(msg.Action)

	// Handle destroy event
	if msg.Action == events.ActionDestroy {
		d.handleContainerDestroy(ctx, c)
		return
	}

	// Restart stats streaming when container starts or unpauses
	if msg.Action == events.ActionStart || msg.Action == events.ActionUnPause {
		d.restartStatsCollection(ctx, c)
	}
}

// handleContainerDestroy handles container destruction.
func (d *Docker) handleContainerDestroy(ctx context.Context, c *Container) {
	// Cancel the stats goroutine before deleting the container
	d.statsContextsLock.Lock()
	if statsCancel, exists := d.statsContexts[c.ID]; exists {
		statsCancel()
		delete(d.statsContexts, c.ID)
	}
	d.statsContextsLock.Unlock()

	// Cancel the log collection goroutine before deleting the container
	d.logContextsLock.Lock()
	if logCancel, exists := d.logContexts[c.ID]; exists {
		logCancel()
		delete(d.logContexts, c.ID)
	}
	d.logContextsLock.Unlock()

	// Remove container from map
	d.removeContainer(ctx, c.ID)
	c.Delete()
}

// restartStatsCollection restarts stats collection for a container.
func (d *Docker) restartStatsCollection(ctx context.Context, c *Container) {
	d.statsContextsLock.Lock()
	// Cancel old stats goroutine if it exists
	if oldCancel, exists := d.statsContexts[c.ID]; exists {
		oldCancel()
	}
	// Increment stats generation to invalidate old stats goroutines
	newGen := c.statsGen.Add(1)
	// Create new context for stats goroutine using parentCtx
	// to prevent cascading cancellations from errgroup failures
	// Use getParentCtx() for thread-safe access.
	statsCtx, statsCancel := context.WithCancel(d.getParentCtx())
	d.statsContexts[c.ID] = statsCancel
	d.statsContextsLock.Unlock()

	d.statsWG.Add(1)
	go func(gen uint64) {
		defer d.statsWG.Done()
		d.getContainerStatsRealtime(statsCtx, c, gen)
	}(newGen)
}

// createContainerFromEvent creates a new container from a Docker event.
func (d *Docker) createContainerFromEvent(ctx context.Context, msg events.Message) {
	d.createContainer(
		ctx,
		containerSummaryFromEvent(msg),
		msg.Action,
	)
}