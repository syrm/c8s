package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/sync/errgroup"

	"github.com/syrm/c8s/internal/model"
)

func (d *Docker) collectContainerLogs(ctx context.Context, c *Container) {
	if c == nil {
		return
	}

	d.logger.DebugContext(ctx, "collectContainerLogs started", slog.String("container_id", string(c.ID)))

	// Set flag
	c.mu.Lock()
	c.LogCollectionActive = true
	c.mu.Unlock()

	defer d.cleanupLogContext(c.ID)
	defer func() {
		c.mu.Lock()
		c.LogCollectionActive = false
		c.mu.Unlock()
	}()

	// Open the log stream from Docker
	logStream, err := d.openLogStream(ctx, c)
	if err != nil {
		return
	}
	defer logStream.Close()

	// Process the log stream
	d.processLogStream(ctx, c, logStream)
}

// cleanupLogContext removes the log context entry for a container.
func (d *Docker) cleanupLogContext(containerID model.ContainerID) {
	d.logContextsLock.Lock()
	delete(d.logContexts, containerID)
	d.logContextsLock.Unlock()
}

// openLogStream opens a log stream for a container.
func (d *Docker) openLogStream(ctx context.Context, c *Container) (io.ReadCloser, error) {
	since := time.Now().Add(-d.cfg.LogHistory).Format(time.RFC3339)
	out, err := d.client.ContainerLogs(ctx, string(c.ID), apiContainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Since:      since,
	})
	if err != nil {
		d.logger.ErrorContext(ctx, "container logs failed",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
		if out != nil {
			out.Close()
		}
		return nil, err
	}
	return out, nil
}

// processLogStream processes a Docker log stream and sends lines to the container.
func (d *Docker) processLogStream(ctx context.Context, c *Container, logStream io.ReadCloser) {
	pr, pw := io.Pipe()

	g, egCtx := errgroup.WithContext(ctx)

	// Goroutine 1: Demultiplex Docker log stream to Pipe
	g.Go(func() error {
		defer pw.Close()
		_, err := stdcopy.StdCopy(pw, pw, logStream)
		if err != nil && !errors.Is(err, io.EOF) {
			d.logger.ErrorContext(egCtx, "stdcopy finished with error",
				slog.String("container_id", string(c.ID)),
				slog.Any("error", err))
			return err
		}
		return nil
	})

	// Goroutine 2: Read lines from Pipe and append to container
	g.Go(func() error {
		defer pr.Close()
		reader := bufio.NewReader(pr)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if !errors.Is(err, io.EOF) {
					d.logger.ErrorContext(egCtx, "end of container logs with error",
						slog.String("container_id", string(c.ID)),
						slog.Any("error", err))
					return err
				}
				return nil
			}

			// Check context
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			default:
				c.AppendLog(line)
			}
		}
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.ErrorContext(ctx, "collectContainerLogs errgroup finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
	}
}
