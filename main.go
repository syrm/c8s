package main

import (
	"context"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"

	"github.com/rivo/tview"

	"github/syrm/c8s/docker"
	"github/syrm/c8s/tui"
)

func main() {
	runtime.SetMutexProfileFraction(1)

	ctx := context.Background()

	file, err := os.OpenFile("app.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{AddSource: true, Level: slog.LevelDebug}))

	go func() {
		logger.Info("server", slog.Any("error", http.ListenAndServe("0.0.0.0:6060", nil)))
	}()

	// ctx2, cancel := context.WithTimeout(ctx, 10 * time.Second)
	// defer cancel()

	tuiC8s := tui.NewTui(logger)
	projectTableContent := tui.NewProjectTableContent(ctx, tuiC8s.GetRefreshViewCh(), logger)

	doc := docker.NewDocker(ctx, logger, projectTableContent.GetUpdateCh())
	go doc.Run(ctx)

	projectTable := tview.NewTable().SetSelectable(true, false)
	projectTable.SetBorder(false)
	projectTable.SetContent(projectTableContent)
	tuiC8s.SetProjectTable(projectTable)
	tuiC8s.Render(ctx)

	logger.InfoContext(ctx, "c8s is over")
}
