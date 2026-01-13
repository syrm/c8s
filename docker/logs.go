package docker

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	apiContainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
	"golang.org/x/sync/errgroup"

	"github.com/syrm/c8s/dto"
	"github.com/syrm/c8s/internal/timer"
)

func (d *Docker) collectContainerLogs(ctx context.Context, c *Container) {
	if c == nil {
		return
	}

	// CRITICAL: Always clean up the log context entry when this goroutine exits
	defer d.cleanupLogContext(c.ID)
	defer d.resetLogCollectionFlag(c)

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
// This prevents memory leaks when log collection ends.
func (d *Docker) cleanupLogContext(containerID dto.ContainerID) {
	d.logContextsLock.Lock()
	delete(d.logContexts, containerID)
	d.logContextsLock.Unlock()
}

// openLogStream opens a log stream for a container.
func (d *Docker) openLogStream(ctx context.Context, c *Container) (io.ReadCloser, error) {
	since := time.Now().Add(-logHistoryDuration).Format(time.RFC3339)
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
	// Use a pipe to demultiplex the Docker log stream
	pr, pw := io.Pipe()

	// closeResources closes both the Docker log stream and the pipe reader.
	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			logStream.Close()
			pr.Close()
		})
	}

	lines := make(chan string, logLineBufferSize)
	g, egCtx := errgroup.WithContext(ctx)

	// Goroutine 1: Demultiplex Docker log stream
	g.Go(func() error {
		return d.demuxLogStream(egCtx, c, pw, logStream, closeResources)
	})

	// Goroutine 2: Read lines from pipe
	g.Go(func() error {
		return d.readLogLines(egCtx, c, pr, lines, closeResources)
	})

	// Goroutine 3: Send lines to container
	g.Go(func() error {
		return d.sendLogLinesToContainer(egCtx, c, lines, closeResources)
	})

	if err := g.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		d.logger.ErrorContext(ctx, "collectContainerLogs errgroup finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
	}
}

// demuxLogStream demultiplexes the Docker log stream using stdcopy.
func (d *Docker) demuxLogStream(ctx context.Context, c *Container, pw *io.PipeWriter, logStream io.ReadCloser, closeResources func()) error {
	defer pw.Close()
	defer closeResources()
	_, err := stdcopy.StdCopy(pw, pw, logStream)
	if err != nil && !errors.Is(err, io.EOF) {
		d.logger.ErrorContext(ctx, "stdcopy finished with error",
			slog.String("container_id", string(c.ID)),
			slog.Any("error", err))
		return err
	}
	return nil
}

// readLogLines reads lines from the pipe and sends them to the lines channel.
func (d *Docker) readLogLines(ctx context.Context, c *Container, pr *io.PipeReader, lines chan<- string, closeResources func()) error {
	defer close(lines)
	defer closeResources()
	reader := bufio.NewReader(pr)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if !errors.Is(err, io.EOF) {
				d.logger.ErrorContext(ctx, "end of container logs with error",
					slog.String("container_id", string(c.ID)),
					slog.Any("error", err))
				return err
			}
			return nil
		}
		select {
		case lines <- line:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// sendLogLinesToContainer receives log lines and sends them to the container.
func (d *Docker) sendLogLinesToContainer(ctx context.Context, c *Container, lines <-chan string, closeResources func()) error {
	sendTimer := timer.New(time.Second)
	defer timer.Stop(sendTimer)

	for {
		select {
		case <-ctx.Done():
			d.logger.DebugContext(ctx, "collectContainerLogs context is done",
				slog.String("container_id", string(c.ID)))
			closeResources()
			return ctx.Err()
		case line, ok := <-lines:
			if !ok {
				return nil
			}
			timer.Stop(sendTimer)
			sendTimer.Reset(time.Second)

			select {
			case c.Command <- ContainerCommand{
				functor: func(container *Container) {
					container.AppendLog(line)
				},
			}:
			case <-sendTimer.C:
				// Timeout sending log, container might be busy or deleted
			case <-ctx.Done():
				closeResources()
				return ctx.Err()
			}
		}
	}
}
