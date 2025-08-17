package tui

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github/syrm/c8s/dto"

	"github.com/rivo/tview"
)

type Tui struct {
	app                 *tview.Application
	projectMsg          chan dto.Project
	tableProject        *tview.Table
	tableProjectData    map[dto.ProjectID]dto.Project
	tableProjectMapping map[dto.ProjectID]int
	logger              *slog.Logger
}

func NewTui(logger *slog.Logger) *Tui {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	app := tview.NewApplication()

	t := tview.NewTable().SetSelectable(true, false)
	t.SetBorder(false)

	t.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetSelectable(false))

	return &Tui{
		app:                 app,
		logger:              logger,
		projectMsg:          make(chan dto.Project),
		tableProject:        t,
		tableProjectData:    make(map[dto.ProjectID]dto.Project),
		tableProjectMapping: make(map[dto.ProjectID]int),
	}
}

func (t *Tui) GetProjectMsg() chan<- dto.Project {
	return t.projectMsg
}

func (t *Tui) readProjectMsg(ctx context.Context) {
	for {
		select {
		case p := <-t.projectMsg:
			rowIndex, ok := t.tableProjectMapping[p.ID]

			if !ok {
				rowIndex = t.tableProject.GetRowCount()
				t.tableProjectMapping[p.ID] = rowIndex
			}
			// t.logger.DebugContext(ctx, "tui project msg received", slog.Any("project_id", p.ID), slog.Int("index", rowIndex))

			t.app.QueueUpdateDraw(func() {
				t.tableProject.SetCell(rowIndex, 0, tview.NewTableCell(p.Name))
				t.tableProject.SetCell(rowIndex, 1, tview.NewTableCell(fmt.Sprintf("%.2f%%", p.CPUPercentage)).SetAlign(tview.AlignRight))
				t.tableProject.SetCell(rowIndex, 2, tview.NewTableCell(fmt.Sprintf("%.2f%%", p.MemoryUsagePercentage)).SetAlign(tview.AlignRight))
				t.tableProject.SetCell(rowIndex, 3, tview.NewTableCell(fmt.Sprintf("%d/%d", p.ContainersRunning, p.ContainersTotal)).SetAlign(tview.AlignRight))
			})
		case <-ctx.Done():
			t.logger.DebugContext(ctx, "context is done")
		}
	}
}

func (t *Tui) Render(ctx context.Context) {
	go t.readProjectMsg(ctx)

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}

	t.logger.ErrorContext(ctx, "tui exited")
}
