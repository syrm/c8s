package main

import (
	"context"
	"log/slog"
	"os"

	"github/syrm/c8s/docker"
	"github/syrm/c8s/tui"
)

func main() {
	ctx := context.Background()

	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}))

	// ctx2, cancel := context.WithTimeout(ctx, 10 * time.Second)
	// defer cancel()

	t := tui.NewTui(logger)

	doc := docker.NewDocker(ctx, t.GetProjectUpdated(), *logger)
	go doc.Run(ctx)

	t.Render(ctx)

	logger.InfoContext(ctx, "c8s is over")
}
