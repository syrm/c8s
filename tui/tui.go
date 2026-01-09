package tui

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/syrm/c8s/dto"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)


type Tui struct {
	app                        *tview.Application
	pages                      *tview.Pages
	tableProject               *tview.Table
	tableProjectData           map[dto.ProjectID]dto.Project
	tableProjectDataLock       sync.RWMutex
	projectSearchInput         *tview.InputField
	projectSearchQuery         string
	projectSearchQueryLock     sync.RWMutex
	projectLayout              *tview.Flex
	tableContainer             *tview.Table
	tableContainerData         map[dto.ContainerID]dto.Container
	tableContainerDataLock     sync.RWMutex
	containerSearchInput       *tview.InputField
	containerSearchQuery       string
	containerSearchQueryLock   sync.RWMutex
	containerLayout            *tview.Flex
	statusBar                  *tview.TextView
	tableContainerLog          *tview.TextView
	tableContainerLogData      []string
	tableContainerLogDataLock  sync.RWMutex
	logPaused                  atomic.Bool
	logShowTimestamp           atomic.Bool
	logFilter                  string
	logFilterLock              sync.RWMutex
	statusTimer                *time.Timer
	statusTimerLock            sync.Mutex
	logFilterInput             *tview.InputField
	logLayout                  *tview.Flex
	currentView                currentView
	currentViewLock            sync.RWMutex
	currentProjectID           string
	currentContainerID         string
	currentContainerName       string
	currentContainerService    string
	currentContainerLock       sync.RWMutex // Protects currentProjectID, currentContainerID, currentContainerName, currentContainerService
	containerRefreshPaused     atomic.Bool
	containerRefreshTimer      *time.Timer
	containerRefreshTimerLock  sync.Mutex
	projectRefreshPaused       atomic.Bool
	projectRefreshTimer        *time.Timer
	projectRefreshTimerLock    sync.Mutex
	projectSortColumn          projectSortColumn
	projectSortAsc             bool
	projectSortLock            sync.RWMutex // Protects projectSortColumn and projectSortAsc
	containerSortColumn        containerSortColumn
	containerSortAsc           bool
	containerSortLock          sync.RWMutex // Protects containerSortColumn and containerSortAsc
	currentProjectName         string       // Protected by currentContainerLock
	currentTableWidth          atomic.Int32
	containerDisappeared       atomic.Bool
	containerDisappearedModal  *tview.Modal
	helpModal                  *tview.Grid
	helpTextView               *tview.TextView
	headerView                 *tview.TextView
	requestData                chan dto.RequestData
	logger                     *slog.Logger
	closing                    atomic.Bool
	// actionsContext tracks goroutines for container actions (stop, restart, remove)
	actionsCtx        context.Context
	actionsCancel     context.CancelFunc
	actionsCancelLock sync.Mutex
	actionsWG         sync.WaitGroup
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
		containerDisappearedModal: containerDisappearedModal,
		helpModal:                 helpModal,
		helpTextView:              helpTextView,
		headerView:                headerView,
		requestData:               make(chan dto.RequestData),
		currentView:        viewProjectList,
		projectSortColumn:  projectSortCPU,
		containerSortColumn: containerSortCPU,
	}

	// Initialize actions context for container action goroutines
	tui.actionsCtx, tui.actionsCancel = context.WithCancel(context.Background())

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

// stopTimer stops a timer if it hasn't already fired, preventing resource leaks.
// Safe to call multiple times on the same timer.
func stopTimer(t *time.Timer) {
	if t != nil {
		if !t.Stop() {
			// If the timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.C:
			default:
			}
		}
	}
}

// stopTicker stops a ticker if it hasn't already fired, preventing resource leaks.
// Safe to call multiple times on the same ticker.
func stopTicker(t *time.Ticker) {
	if t != nil {
		t.Stop()
		// Drain the channel to prevent goroutine leak
		select {
		case <-t.C:
		default:
		}
	}
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
	sortCol, sortAsc := t.getProjectSort()
	nameIndicator := t.getSortIndicator(sortCol == projectSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == projectSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == projectSortMemory, sortAsc)
	contIndicator := t.getSortIndicator(sortCol == projectSortContainers, sortAsc)

	t.tableProject.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.tableProject.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.tableProject.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]CONT%s[-::-]", contIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.tableProject.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader() {
	sortCol, sortAsc := t.getContainerSort()
	nameIndicator := t.getSortIndicator(sortCol == containerSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == containerSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == containerSortMemory, sortAsc)
	statusIndicator := t.getSortIndicator(sortCol == containerSortStatus, sortAsc)

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
	// Copy data under lock to avoid race conditions
	t.tableProjectDataLock.RLock()
	projects := make([]dto.Project, 0, len(t.tableProjectData))
	for _, p := range t.tableProjectData {
		projects = append(projects, p)
	}
	t.tableProjectDataLock.RUnlock()

	projects = filterProjects(projects, t.getProjectSearchQuery())
	sortCol, sortAsc := t.getProjectSort()
	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		return compareProjects(a, b, sortCol, sortAsc)
	})

	t.tableProject.Clear()
	t.RenderProjectHeader()

	for index, project := range projects {
		rowIndex := index + 1

		// Column 0: Project name with warning symbol if needed
		projectName := project.Name
		if project.CPUPercentage > resourceWarningThreshold || project.MemoryPercentage > resourceWarningThreshold {
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
	// Copy data under lock to avoid race conditions
	t.tableContainerDataLock.RLock()
	containers := make([]dto.Container, 0, len(t.tableContainerData))
	for _, c := range t.tableContainerData {
		containers = append(containers, c)
	}
	t.tableContainerDataLock.RUnlock()

	currentProjectID := t.getCurrentProjectID()

	// Sort the copied data (no lock needed)
	sortCol, sortAsc := t.getContainerSort()
	slices.SortStableFunc(containers, func(a, b dto.Container) int {
		return compareContainers(a, b, sortCol, sortAsc)
	})

	t.tableContainer.Clear()
	t.RenderContainerHeader()

	index := 0
	for _, container := range containers {
		// Filter by project
		if string(container.Project.ID) != currentProjectID {
			continue
		}

		// Filter by search query
		if !fuzzyMatch(container.Service, t.getContainerSearchQuery()) {
			continue
		}

		index++

		// Column 0: Container service name with warning symbol
		serviceName := container.Service
		if container.CPUPercentage > resourceWarningThreshold || container.MemoryPercentage > resourceWarningThreshold {
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
		case dto.StatusRunning:
			statusColor = "green"
		case dto.StatusExited, "dead", dto.StatusRemoving:
			statusColor = "red"
		case dto.StatusPaused:
			statusColor = "yellow"
		case dto.StatusRestarting:
			statusColor = "fuchsia"
		case dto.StatusCreated:
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

	paused := t.getLogPaused()
	filter := t.getLogFilter()
	showTimestamp := t.getLogShowTimestamp()
	logData := t.getTableContainerLogData()

	// Apply filter and colorize
	var logs []string
	for _, line := range logData {
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
	// Escape brackets to prevent tview from interpreting them as color tags
	escaped := strings.ReplaceAll(message, "[", "[[]")
	t.statusBar.SetText("[red]" + escaped + "[-]")
	t.containerLayout.AddItem(t.statusBar, 1, 0, false)

	// Cancel previous timer if exists - protected by mutex
	t.statusTimerLock.Lock()
	defer t.statusTimerLock.Unlock()

	if t.statusTimer != nil {
		if !t.statusTimer.Stop() {
			// Timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.statusTimer.C:
			default:
			}
		}
	}

	// Clear message after duration
	t.statusTimer = time.AfterFunc(statusMessageDuration, func() {
		// Check if TUI is closing to avoid race with cleanup
		if t.closing.Load() {
			return
		}

		t.app.QueueUpdateDraw(func() {
			t.containerLayout.RemoveItem(t.statusBar)
			t.statusBar.SetText("")
		})
	})
}

func (t *Tui) pauseContainerRefresh() {
	t.containerRefreshPaused.Store(true)

	t.containerRefreshTimerLock.Lock()
	// Cancel previous timer if exists
	if t.containerRefreshTimer != nil {
		if !t.containerRefreshTimer.Stop() {
			// Timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.containerRefreshTimer.C:
			default:
			}
		}
	}

	// Resume refresh after duration
	t.containerRefreshTimer = time.AfterFunc(refreshPauseDuration, func() {
		if !t.closing.Load() {
			t.containerRefreshPaused.Store(false)
		}
	})
	t.containerRefreshTimerLock.Unlock()
}

func (t *Tui) setProjectSort(col projectSortColumn) {
	currentCol, currentAsc := t.getProjectSort()
	if currentCol == col {
		// Toggle direction if same column
		t.setProjectSortState(col, !currentAsc)
	} else {
		// New column: default to descending for metrics, ascending for name
		t.setProjectSortState(col, col == projectSortName)
	}
	t.drawProjects()
}

func (t *Tui) setContainerSort(col containerSortColumn) {
	currentCol, currentAsc := t.getContainerSort()
	if currentCol == col {
		// Toggle direction if same column
		t.setContainerSortState(col, !currentAsc)
	} else {
		// New column: default to descending for metrics, ascending for name and status
		t.setContainerSortState(col, col == containerSortName || col == containerSortStatus)
	}
	t.drawContainers()
}

func (t *Tui) pauseProjectRefresh() {
	t.projectRefreshPaused.Store(true)

	t.projectRefreshTimerLock.Lock()
	// Cancel previous timer if exists
	if t.projectRefreshTimer != nil {
		if !t.projectRefreshTimer.Stop() {
			// Timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.projectRefreshTimer.C:
			default:
			}
		}
	}

	// Resume refresh after duration
	t.projectRefreshTimer = time.AfterFunc(refreshPauseDuration, func() {
		if !t.closing.Load() {
			t.projectRefreshPaused.Store(false)
		}
	})
	t.projectRefreshTimerLock.Unlock()
}

func (t *Tui) GetRequestData() <-chan dto.RequestData {
	return t.requestData
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer stopTicker(ticker)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			cv := t.getCurrentView()
			switch cv {
			case viewProjectList:
				t.refreshProjectList()

			case viewProject:
				t.refreshContainerList()

			case viewContainerLog:
				t.refreshContainerLog(ctx)
			}
		}
	}
}

func (t *Tui) refreshProjectList() {
	if t.getProjectRefreshPaused() {
		return
	}

	response := make(chan []dto.Project, 1)
	timer := time.NewTimer(channelTimeout)
	defer stopTimer(timer)

	select {
	case t.requestData <- &dto.RequestProjectList{Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending project list request")
		return
	}

	select {
	case projects := <-response:
		t.tableProjectDataLock.Lock()
		// Update map in place instead of recreating it to reduce GC pressure
		// Build a set of active project IDs
		activeProjects := make(map[dto.ProjectID]struct{}, len(projects))
		for _, p := range projects {
			t.tableProjectData[p.ID] = p
			activeProjects[p.ID] = struct{}{}
		}
		// Remove projects that are no longer present
		for id := range t.tableProjectData {
			if _, exists := activeProjects[id]; !exists {
				delete(t.tableProjectData, id)
			}
		}
		t.tableProjectDataLock.Unlock()

		t.app.QueueUpdateDraw(func() {
			t.drawProjects()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for project list response")
	}
}

func (t *Tui) refreshContainerList() {
	if t.getContainerRefreshPaused() {
		return
	}

	currentProjectID := t.getCurrentProjectID()
	response := make(chan []dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer stopTimer(timer)

	select {
	case t.requestData <- &dto.RequestProject{ProjectID: dto.ProjectID(currentProjectID), Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending container list request")
		return
	}

	select {
	case containers := <-response:
		t.tableContainerDataLock.Lock()
		// Update map in place instead of recreating it to reduce GC pressure
		// Build a set of active container IDs
		activeContainers := make(map[dto.ContainerID]struct{}, len(containers))
		for _, c := range containers {
			t.tableContainerData[c.ID] = c
			activeContainers[c.ID] = struct{}{}
		}
		// Remove containers that are no longer present
		for id := range t.tableContainerData {
			if _, exists := activeContainers[id]; !exists {
				delete(t.tableContainerData, id)
			}
		}
		t.tableContainerDataLock.Unlock()

		t.app.QueueUpdateDraw(func() {
			t.drawContainers()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for container list response")
	}
}

func (t *Tui) refreshContainerLog(ctx context.Context) {
	if t.getLogPaused() {
		return
	}

	if t.getContainerDisappeared() {
		t.handleDisappearedContainer(ctx)
		return
	}

	currentContainerID := t.getCurrentContainerID()
	t.logger.DebugContext(ctx, "fetching logs for container", slog.String("container_id", currentContainerID))

	// Always update logs - the Docker layer manages the log collection lifecycle
	t.updateLogs(ctx)
}

func (t *Tui) handleDisappearedContainer(ctx context.Context) {
	currentContainerID := t.getCurrentContainerID()

	// Log collection is running, check if we have logs now
	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer stopTimer(timer)

	select {
	case t.requestData <- &dto.RequestContainerLog{ContainerID: dto.ContainerID(currentContainerID), Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending container log request in handleDisappearedContainer")
		return
	}

	var c dto.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in handleDisappearedContainer")
		return
	}

	if c.ID == "" {
		// Container disappeared again!
		t.logger.DebugContext(ctx, "container disappeared again")
		t.tryReconnectContainer(ctx)
		return
	}

	// Wait until we have actual logs before hiding the modal
	if len(c.Logs) == 0 {
		t.logger.DebugContext(ctx, "still waiting for logs...")
		return
	}

	// We have logs! Update them and close the modal
	t.logger.DebugContext(ctx, "got logs, closing modal", slog.Int("log_count", len(c.Logs)))
	t.setContainerDisappeared(false)
	t.setTableContainerLogData(c.Logs)

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
		t.pages.HidePage("modal")
	})
}

func (t *Tui) tryReconnectContainer(ctx context.Context) {
	currentContainerService := t.getCurrentContainerService()
	currentProjectID := t.getCurrentProjectID()

	t.logger.DebugContext(ctx, "checking for container reappearance",
		slog.String("service", currentContainerService),
		slog.String("project", currentProjectID))

	responseProject := make(chan []dto.Container, 1)
	timer1 := time.NewTimer(channelTimeout)
	defer stopTimer(timer1)

	select {
	case t.requestData <- &dto.RequestProject{ProjectID: dto.ProjectID(currentProjectID), Response: responseProject}:
	case <-timer1.C:
		t.logger.Warn("timeout sending project request in tryReconnectContainer")
		return
	}

	var containers []dto.Container
	select {
	case containers = <-responseProject:
	case <-timer1.C:
		t.logger.Warn("timeout waiting for project response in tryReconnectContainer")
		return
	}

	var foundContainer *dto.Container
	for _, container := range containers {
		t.logger.DebugContext(ctx, "checking container",
			slog.String("container_service", container.Service),
			slog.String("looking_for", currentContainerService))
		if container.Service == currentContainerService && string(container.Project.ID) == currentProjectID {
			foundContainer = &container
			break
		}
	}

	if foundContainer == nil {
		return
	}

	// Container reappeared! Start log collection ONCE
	t.logger.InfoContext(ctx, "container reappeared, starting log collection",
		slog.String("service", foundContainer.Service),
		slog.String("new_id", string(foundContainer.ID)))

	t.setCurrentContainerInfo(string(foundContainer.ID), foundContainer.Name, foundContainer.Service)

	// Start log collection for the new container
	response := make(chan dto.Container, 1)
	newContainerID := t.getCurrentContainerID()
	timer2 := time.NewTimer(channelTimeout)
	defer stopTimer(timer2)

	select {
	case t.requestData <- &dto.RequestContainerLog{ContainerID: dto.ContainerID(newContainerID), Response: response}:
	case <-timer2.C:
		t.logger.Warn("timeout sending container log request in tryReconnectContainer")
		return
	}

	var c dto.Container
	select {
	case c = <-response:
	case <-timer2.C:
		t.logger.Warn("timeout waiting for container log response in tryReconnectContainer")
		return
	}

	if c.ID == "" {
		t.logger.DebugContext(ctx, "container reappeared but logs not ready yet")
		return
	}

	t.logger.DebugContext(ctx, "log collection started, waiting for logs...")
}

func (t *Tui) startLogCollection(ctx context.Context) {
	currentContainerID := t.getCurrentContainerID()
	currentContainerName := t.getCurrentContainerName()

	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer stopTimer(timer)

	select {
	case t.requestData <- &dto.RequestContainerLog{ContainerID: dto.ContainerID(currentContainerID), Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending container log request in startLogCollection")
		return
	}

	var c dto.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in startLogCollection")
		return
	}

	if c.ID == "" {
		// Container no longer exists, show modal
		t.setContainerDisappeared(true)

		// Safe substring for container ID display
		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.containerDisappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}
}

func (t *Tui) updateLogs(ctx context.Context) {
	currentContainerID := t.getCurrentContainerID()
	currentContainerName := t.getCurrentContainerName()

	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer stopTimer(timer)

	select {
	case t.requestData <- &dto.RequestContainerLog{ContainerID: dto.ContainerID(currentContainerID), Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending container log request in updateLogs")
		return
	}

	var c dto.Container
	select {
	case c = <-response:
	case <-timer.C:
		t.logger.Warn("timeout waiting for container log response in updateLogs")
		return
	}

	if c.ID == "" {
		// Container no longer exists, show modal
		t.setContainerDisappeared(true)

		// Safe substring for container ID display
		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.containerDisappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}

	// Only update logs if we have some (don't erase existing logs with empty array)
	if len(c.Logs) > 0 {
		t.setTableContainerLogData(c.Logs)
	}

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
	})
}

func (t *Tui) Render(ctx context.Context) error {
	go t.getData(ctx)

	if err := t.app.SetRoot(t.pages, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		return err
	}

	// Cleanup on exit
	t.cleanup()
	return nil
}

// cleanup releases resources when the TUI exits
func (t *Tui) cleanup() {
	// Set closing flag to prevent timer callbacks from running
	t.closing.Store(true)

	// Cancel any pending action goroutines and wait for them to finish
	t.actionsCancelLock.Lock()
	if t.actionsCancel != nil {
		t.actionsCancel()
		t.actionsCancel = nil
	}
	t.actionsCancelLock.Unlock()

	// Wait for all action goroutines to complete
	t.actionsWG.Wait()

	// Stop all timers - protected by mutex
	t.statusTimerLock.Lock()
	if t.statusTimer != nil {
		if !t.statusTimer.Stop() {
			// Timer already fired, drain the channel to prevent goroutine leak
			select {
			case <-t.statusTimer.C:
			default:
			}
		}
	}
	t.statusTimerLock.Unlock()

	t.stopContainerRefreshTimer()
	t.stopProjectRefreshTimer()

	// Close request channel to signal docker layer
	close(t.requestData)
}
