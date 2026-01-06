package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/syrm/c8s/docker"
	"github.com/syrm/c8s/tui"
)

func main() {
	ctx := context.Background()

	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{AddSource: true, Level: slog.LevelInfo}))

	t := tui.NewTui(logger)

	doc := docker.NewDocker(ctx, t.GetRequestData(), logger)
	go doc.Run(ctx)

	t.Render(ctx)

	logger.InfoContext(ctx, "c8s is over")
}
