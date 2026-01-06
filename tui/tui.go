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
	"sync/atomic"
	"time"

	"github.com/syrm/c8s/dto"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
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

type RequestSetPendingAction struct {
	ContainerID   dto.ContainerID
	PendingAction string
	Response      chan bool
}

func (p *RequestSetPendingAction) isRequestData() {}

type Tui struct {
	app                        *tview.Application
	pages                      *tview.Pages
	tableProject               *tview.Table
	tableProjectData           map[dto.ProjectID]dto.Project
	tableProjectDataLock       sync.RWMutex
	projectSearchInput         *tview.InputField
	projectSearchActive        bool
	projectSearchQuery         string
	projectLayout              *tview.Flex
	tableContainer             *tview.Table
	tableContainerData         map[dto.ContainerID]dto.Container
	tableContainerDataLock     sync.RWMutex
	containerSearchInput       *tview.InputField
	containerSearchActive      bool
	containerSearchQuery       string
	containerLayout            *tview.Flex
	statusBar                  *tview.TextView
	tableContainerLog          *tview.TextView
	tableContainerLogData      []string
	logPaused                  bool
	logPausedLock              sync.RWMutex
	logShowTimestamp           bool
	logShowTimestampLock       sync.RWMutex
	logFilter                  string
	logFilterLock              sync.RWMutex
	statusTimer                *time.Timer
	logFilterInput             *tview.InputField
	logLayout                  *tview.Flex
	currentView                currentView
	currentViewLock            sync.RWMutex
	currentProjectID           string
	currentContainerID         string
	currentContainerName       string
	currentContainerService    string
	containerRefreshPaused     bool
	containerRefreshPausedLock sync.RWMutex
	containerRefreshTimer      *time.Timer
	containerRefreshTimerLock  sync.Mutex
	projectRefreshPaused       bool
	projectRefreshPausedLock   sync.RWMutex
	projectRefreshTimer        *time.Timer
	projectRefreshTimerLock    sync.Mutex
	projectSortColumn          projectSortColumn
	projectSortAsc             bool
	containerSortColumn        containerSortColumn
	containerSortAsc           bool
	currentProjectName         string
	currentTableWidth          atomic.Int32
	containerDisappeared       bool
	containerDisappearedLock   sync.RWMutex
	containerDisappearedModal  *tview.Modal
	helpModal                  *tview.Grid
	helpTextView               *tview.TextView
	headerView                 *tview.TextView
	requestData                chan RequestData
	logger                     *slog.Logger
}

func NewTui(logger *slog.Logger) *Tui {
	setupStyles()

	app := tview.NewApplication()

	// Create tables
	tableProject := createTable()
	tableProject.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.ColorNavy).
		Foreground(tcell.ColorBlack).
		Bold(true))

	tableContainer := createTable()

	// Create search inputs
	projectSearchInput := createSearchInput()
	containerSearchInput := createSearchInput()
	logFilterInput := createSearchInput()

	// Create status bar
	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetTextAlign(tview.AlignCenter)
	statusBar.SetBackgroundColor(tcell.ColorBlack)
	statusBar.SetTextColor(tcell.ColorWhite)

	// Create log view
	tableContainerLog := tview.NewTextView()
	tableContainerLog.SetScrollable(true)
	tableContainerLog.SetWordWrap(true)
	tableContainerLog.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	tableContainerLog.SetDynamicColors(true)
	tableContainerLog.SetBackgroundColor(tcell.ColorBlack)
	tableContainerLog.SetTextColor(tcell.ColorWhite)

	// Create modal
	containerDisappearedModal := tview.NewModal().
		SetText("").
		AddButtons([]string{"OK"}).
		SetBackgroundColor(tcell.ColorBlack).
		SetTextColor(tcell.ColorWhite).
		SetButtonBackgroundColor(tcell.ColorDarkCyan).
		SetButtonTextColor(tcell.ColorWhite)

	// Create help modal
	helpText := `                 [cyan::b]Keyboard Shortcuts[-::-]

  [cyan]Filtering:[-]
    /    Activate filter     c    Clear filter

  [cyan]Containers list:[-]
    r    Start/Restart       x    Stop
    d    Remove container    s    Open shell

  [cyan]Container logs:[-]
    p    Pause/unpause       t    Toggle timestamps`

	helpTextView := tview.NewTextView()
	helpTextView.SetText(helpText)
	helpTextView.SetTextAlign(tview.AlignLeft)
	helpTextView.SetDynamicColors(true)
	helpTextView.SetBackgroundColor(tcell.ColorBlack)
	helpTextView.SetBorder(true)
	helpTextView.SetBorderColor(tcell.ColorDarkCyan)
	helpTextView.SetBorderPadding(1, 1, 2, 2)

	helpModal := tview.NewGrid().
		SetColumns(0, 59, 0).
		SetRows(0, 18, 0).
		AddItem(helpTextView, 1, 1, 1, 1, 0, 0, true)

	// Create header view
	headerView := tview.NewTextView()
	headerView.SetDynamicColors(true)
	headerView.SetBackgroundColor(tcell.ColorBlack)
	headerView.SetTextAlign(tview.AlignLeft)
	headerView.SetText(" [white::b]c8s[-::]")

	// Create layouts
	projectLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerView, 1, 0, false).
		AddItem(tableProject, 0, 1, true)

	containerLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerView, 1, 0, false).
		AddItem(tableContainer, 0, 1, true)

	logLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(headerView, 1, 0, false).
		AddItem(tableContainerLog, 0, 1, true)

	pages := tview.NewPages()

	tui := &Tui{
		app:                       app,
		pages:                     pages,
		logger:                    logger,
		tableProject:              tableProject,
		tableProjectData:          make(map[dto.ProjectID]dto.Project),
		projectSearchInput:        projectSearchInput,
		projectLayout:             projectLayout,
		tableContainer:            tableContainer,
		tableContainerData:        make(map[dto.ContainerID]dto.Container),
		containerSearchInput:      containerSearchInput,
		containerLayout:           containerLayout,
		statusBar:                 statusBar,
		tableContainerLog:         tableContainerLog,
		logFilterInput:            logFilterInput,
		logLayout:                 logLayout,
		logShowTimestamp:          false,
		containerDisappearedModal: containerDisappearedModal,
		helpModal:                 helpModal,
		helpTextView:              helpTextView,
		headerView:                headerView,
		requestData:               make(chan RequestData),
		currentView:               viewProjectList,
		projectSortColumn:         projectSortCPU,
		projectSortAsc:            false,
		containerSortColumn:       containerSortCPU,
		containerSortAsc:          false,
		currentProjectName:        "",
		currentTableWidth:         atomic.Int32{},
	}

	// Add pages
	pages.AddPage("projectList", projectLayout, true, true)
	pages.AddPage("containerList", containerLayout, true, false)
	pages.AddPage("logs", logLayout, true, false)
	pages.AddPage("modal", containerDisappearedModal, false, false)
	pages.AddPage("help", helpModal, true, false)

	// Hook to update column widths on resize
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		screenWidth, _ := screen.Size()
		tui.currentTableWidth.Store(int32(screenWidth - 2))
		return false
	})

	// Setup handlers and callbacks
	tui.setupProjectTableHandler()
	tui.setupContainerTableHandler()
	tui.setupLogViewHandler()
	tui.setupSearchCallbacks()
	tui.setupModalCallbacks()

	return tui
}

func (t *Tui) getSortIndicator(isActive bool, isAsc bool) string {
	if !isActive {
		return ""
	}
	if isAsc {
		return "[fuchsia]↑[-]"
	}
	return "[fuchsia]↓[-]"
}

func (t *Tui) RenderProjectHeader() {
	nameIndicator := t.getSortIndicator(t.projectSortColumn == projectSortName, t.projectSortAsc)
	cpuIndicator := t.getSortIndicator(t.projectSortColumn == projectSortCPU, t.projectSortAsc)
	memIndicator := t.getSortIndicator(t.projectSortColumn == projectSortMemory, t.projectSortAsc)
	contIndicator := t.getSortIndicator(t.projectSortColumn == projectSortContainers, t.projectSortAsc)

	t.tableProject.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]CONT%s[-::-]", contIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader(project string) {
	nameIndicator := t.getSortIndicator(t.containerSortColumn == containerSortName, t.containerSortAsc)
	cpuIndicator := t.getSortIndicator(t.containerSortColumn == containerSortCPU, t.containerSortAsc)
	memIndicator := t.getSortIndicator(t.containerSortColumn == containerSortMemory, t.containerSortAsc)
	statusIndicator := t.getSortIndicator(t.containerSortColumn == containerSortStatus, t.containerSortAsc)

	// Use stored width from BeforeDrawFunc to get current value
	w := int(t.currentTableWidth.Load())
	statusMaxWidth := t.calculateStatusWidth(w)

	// Create or update header cells
	t.tableContainer.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.tableContainer.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]STATUS%s[-::-]", statusIndicator)).SetAlign(tview.AlignLeft).SetExpansion(0).SetMaxWidth(statusMaxWidth).SetSelectable(false))
	t.tableContainer.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableContainer.SetFixed(1, 0)
}

func (t *Tui) calculateStatusWidth(tableWidth int) int {
	// Minimum space needed for other columns: NAME(20) + CPU(7) + MEM(7) = 44
	minOtherWidth := 44
	availableForStatus := tableWidth - minOtherWidth

	// If space is tight, STATUS gets squeezed first
	if availableForStatus < 4 {
		return 4 // Absolute minimum to show something like "run…"
	}
	if availableForStatus < 12 {
		return availableForStatus
	}
	return 12 // Max width for STATUS
}

func (t *Tui) drawProjects() {
	projects := slices.Collect(maps.Values(t.tableProjectData))
	projects = filterProjects(projects, t.projectSearchQuery)
	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		return compareProjects(a, b, t.projectSortColumn, t.projectSortAsc)
	})

	t.tableProject.Clear()
	t.RenderProjectHeader()

	offset := 0
	for index, project := range projects {
		rowIndex := index + 1 + offset

		// Column 0: Project name with warning symbol if needed
		projectName := project.Name
		if project.CPUPercentage > 80 || project.MemoryPercentage > 80 {
			projectName = "[yellow]⚠[-] " + projectName
		}
		t.tableProject.SetCell(rowIndex, 0, tview.NewTableCell(projectName))

		// Column 1: CPU
		t.tableProject.SetCell(
			rowIndex,
			1,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.CPUPercentage)),
			).
				SetAlign(tview.AlignRight),
		)

		// Column 2: Memory
		t.tableProject.SetCell(
			rowIndex,
			2,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", max(0, project.MemoryPercentage)),
			).
				SetAlign(tview.AlignRight),
		)

		// Column 3: Container count
		t.tableProject.SetCell(
			rowIndex,
			3,
			tview.NewTableCell(
				fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState)),
			).
				SetAlign(tview.AlignRight),
		)
	}

	// Update header to show current state
	t.updateHeader()
}

func (t *Tui) drawContainers() {
	t.tableContainerDataLock.RLock()
	containers := slices.SortedStableFunc(maps.Values(t.tableContainerData), func(a, b dto.Container) int {
		return compareContainers(a, b, t.containerSortColumn, t.containerSortAsc)
	})
	t.tableContainerDataLock.RUnlock()

	t.tableContainer.Clear()
	t.tableProjectDataLock.RLock()
	t.RenderContainerHeader(t.tableProjectData[dto.ProjectID(t.currentProjectID)].Name)
	t.tableProjectDataLock.RUnlock()

	index := 0
	for _, container := range containers {
		// Filter by project
		if string(container.Project.ID) != t.currentProjectID {
			continue
		}

		// Filter by search query
		if !fuzzyMatch(container.Service, t.containerSearchQuery) {
			continue
		}

		index += 1

		// Column 0: Container service name with warning symbol
		serviceName := container.Service
		if container.CPUPercentage > 80 || container.MemoryPercentage > 80 {
			serviceName = "[yellow]⚠[-] " + serviceName
		}
		t.tableContainer.SetCell(index, 0, tview.NewTableCell(serviceName))

		// Column 1: Status with color
		statusText := container.Status
		if statusText == "" {
			statusText = "unknown"
		}

		// Determine display text and color
		displayText := statusText
		var statusColor string
		switch strings.ToLower(statusText) {
		case "running":
			statusColor = "green"
		case "exited", "dead", "removing":
			statusColor = "red"
		case "paused":
			statusColor = "yellow"
		case "restarting":
			statusColor = "fuchsia"
		case "created":
			statusColor = "cyan"
		default:
			statusColor = "gray"
		}

		// Show pending action with ellipsis character (single char … not three dots)
		if container.PendingAction != "" {
			displayText = container.PendingAction + "…"
			statusColor = "fuchsia"
		}

		// Truncate status text if needed based on available screen width
		maxWidth := t.calculateStatusWidth(int(t.currentTableWidth.Load()))
		if len(displayText) > maxWidth {
			// Truncate and add ellipsis if needed
			if maxWidth > 3 {
				displayText = displayText[:maxWidth-1] + "…"
			} else {
				displayText = "…"
			}
		}

		t.tableContainer.SetCell(
			index,
			1,
			tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", statusColor, displayText)).
				SetAlign(tview.AlignLeft),
		)

		// Column 2: CPU
		t.tableContainer.SetCell(
			index,
			2,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", container.CPUPercentage),
			).
				SetAlign(tview.AlignRight),
		)

		// Column 3: Memory
		t.tableContainer.SetCell(
			index,
			3,
			tview.NewTableCell(
				fmt.Sprintf("%.2f%%", container.MemoryPercentage),
			).
				SetAlign(tview.AlignRight),
		)
	}

	// Update header to show current state
	t.updateHeader()
}

func (t *Tui) drawContainerLog() {
	t.tableContainerLog.Clear()

	t.logPausedLock.RLock()
	paused := t.logPaused
	t.logPausedLock.RUnlock()

	t.logFilterLock.RLock()
	filter := t.logFilter
	t.logFilterLock.RUnlock()

	t.logShowTimestampLock.RLock()
	showTimestamp := t.logShowTimestamp
	t.logShowTimestampLock.RUnlock()

	statusIndicators := ""
	if paused {
		statusIndicators += " [yellow](PAUSED)[-]"
	}
	if filter != "" {
		statusIndicators += fmt.Sprintf(" [green](filter: %s)[-]", filter)
	}
	if showTimestamp {
		statusIndicators += " [gray](time)[-]"
	}
	t.tableContainerLog.SetTitle(fmt.Sprintf(" [::b]logs%s ", statusIndicators))

	// Apply filter and colorize
	var logs []string
	for _, line := range t.tableContainerLogData {
		if filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
			continue
		}
		logs = append(logs, colorizeLogLine(line, showTimestamp))
	}

	t.tableContainerLog.SetText(strings.Join(logs, ""))

	// Auto-scroll to bottom
	if !paused {
		t.tableContainerLog.ScrollToEnd()
	}

	// Update header to show current state
	t.updateHeader()
}

func (t *Tui) showStatusMessage(message string) {
	t.statusBar.SetText("[red]" + message + "[-]")
	t.containerLayout.AddItem(t.statusBar, 1, 0, false)

	// Cancel previous timer if exists
	if t.statusTimer != nil {
		t.statusTimer.Stop()
	}

	// Clear message after duration
	t.statusTimer = time.AfterFunc(statusMessageDuration, func() {
		t.app.QueueUpdateDraw(func() {
			t.containerLayout.RemoveItem(t.statusBar)
			t.statusBar.SetText("")
		})
	})
}

func (t *Tui) pauseContainerRefresh() {
	t.containerRefreshPausedLock.Lock()
	t.containerRefreshPaused = true
	t.containerRefreshPausedLock.Unlock()

	t.containerRefreshTimerLock.Lock()
	// Cancel previous timer if exists
	if t.containerRefreshTimer != nil {
		t.containerRefreshTimer.Stop()
	}

	// Resume refresh after duration
	t.containerRefreshTimer = time.AfterFunc(refreshPauseDuration, func() {
		t.containerRefreshPausedLock.Lock()
		t.containerRefreshPaused = false
		t.containerRefreshPausedLock.Unlock()
	})
	t.containerRefreshTimerLock.Unlock()
}

func (t *Tui) setProjectSort(col projectSortColumn) {
	if t.projectSortColumn == col {
		// Toggle direction if same column
		t.projectSortAsc = !t.projectSortAsc
	} else {
		// New column: default to descending for metrics, ascending for name
		t.projectSortColumn = col
		t.projectSortAsc = col == projectSortName
	}
	t.drawProjects()
}

func (t *Tui) setContainerSort(col containerSortColumn) {
	if t.containerSortColumn == col {
		// Toggle direction if same column
		t.containerSortAsc = !t.containerSortAsc
	} else {
		// New column: default to descending for metrics, ascending for name and status
		t.containerSortColumn = col
		t.containerSortAsc = col == containerSortName || col == containerSortStatus
	}
	t.drawContainers()
}

func (t *Tui) pauseProjectRefresh() {
	t.projectRefreshPausedLock.Lock()
	t.projectRefreshPaused = true
	t.projectRefreshPausedLock.Unlock()

	t.projectRefreshTimerLock.Lock()
	// Cancel previous timer if exists
	if t.projectRefreshTimer != nil {
		t.projectRefreshTimer.Stop()
	}

	// Resume refresh after duration
	t.projectRefreshTimer = time.AfterFunc(refreshPauseDuration, func() {
		t.projectRefreshPausedLock.Lock()
		t.projectRefreshPaused = false
		t.projectRefreshPausedLock.Unlock()
	})
	t.projectRefreshTimerLock.Unlock()
}

func (t *Tui) GetRequestData() <-chan RequestData {
	return t.requestData
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
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

				t.projectRefreshPausedLock.RLock()
				paused := t.projectRefreshPaused
				t.projectRefreshPausedLock.RUnlock()

				if paused {
					continue
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

				t.containerRefreshPausedLock.RLock()
				paused := t.containerRefreshPaused
				t.containerRefreshPausedLock.RUnlock()

				if paused {
					continue
				}

				response := make(chan []dto.Container)
				t.requestData <- &RequestProject{
					ProjectID: dto.ProjectID(t.currentProjectID),
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
				t.logPausedLock.RLock()
				paused := t.logPaused
				t.logPausedLock.RUnlock()

				if paused {
					continue
				}

				t.containerDisappearedLock.RLock()
				disappeared := t.containerDisappeared
				t.containerDisappearedLock.RUnlock()

				if disappeared {
					// If we haven't started log collection yet for the reappeared container
					if ctxCancel == nil {
						// Try to find a container with same service name and project
						t.logger.InfoContext(ctx, "checking for container reappearance",
							slog.String("service", t.currentContainerService),
							slog.String("project", t.currentProjectID))

						responseProject := make(chan []dto.Container)
						t.requestData <- &RequestProject{
							ProjectID: dto.ProjectID(t.currentProjectID),
							Response:  responseProject,
						}
						containers := <-responseProject

						var foundContainer *dto.Container
						for _, container := range containers {
							t.logger.InfoContext(ctx, "checking container",
								slog.String("container_service", container.Service),
								slog.String("looking_for", t.currentContainerService))
							if container.Service == t.currentContainerService && string(container.Project.ID) == t.currentProjectID {
								foundContainer = &container
								break
							}
						}

						if foundContainer != nil {
							// Container reappeared! Start log collection ONCE
							t.logger.InfoContext(ctx, "container reappeared, starting log collection",
								slog.String("service", foundContainer.Service),
								slog.String("new_id", string(foundContainer.ID)))

							t.currentContainerID = string(foundContainer.ID)
							t.currentContainerName = foundContainer.Name

							// Start log collection for the new container
							response := make(chan dto.Container)
							t.requestData <- &RequestContainerLog{
								ContainerID: dto.ContainerID(t.currentContainerID),
								Response:    response,
							}
							c := <-response

							if c.ID == "" {
								// Still not available, keep waiting
								t.logger.InfoContext(ctx, "container reappeared but logs not ready yet")
								continue
							}

							// Got container, save LogCancel
							ctxCancel = c.LogCancel
							t.logger.InfoContext(ctx, "log collection started, waiting for logs...")
						}

						// Still disappeared or just started log collection, continue waiting
						continue
					}

					// Log collection is running, check if we have logs now
					response := make(chan dto.Container)
					t.requestData <- &RequestContainerLog{
						ContainerID: dto.ContainerID(t.currentContainerID),
						Response:    response,
					}
					c := <-response

					if c.ID == "" {
						// Container disappeared again!
						t.logger.InfoContext(ctx, "container disappeared again")
						if ctxCancel != nil {
							ctxCancel()
							ctxCancel = nil
						}
						continue
					}

					// Wait until we have actual logs before hiding the modal
					if len(c.Logs) == 0 {
						t.logger.InfoContext(ctx, "still waiting for logs...")
						continue
					}

					// We have logs! Update them and close the modal
					t.logger.InfoContext(ctx, "got logs, closing modal", slog.Int("log_count", len(c.Logs)))

					t.containerDisappearedLock.Lock()
					t.containerDisappeared = false
					t.containerDisappearedLock.Unlock()

					// Update logs BEFORE queuing the draw
					t.tableContainerLogData = c.Logs

					t.app.QueueUpdateDraw(func() {
						t.drawContainerLog()
						t.pages.HidePage("modal")
					})

					// Continue to next cycle
					continue
				}

				t.logger.InfoContext(ctx, "fetching logs for container", slog.String("container_id", t.currentContainerID))

				// First time we enter this view, start the log collection
				if ctxCancel == nil {
					response := make(chan dto.Container)
					t.requestData <- &RequestContainerLog{
						ContainerID: dto.ContainerID(t.currentContainerID),
						Response:    response,
					}
					c := <-response
					if c.ID == "" {
						// Container no longer exists, show modal
						containerName := t.currentContainerName
						containerID := t.currentContainerID

						t.containerDisappearedLock.Lock()
						t.containerDisappeared = true
						t.containerDisappearedLock.Unlock()

						t.app.QueueUpdateDraw(func() {
							t.containerDisappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", containerName, containerID[:12]))
							t.pages.ShowPage("modal")
						})
						continue
					}
					ctxCancel = c.LogCancel
					// Skip the immediate second request on first start
					continue
				}

				// Get the latest logs (only when ctxCancel is already set)
				response := make(chan dto.Container)
				t.requestData <- &RequestContainerLog{
					ContainerID: dto.ContainerID(t.currentContainerID),
					Response:    response,
				}

				c := <-response

				if c.ID == "" {
					// Container no longer exists, show modal
					if ctxCancel != nil {
						ctxCancel()
						ctxCancel = nil
					}

					containerName := t.currentContainerName
					containerID := t.currentContainerID

					t.containerDisappearedLock.Lock()
					t.containerDisappeared = true
					t.containerDisappearedLock.Unlock()

					t.app.QueueUpdateDraw(func() {
						t.containerDisappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", containerName, containerID[:12]))
						t.pages.ShowPage("modal")
					})
					continue
				}

				// Only update logs if we have some (don't erase existing logs with empty array)
				if len(c.Logs) > 0 {
					t.tableContainerLogData = c.Logs
				}

				t.app.QueueUpdateDraw(func() {
					t.drawContainerLog()
				})
			}
		}
	}
}

func (t *Tui) Render(ctx context.Context) {
	go t.getData(ctx)

	if err := t.app.SetRoot(t.pages, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		os.Exit(1)
	}
}
