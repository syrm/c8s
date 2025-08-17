package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github/syrm/c8s/docker"
	"github/syrm/c8s/tui"

	"github.com/rivo/tview"
)

type view int

const (
	viewComposer view = iota
	viewContainer
)

var currentView = viewComposer

type project struct {
	name     string
	cpuStats int
}

type container struct {
	id            string
	name          string
	cpuPercentage int
}

var (
	containers map[string]container
	Logger     *log.Logger
)

func init() {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	containers = make(map[string]container)
}

func main() {
	ctx := context.Background()

	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ctx2, cancel := context.WithTimeout(ctx, 10 * time.Second)
	// defer cancel()

	t := tui.NewTui(logger)

	doc := docker.NewDocker(ctx, *logger, t.GetProjectMsg())
	go doc.Run(ctx)

	t.Render(ctx)

	logger.InfoContext(ctx, "c8s is over")
}
