package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/syrm/c8s/docker"
	"github.com/syrm/c8s/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		// Log file is not critical, continue with stderr logging
		fmt.Fprintf(os.Stderr, "warning: could not create log file: %v\n", err)
		file = nil
	}
	if file != nil {
		defer file.Close()
	}

	var logger *slog.Logger
	if file != nil {
		logger = slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	t := tui.NewTui(logger)

	doc, err := docker.NewDocker(ctx, t.GetRequestData(), logger)
	if err != nil {
		return fmt.Errorf("failed to initialize docker client: %w", err)
	}
	go doc.Run(ctx)

	if err := t.Render(ctx); err != nil {
		return fmt.Errorf("failed to render TUI: %w", err)
	}

	logger.InfoContext(ctx, "c8s is over")
	return nil
}
