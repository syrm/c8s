package docker

import (
	"context"
	"log/slog"

	"github.com/syrm/c8s/dto"
	ch "github.com/syrm/c8s/internal/channel"
)

// ContainersCommand represents a command to be executed on the containers map.
// It uses a functor pattern to serialize access to the containers.
type ContainersCommand struct {
	functor  func(*Docker) *Container
	response chan *Container
}

// handleContainersCommand processes commands that access the containers map.
// It runs in a dedicated goroutine to serialize all container map access.
func (d *Docker) handleContainersCommand(ctx context.Context) error {
	for {
		select {
		case cmd := <-d.containersCommand:
			d.executeContainersCommand(ctx, cmd)
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "handleContainersCommand context is done")
			return nil
		}
	}
}

// executeContainersCommand executes a single command with panic recovery.
// This ensures that even if a functor panics, the response channel is written to,
// preventing deadlocks in callers waiting for a response.
func (d *Docker) executeContainersCommand(ctx context.Context, cmd ContainersCommand) {
	var c *Container

	defer func() {
		if r := recover(); r != nil {
			d.logger.ErrorContext(ctx, "panic in containersCommand functor", slog.Any("recover", r))
			c = nil
		}
		if cmd.response != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						d.logger.ErrorContext(ctx, "panic sending response (channel likely closed)", slog.Any("recover", r))
					}
				}()
				cmd.response <- c
			}()
		}
	}()

	if cmd.functor != nil {
		c = cmd.functor(d)
	}
}

// getContainer retrieves a container by ID with proper timeout handling.
func (d *Docker) getContainer(ctx context.Context, containerID dto.ContainerID) *Container {
	response := make(chan *Container, 1)

	cmd := ContainersCommand{
		functor: func(docker *Docker) *Container {
			return docker.containers[containerID]
		},
		response: response,
	}

	if ch.Send(ctx, d.containersCommand, cmd, dto.ChannelTimeout) != ch.SendOK {
		d.logger.Warn("timeout sending to containersCommand in getContainer")
		return nil
	}

	c, result := ch.Receive(ctx, response, dto.ChannelTimeout)
	if result != ch.ReceiveOK {
		d.logger.Warn("timeout waiting for response in getContainer")
		return nil
	}

	return c
}

// getContainersList retrieves all containers matching a filter function.
func (d *Docker) getContainersList(ctx context.Context, filter func(*Container) bool) []*Container {
	response := make(chan []*Container, 1)

	cmd := ContainersCommand{
		functor: func(docker *Docker) *Container {
			var containers []*Container
			for _, c := range docker.containers {
				if filter == nil || filter(c) {
					containers = append(containers, c)
				}
			}
			response <- containers
			return nil
		},
	}

	if ch.Send(ctx, d.containersCommand, cmd, dto.ChannelTimeout) != ch.SendOK {
		d.logger.Warn("timeout sending to containersCommand in getContainersList")
		return nil
	}

	containers, result := ch.Receive(ctx, response, dto.ChannelTimeout)
	if result != ch.ReceiveOK {
		d.logger.Warn("timeout waiting for response in getContainersList")
		return nil
	}

	return containers
}

// addContainer adds a container to the map.
func (d *Docker) addContainer(ctx context.Context, c *Container) bool {
	cmd := ContainersCommand{
		functor: func(docker *Docker) *Container {
			docker.containers[c.ID] = c
			return nil
		},
	}

	return ch.Send(ctx, d.containersCommand, cmd, dto.ChannelTimeout) == ch.SendOK
}

// removeContainer removes a container from the map.
func (d *Docker) removeContainer(ctx context.Context, containerID dto.ContainerID) bool {
	cmd := ContainersCommand{
		functor: func(docker *Docker) *Container {
			delete(docker.containers, containerID)
			return nil
		},
	}

	return ch.Send(ctx, d.containersCommand, cmd, dto.ChannelTimeout) == ch.SendOK
}
