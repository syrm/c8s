package docker

import (
	"context"
	"io"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
)

// DockerAPI defines the interface for Docker client operations used by this package.
// This interface allows for mocking in tests.
type DockerAPI interface {
	// ContainerList returns a list of containers.
	ContainerList(ctx context.Context, options apiContainer.ListOptions) ([]apiContainer.Summary, error)

	// ContainerStats returns a stream of container stats.
	ContainerStats(ctx context.Context, containerID string, stream bool) (apiContainer.StatsResponseReader, error)

	// ContainerLogs returns a stream of container logs.
	ContainerLogs(ctx context.Context, containerID string, options apiContainer.LogsOptions) (io.ReadCloser, error)

	// Events returns a stream of Docker events.
	Events(ctx context.Context, options events.ListOptions) (<-chan events.Message, <-chan error)

	// Close closes the Docker client connection.
	Close() error
}

// Ensure Docker client implements DockerAPI.
// This is a compile-time check that the Docker client satisfies the interface.
// Note: We don't use dockerClient.Client directly as it has many more methods,
// but our interface only needs these specific methods.
