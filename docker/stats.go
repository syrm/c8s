package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"

	"github.com/syrm/c8s/internal/model"
)

func (d *Docker) collectContainers(ctx context.Context) error {
	listCtx, cancel := context.WithTimeout(ctx, d.cfg.DockerTimeout)
	defer cancel()

	dockerContainers, err := d.client.ContainerList(listCtx, apiContainer.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("listing containers: %w", err)
	}

	d.logger.DebugContext(ctx, "CollectContainers started")

	for _, dockerContainer := range dockerContainers {
		d.createContainer(ctx, dockerContainer, events.ActionCreate)
	}

	d.logger.DebugContext(ctx, "CollectContainers is done")

	return nil
}

// isValidProjectID validates that a project ID (working directory path) is reasonable.
// It checks that the path is not empty and doesn't contain null bytes or other suspicious characters.
func isValidProjectID(projectID string) bool {
	if projectID == "" {
		return false
	}
	// Check for null bytes or control characters (potential injection)
	for _, c := range projectID {
		if c == 0 || (c < 32 && c != '\t') {
			return false
		}
	}
	return true
}

func (d *Docker) createContainer(ctx context.Context, dockerContainer apiContainer.Summary, action events.Action) {
	projectIDraw, isProject := dockerContainer.Labels["com.docker.compose.project.working_dir"]

	if !isProject {
		return
	}

	// Validate project ID before use
	if !isValidProjectID(projectIDraw) {
		d.logger.WarnContext(ctx, "invalid project ID, skipping container",
			slog.String("container_id", dockerContainer.ID),
			slog.String("project_id", projectIDraw))
		return
	}

	project := model.ContainerProject{
		ID:   model.ProjectID(projectIDraw),
		Name: dockerContainer.Labels["com.docker.compose.project"],
	}

	// Check if container already exists
	c := d.getContainer(model.ContainerID(dockerContainer.ID))
	if c != nil {
		// Container already exists
		return
	}

	c, err := NewContainer(ctx, dockerContainer, action, project)
	if err != nil {
		// Context was cancelled or container creation failed
		slog.DebugContext(ctx, "failed to create container", slog.Any("error", err))
		return
	}

	// Add container to map
	d.addContainer(c)

	// Create a dedicated context for stats collection using parentCtx.
	// Using parentCtx instead of ctx (errCtx) prevents cascading cancellations
	// when other errgroup goroutines fail.
	// Use getParentCtx() for thread-safe access.
	statsCtx, statsCancel := context.WithCancel(d.getParentCtx())

	d.statsContextsLock.Lock()
	d.statsContexts[c.ID] = statsCancel
	d.statsContextsLock.Unlock()

	// Capture current generation before spawning goroutine
	currentGen := c.statsGen.Load()

	d.statsWG.Add(1)
	go func() {
		defer d.statsWG.Done()
		d.getContainerStatsRealtime(statsCtx, c, currentGen)
	}()
}

func (d *Docker) getContainerStatsRealtime(ctx context.Context, c *Container, expectedGen uint64) {
	// Use the expected generation passed by the caller to check for restarts
	// This avoids race conditions where statsGen could be incremented between
	// the goroutine spawn and this Load() call
	myGen := expectedGen

	// CRITICAL: Always clean up the stats context entry when this goroutine exits
	// This prevents memory leaks regardless of how we exit (error, EOF, context cancel)
	defer func() {
		d.statsContextsLock.Lock()
		// Only delete if we're still the current stats goroutine (same generation)
		// A new stats goroutine might have replaced us already
		if _, exists := d.statsContexts[c.ID]; exists {
			currentGen := c.statsGen.Load()
			if currentGen == myGen {
				delete(d.statsContexts, c.ID)
			}
		}
		d.statsContextsLock.Unlock()
	}()

	dockerContainerStats, err := d.client.ContainerStats(ctx, string(c.ID), true)
	if err != nil {
		d.logger.ErrorContext(ctx, "container stats failed", slog.String("container_id", string(c.ID)), slog.Any("error", err))
		d.markContainerExited(ctx, c)
		return
	}

	defer dockerContainerStats.Body.Close()
	defer d.markContainerExited(ctx, c)

	dec := json.NewDecoder(dockerContainerStats.Body)

	for {
		var stats apiContainer.StatsResponse

		// Decode synchronously - the Body will be closed by defer when context is cancelled,
		// which will unblock the Decode() call. This avoids goroutine leaks.
		errDecode := dec.Decode(&stats)
		if errDecode != nil {
			// Check if context was cancelled (Body was closed)
			if ctx.Err() != nil {
				d.logger.DebugContext(ctx, "container stats cancelled", slog.String("container_id", string(c.ID)))
				return
			}
			if !errors.Is(errDecode, io.EOF) && !errors.Is(errDecode, context.DeadlineExceeded) {
				d.logger.ErrorContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			} else {
				d.logger.DebugContext(ctx, "end of container stats", slog.String("container_id", string(c.ID)), slog.Any("error", errDecode))
			}
			return
		}

		// Check if this goroutine is still the current generation (container hasn't restarted)
		currentGen := c.statsGen.Load()
		if currentGen != myGen {
			// Container has restarted, this goroutine should exit
			d.logger.DebugContext(ctx, "container stats goroutine outdated", slog.String("container_id", string(c.ID)), slog.Uint64("old_gen", myGen), slog.Uint64("new_gen", currentGen))
			return
		}

		// Send stats update to container
		d.sendStatsUpdate(ctx, c, stats, myGen)
	}
}

// markContainerExited marks a container as exited.
func (d *Docker) markContainerExited(ctx context.Context, c *Container) {
	c.SetStatus(model.StatusExited)
	d.logger.DebugContext(ctx, "container stopped (stats ended)", slog.String("container_id", string(c.ID)))
}

// sendStatsUpdate sends a stats update to a container.
func (d *Docker) sendStatsUpdate(ctx context.Context, c *Container, stats apiContainer.StatsResponse, myGen uint64) {
	// Double-check generation before updating to prevent race condition
	if c.statsGen.Load() == myGen {
		c.Update(stats)
	}
}

// containerSummaryFromEvent creates a container summary from a Docker event.
func containerSummaryFromEvent(msg events.Message) apiContainer.Summary {
	return apiContainer.Summary{
		ID:     msg.Actor.ID,
		Names:  []string{msg.Actor.Attributes["name"]},
		Labels: msg.Actor.Attributes,
	}
}
