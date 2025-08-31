package tui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github/syrm/c8s/dto"

	"github.com/rivo/tview"
)

type Tui struct {
	app                *tview.Application
	projectUpdated     chan dto.Project
	tableProject       *tview.Table
	tableProjectData   map[dto.ProjectID]dto.Project
	containerUpdated   chan dto.Container
	tableContainer     *tview.Table
	tableContainerData map[dto.ContainerID]dto.Container
	currentProjectID   dto.ProjectID
	logger             *slog.Logger
}

func NewTui(logger *slog.Logger) *Tui {
	tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
	tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
	tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
	tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
	tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
	tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

	app := tview.NewApplication()

	tableProject := tview.NewTable().SetSelectable(true, false)
	tableProject.SetBorder(false)

	tableContainer := tview.NewTable().SetSelectable(true, false)
	tableContainer.SetBorder(false)

	tui := &Tui{
		app:                app,
		logger:             logger,
		projectUpdated:     make(chan dto.Project, 256),
		tableProject:       tableProject,
		tableProjectData:   make(map[dto.ProjectID]dto.Project),
		containerUpdated:   make(chan dto.Container, 256),
		tableContainer:     tableContainer,
		tableContainerData: make(map[dto.ContainerID]dto.Container),
	}

	tableProject.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableProject.GetSelection()
			for _, project := range tui.tableProjectData {
				if project.Name == tableProject.GetCell(rowIndex, 0).Text {
					tui.currentProjectID = project.ID
					break
				}
			}

			tui.app.SetRoot(tui.tableContainer, true)
		}

		return event
	})

	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.app.SetRoot(tui.tableProject, true)
		}

		return event
	})

	return tui
}

func (t *Tui) RenderProjectHeader() {
	t.tableProject.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader(project string) {
	t.tableContainer.SetCell(0, 0, tview.NewTableCell("[::b]"+project+" container").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.tableContainer.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetFixed(1, 0)
}

func (t *Tui) GetProjectUpdated() chan<- dto.Project {
	return t.projectUpdated
}

func (t *Tui) GetContainerUpdated() chan<- dto.Container {
	return t.containerUpdated
}

func (t *Tui) readProjectUpdated(ctx context.Context) {
	for {
		select {
		case p := <-t.projectUpdated:
			t.tableProjectData[p.ID] = p

			//rowSelected, _ := t.tableProject.GetSelection()
			//rowSelected -= 1
			//var projects []dto.Project
			//var projectSelected dto.Project
			//for _, p := range t.tableProjectData {
			//	if t.tableProject.GetCell(rowSelected, 0).Text == p.Name {
			//		projectSelected = p
			//		continue
			//	}
			//	projects = append(projects, p)
			//}
			//
			projects := slices.SortedStableFunc(maps.Values(t.tableProjectData), func(a, b dto.Project) int {
				if a.CPUPercentage < b.CPUPercentage {
					return 1
				}

				if a.CPUPercentage > b.CPUPercentage {
					return -1
				}

				return strings.Compare(a.Name, b.Name)
			})
			//
			//if rowSelected == 0 {
			//	projects = append([]dto.Project{projectSelected}, projects...)
			//} else {
			//	projectsTmp := projects[:rowSelected-1]
			//	projectsTmp = append(projectsTmp, projectSelected)
			//	projects = append(projectsTmp, projects[rowSelected:]...)
			//}

			//rowSelected := 3
			//projects := slices.Collect(maps.Values(t.tableProjectData))
			//
			//if len(projects) > 3 {
			//	projectSelected := projects[rowSelected]
			//	copy(projects[rowSelected:], projects[rowSelected+1:])
			//	slices.SortStableFunc(projects[:len(projects)-1], func(a, b dto.Project) int {
			//		if a.CPUPercentage < b.CPUPercentage {
			//			return 1
			//		}
			//
			//		if a.CPUPercentage > b.CPUPercentage {
			//			return -1
			//		}
			//
			//		return strings.Compare(a.Name, b.Name)
			//	})
			//	copy(projects[rowSelected+1:], projects[rowSelected:len(projects)-1])
			//	projects[rowSelected] = projectSelected
			//}

			t.app.QueueUpdateDraw(func() {
				t.tableProject.Clear()
				t.RenderProjectHeader()
				offset := 0
				for index, project := range projects {
					//rowSelected, _ := t.tableProject.GetSelection()
					//if index+1 == rowSelected {
					//	offset = 1
					//}
					//
					//if t.tableProject.GetCell(rowSelected, 0).Text == project.Name {
					//	continue
					//}

					t.tableProject.SetCell(index+1+offset, 0, tview.NewTableCell(project.Name))
					t.tableProject.SetCell(
						index+1+offset,
						1,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", project.CPUPercentage),
						).
							SetAlign(tview.AlignRight),
					)
					t.tableProject.SetCell(
						index+1+offset,
						2,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", project.MemoryUsagePercentage),
						).
							SetAlign(tview.AlignRight),
					)
					t.tableProject.SetCell(
						index+1+offset,
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

func (t *Tui) readContainerUpdated(ctx context.Context) {
	for {
		select {
		case c := <-t.containerUpdated:
			t.tableContainerData[c.ID] = c

			containers := slices.SortedStableFunc(maps.Values(t.tableContainerData), func(a, b dto.Container) int {
				if a.CPUPercentage < b.CPUPercentage {
					return 1
				}

				if a.CPUPercentage > b.CPUPercentage {
					return -1
				}

				return strings.Compare(a.Name, b.Name)
			})

			t.app.QueueUpdateDraw(func() {
				t.tableContainer.Clear()
				t.RenderContainerHeader(t.tableProjectData[t.currentProjectID].Name)
				index := 0
				for _, container := range containers {
					if container.ProjectID != t.currentProjectID {
						continue
					}
					index += 1

					t.tableContainer.SetCell(index, 0, tview.NewTableCell(container.Service))
					t.tableContainer.SetCell(
						index,
						1,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", container.CPUPercentage),
						).
							SetAlign(tview.AlignRight),
					)
					t.tableContainer.SetCell(
						index,
						2,
						tview.NewTableCell(
							fmt.Sprintf("%.2f%%", container.MemoryUsagePercentage),
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
	go t.readContainerUpdated(ctx)

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}

	t.logger.ErrorContext(ctx, "tui exited")
}
