package tui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github/syrm/c8s/dto"

	"github.com/rivo/tview"
)

type Tui struct {
	app              *tview.Application
	projectUpdated   chan dto.Project
	tableProject     *tview.Table
	tableProjectData map[dto.ProjectID]dto.Project
	logger           *slog.Logger
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

	tui := &Tui{
		app:              app,
		logger:           logger,
		projectUpdated:   make(chan dto.Project, 256),
		tableProject:     t,
		tableProjectData: make(map[dto.ProjectID]dto.Project),
	}

	tui.RenderHeader()

	return tui
}

func (t *Tui) RenderHeader() {
	t.tableProject.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) GetProjectUpdated() chan<- dto.Project {
	return t.projectUpdated
}

func (t *Tui) readProjectUpdated(ctx context.Context) {
	for {
		select {
		case p := <-t.projectUpdated:
			t.tableProjectData[p.ID] = p

			projects := slices.SortedStableFunc(maps.Values(t.tableProjectData), func(a, b dto.Project) int {
				if a.CPUPercentage < b.CPUPercentage {
					return 1
				}

				if a.CPUPercentage > b.CPUPercentage {
					return -1
				}

				return strings.Compare(a.Name, b.Name)
			})

			t.app.QueueUpdateDraw(func() {
				t.tableProject.Clear()
				t.RenderHeader()
				for index, project := range projects {
					t.tableProject.SetCell(index+1, 0, tview.NewTableCell(project.Name))
					t.tableProject.SetCell(
						index+1,
						1,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", project.CPUPercentage),
						).
							SetAlign(tview.AlignRight),
					)
					t.tableProject.SetCell(
						index+1,
						2,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", project.MemoryUsagePercentage),
						).
							SetAlign(tview.AlignRight),
					)
					t.tableProject.SetCell(
						index+1,
						3,
						tview.NewTableCell(
							fmt.Sprintf("%d/%d", project.ContainersRunning, project.ContainersTotal),
						).
							SetAlign(tview.AlignRight),
					)

				}
			})

		case <-ctx.Done():
			t.logger.DebugContext(ctx, "context is done")
			return
		}
	}
}

func (t *Tui) Render(ctx context.Context) {
	go t.readProjectUpdated(ctx)

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}
}
