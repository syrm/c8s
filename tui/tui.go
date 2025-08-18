package tui

import (
	"context"
	"log/slog"
	"os"

	"github.com/rivo/tview"
)

type Tui struct {
	app           *tview.Application
	tableProject  *tview.Table
	refreshViewCh chan struct{}
	logger        *slog.Logger
}

func NewTui(logger *slog.Logger) *Tui {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	app := tview.NewApplication()

	refreshViewCh := make(chan struct{})

	return &Tui{
		app:           app,
		logger:        logger,
		refreshViewCh: refreshViewCh,
	}
}

func (t *Tui) SetProjectTable(projectTable *tview.Table) {
	t.tableProject = projectTable
}

func (t *Tui) GetRefreshViewCh() chan<- struct{} {
	return t.refreshViewCh
}

func (t *Tui) HandleRefreshViewRequest() {
	go func() {
		for range t.refreshViewCh {
			t.app.QueueUpdateDraw(func() {
				//t.logger.DebugContext(ctx, "refreshing project table")
				t.tableProject.ScrollToBeginning()
			})
		}
	}()
}

func (t *Tui) Render(ctx context.Context) {
	go t.HandleRefreshViewRequest()

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}

	t.logger.ErrorContext(ctx, "tui exited")
}
