package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"time"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/sync/errgroup"

	"github.com/syrm/c8s/internal/model"
)

func (d *Docker) collectContainerLogs(ctx context.Context, c *Container, logChan chan<- []string) {
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
	defer close(logChan)

	// Open the log stream from Docker
	logStream, err := d.openLogStream(ctx, c)
	if err != nil {
		return
	}
	defer logStream.Close()

	// Process the log stream
	d.processLogStream(ctx, c, logStream, logChan)
}

// cleanupLogContext removes the log context entry for a container.
func (d *Docker) cleanupLogContext(containerID model.ContainerID) {
	d.logContextsLock.Lock()
	delete(d.logContexts, containerID)
	d.logContextsLock.Unlock()
}

// openLogStream opens a log stream for a container.
func (d *Docker) openLogStream(ctx context.Context, c *Container) (io.ReadCloser, error) {
	opts := apiContainer.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: true,
		Follow:     true,
		Tail:       d.cfg.LogTail,
	}

	// If LogHistory > 0, also add Since to limit by time
	if d.cfg.LogHistory > 0 {
		opts.Since = time.Now().Add(-d.cfg.LogHistory).Format(time.RFC3339)
	}

	out, err := d.client.ContainerLogs(ctx, string(c.ID), opts)
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

// processLogStream processes a Docker log stream and sends line batches to the TUI via logChan.
func (d *Docker) processLogStream(ctx context.Context, c *Container, logStream io.ReadCloser, logChan chan<- []string) {
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

	// Goroutine 2: Read lines from Pipe and send batches to TUI
	g.Go(func() error {
		defer pr.Close()
		reader := bufio.NewReaderSize(pr, 64*1024)
		var batch []string
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				line = strings.TrimRight(line, "\n")
				batch = append(batch, line)
			}

			if err != nil {
				// Send remaining batch before exit
				if len(batch) > 0 {
					select {
					case logChan <- batch:
					case <-egCtx.Done():
						return egCtx.Err()
					}
				}
				if !errors.Is(err, io.EOF) {
					d.logger.ErrorContext(egCtx, "end of container logs with error",
						slog.String("container_id", string(c.ID)),
						slog.Any("error", err))
					return err
				}
				return nil
			}

			// Send batch when it reaches a reasonable size or when no more data is immediately available
			if len(batch) >= 100 || reader.Buffered() == 0 {
				select {
				case logChan <- batch:
					batch = nil
				case <-egCtx.Done():
					return egCtx.Err()
				}
			}
		}
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.ErrorContext(ctx, "collectContainerLogs errgroup finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
	}
}
