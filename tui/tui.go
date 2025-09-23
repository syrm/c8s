package tui

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/syrm/c8s/dto"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type currentView int

const (
	viewProjectList currentView = iota
	viewProject
	viewContainerLog
)

type RequestData interface {
	isRequestData()
}

type RequestProjectList struct {
	Response chan []dto.Project
}

func (p *RequestProjectList) isRequestData() {}

type RequestContainerLog struct {
	ContainerID dto.ContainerID
	Response    chan dto.Container
}

func (p *RequestContainerLog) isRequestData() {}

type RequestProject struct {
	ProjectID dto.ProjectID
	Response  chan []dto.Container
}

func (p *RequestProject) isRequestData() {}

type Tui struct {
	app                    *tview.Application
	tableProject           *tview.Table
	tableProjectData       map[dto.ProjectID]dto.Project
	tableProjectDataLock   sync.RWMutex
	tableContainer         *tview.Table
	tableContainerData     map[dto.ContainerID]dto.Container
	tableContainerDataLock sync.RWMutex
	tableContainerLog      *tview.TextView
	tableContainerLogData  []string
	currentView            currentView
	currentViewLock        sync.RWMutex
	currentIDTargeted      string
	requestData            chan RequestData
	logger                 *slog.Logger
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
	tableProject.SetBorder(true)

	tableContainer := tview.NewTable().SetSelectable(true, false)
	tableContainer.SetBorder(true)

	tableContainerLog := tview.NewTextView()

	tui := &Tui{
		app:                app,
		logger:             logger,
		tableProject:       tableProject,
		tableProjectData:   make(map[dto.ProjectID]dto.Project),
		tableContainer:     tableContainer,
		tableContainerData: make(map[dto.ContainerID]dto.Container),
		tableContainerLog:  tableContainerLog,
		requestData:        make(chan RequestData),
		currentView:        viewProjectList,
	}

	tableProject.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableProject.GetSelection()
			tui.tableProjectDataLock.RLock()
			for _, project := range tui.tableProjectData {
				if project.Name == tableProject.GetCell(rowIndex, 0).Text {
					tui.currentIDTargeted = string(project.ID)
					tui.currentViewLock.Lock()
					tui.currentView = viewProject
					tui.currentViewLock.Unlock()
					break
				}
			}
			tui.tableProjectDataLock.RUnlock()
			tui.tableContainer.Clear()
			tui.drawContainers()
			tui.app.SetRoot(tui.tableContainer, true)
		}

		return event
	})

	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.app.SetRoot(tui.tableProject, true)
			tui.currentViewLock.Lock()
			tui.currentView = viewProjectList
			tui.currentViewLock.Unlock()
			tui.currentIDTargeted = ""
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableContainer.GetSelection()
			tui.tableContainerDataLock.RLock()
			for _, container := range tui.tableContainerData {
				if container.Service == tableContainer.GetCell(rowIndex, 0).Text {
					tui.currentIDTargeted = string(container.ID)
					break
				}
			}
			tui.tableContainerDataLock.RUnlock()
			tui.app.SetRoot(tui.tableContainerLog, true)
			tui.currentViewLock.Lock()
			tui.currentView = viewContainerLog
			tui.currentViewLock.Unlock()
		}

		return event
	})

	return tui
}

func (t *Tui) RenderProjectHeader() {
	t.tableProject.SetCell(0, 0, tview.NewTableCell("[::b]Project").SetAlign(tview.AlignCenter).SetExpansion(3).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell("[::b]Cont.").SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader(project string) {
	t.tableContainer.SetCell(0, 0, tview.NewTableCell("[::b]"+project+" container").SetAlign(tview.AlignCenter).SetExpansion(2).SetSelectable(false))
	t.tableContainer.SetCell(0, 1, tview.NewTableCell("[::b]CPU").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetCell(0, 2, tview.NewTableCell("[::b]Memory").SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetFixed(1, 0)
}

func (t *Tui) drawProjects() {
	projects := slices.Collect(maps.Values(t.tableProjectData))

	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		if a.CPUPercentage < b.CPUPercentage {
			return 1
		}

		if a.CPUPercentage > b.CPUPercentage {
			return -1
		}

		return strings.Compare(a.Name, b.Name)
	})

	t.tableProject.Clear()
	t.RenderProjectHeader()
	offset := 0
	for index, project := range projects {
		t.tableProject.SetCell(index+1+offset, 0, tview.NewTableCell(project.Name))
		t.tableProject.SetCell(
			index+1+offset,
			1,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.CPUPercentage)),
			).
				SetAlign(tview.AlignRight),
		)
		t.tableProject.SetCell(
			index+1+offset,
			2,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.MemoryPercentage)),
			).
				SetAlign(tview.AlignRight),
		)
		t.tableProject.SetCell(
			index+1+offset,
			3,
			tview.NewTableCell(
				fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState)),
			).
				SetAlign(tview.AlignRight),
		)
	}
}

func (t *Tui) drawContainers() {
	t.tableContainerDataLock.RLock()
	containers := slices.SortedStableFunc(maps.Values(t.tableContainerData), func(a, b dto.Container) int {
		if a.CPUPercentage < b.CPUPercentage {
			return 1
		}

		if a.CPUPercentage > b.CPUPercentage {
			return -1
		}

		return strings.Compare(a.Name, b.Name)
	})
	t.tableContainerDataLock.RUnlock()

	t.tableContainer.Clear()
	t.tableProjectDataLock.RLock()
	t.RenderContainerHeader(t.tableProjectData[dto.ProjectID(t.currentIDTargeted)].Name)
	t.tableProjectDataLock.RUnlock()
	index := 0
	for _, container := range containers {
		if string(container.Project.ID) != t.currentIDTargeted {
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
				fmt.Sprintf("%.2f%%", container.MemoryPercentage),
			).
				SetAlign(tview.AlignRight),
		)
	}
}

func (t *Tui) drawContainerLog() {
	t.tableContainerLog.Clear()
	t.tableContainerLog.SetText(strings.Join(t.tableContainerLogData, "\n"))
}

func (t *Tui) GetRequestData() <-chan RequestData {
	return t.requestData
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	var ctxCancel context.CancelFunc

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			t.currentViewLock.RLock()
			cv := t.currentView
			t.currentViewLock.RUnlock()
			switch cv {
			case viewProjectList:
				if ctxCancel != nil {
					ctxCancel()
					ctxCancel = nil
				}

				response := make(chan []dto.Project)
				t.requestData <- &RequestProjectList{
					Response: response,
				}

				projects := <-response
				t.tableProjectDataLock.Lock()
				t.tableProjectData = make(map[dto.ProjectID]dto.Project)
				for _, p := range projects {
					t.tableProjectData[p.ID] = p
				}
				t.tableProjectDataLock.Unlock()

				t.app.QueueUpdateDraw(func() {
					t.drawProjects()
				})
			case viewProject:
				if ctxCancel != nil {
					ctxCancel()
					ctxCancel = nil
				}

				response := make(chan []dto.Container)
				t.requestData <- &RequestProject{
					ProjectID: dto.ProjectID(t.currentIDTargeted),
					Response:  response,
				}

				containers := <-response
				t.tableContainerDataLock.Lock()
				t.tableContainerData = make(map[dto.ContainerID]dto.Container)
				for _, c := range containers {
					t.tableContainerData[c.ID] = c
				}
				t.tableContainerDataLock.Unlock()

				t.app.QueueUpdateDraw(func() {
					t.drawContainers()
				})

			case viewContainerLog:
				t.logger.DebugContext(ctx, "fetching logs for container", slog.String("container_id", t.currentIDTargeted))
				response := make(chan dto.Container)
				t.requestData <- &RequestContainerLog{
					ContainerID: dto.ContainerID(t.currentIDTargeted),
					Response:    response,
				}

				c := <-response
				ctxCancel = c.LogCancel

				t.app.QueueUpdateDraw(func() {
					t.drawContainers()
				})
			}
		}
	}
}

func (t *Tui) Render(ctx context.Context) {
	go t.getData(ctx)

	if err := t.app.SetRoot(t.tableProject, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}
}
