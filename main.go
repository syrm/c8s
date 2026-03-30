package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/syrm/c8s/docker"
	"github.com/syrm/c8s/internal/config"
	"github.com/syrm/c8s/tui"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run initializes and starts the c8s application.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	file, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create log file: %v\n", err)
		file = nil
	}
	if file != nil {
		defer func() {
			if err := file.Close(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to close log file: %v\n", err)
			}
		}()
	}

	logLevel := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	}

	var logger *slog.Logger
	if file != nil {
		logger = slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{AddSource: true, Level: logLevel}))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	m := tui.New(logger)

	doc, err := docker.NewDocker(ctx, m.GetRequestData(), logger, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize docker client: %w", err)
	}
	defer func() {
		if err := doc.Close(); err != nil {
			logger.ErrorContext(context.Background(), "failed to close docker client", slog.Any("error", err))
		}
	}()

	go doc.Run(ctx)

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("failed to run TUI: %w", err)
	}

	// Cancel context to signal Docker goroutines to shutdown
	cancel()

	// Wait for Docker goroutines to finish cleanly
	doc.Wait()

	logger.InfoContext(context.Background(), "c8s is over")
	return nil
}
