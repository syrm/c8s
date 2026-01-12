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

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/syrm/c8s/dto"
	itimer "github.com/syrm/c8s/internal/timer"
)

// Tui manages the terminal user interface.
type Tui struct {
	app    *tview.Application
	pages  *tview.Pages
	header *tview.TextView
	status *StatusBar
	logger *slog.Logger

	// Views
	projectView   ProjectView
	containerView ContainerView
	logView       LogViewState

	// Navigation
	nav NavigationState

	// Refresh control
	projectRefresh   PausableRefresh
	containerRefresh PausableRefresh

	// Modals
	disappearedModal *tview.Modal
	helpModal        *tview.Grid
	helpTextView     *tview.TextView

	// Communication
	requestData chan dto.RequestData

	// Actions
	actions *ActionController

	// Screen width
	tableWidth atomic.Int32

	// Lifecycle
	dataWG  sync.WaitGroup
	closing atomic.Bool
}

// ProjectView holds all project view state.
type ProjectView struct {
	Table  *tview.Table
	Layout *tview.Flex
	Search *SearchState
	Sort   SortState[projectSortColumn]
	Data   SyncMap[dto.ProjectID, dto.Project]
}

// ContainerView holds all container view state.
type ContainerView struct {
	Table  *tview.Table
	Layout *tview.Flex
	Search *SearchState
	Sort   SortState[containerSortColumn]
	Data   SyncMap[dto.ContainerID, dto.Container]
}

// SyncMap provides thread-safe access to a map.
type SyncMap[K comparable, V any] struct {
	data map[K]V
	mu   sync.RWMutex
}

func (m *SyncMap[K, V]) Get(key K) (V, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	return v, ok
}

func (m *SyncMap[K, V]) Set(key K, value V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = make(map[K]V)
	}
	m.data[key] = value
}

func (m *SyncMap[K, V]) Delete(key K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
}

func (m *SyncMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.data)
}

func (m *SyncMap[K, V]) Values() []V {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]V, 0, len(m.data))
	for _, v := range m.data {
		result = append(result, v)
	}
	return result
}

func (m *SyncMap[K, V]) Keys() []K {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]K, 0, len(m.data))
	for k := range m.data {
		result = append(result, k)
	}
	return result
}

func (m *SyncMap[K, V]) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = make(map[K]V)
}

func (m *SyncMap[K, V]) UpdateFrom(items []V, keyFunc func(V) K) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.data == nil {
		m.data = make(map[K]V)
	}

	activeKeys := make(map[K]struct{}, len(items))
	for _, item := range items {
		key := keyFunc(item)
		m.data[key] = item
		activeKeys[key] = struct{}{}
	}

	for k := range m.data {
		if _, exists := activeKeys[k]; !exists {
			delete(m.data, k)
		}
	}
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

	// Create status bar
	status := NewStatusBar()

	// Create log view
	logView := tview.NewTextView()
	logView.SetScrollable(true)
	logView.SetWordWrap(true)
	logView.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	logView.SetDynamicColors(true)
	logView.SetBackgroundColor(tcell.ColorBlack)
	logView.SetTextColor(tcell.ColorWhite)

	// Create modal
	disappearedModal := tview.NewModal().
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
		AddItem(logView, 0, 1, true)

	// Create search states
	projectSearch := NewSearchState()
	containerSearch := NewSearchState()
	logFilterInput := createSearchInput()

	pages := tview.NewPages()

	tui := &Tui{
		app:    app,
		pages:  pages,
		logger: logger,
		header: headerView,
		status: status,

		projectView: ProjectView{
			Table:  tableProject,
			Layout: projectLayout,
			Search: projectSearch,
			Data:   SyncMap[dto.ProjectID, dto.Project]{data: make(map[dto.ProjectID]dto.Project)},
		},
		containerView: ContainerView{
			Table:  tableContainer,
			Layout: containerLayout,
			Search: containerSearch,
			Data:   SyncMap[dto.ContainerID, dto.Container]{data: make(map[dto.ContainerID]dto.Container)},
		},
		logView: LogViewState{
			View:        logView,
			Layout:      logLayout,
			FilterInput: logFilterInput,
		},

		disappearedModal: disappearedModal,
		helpModal:        helpModal,
		helpTextView:     helpTextView,

		requestData: make(chan dto.RequestData),
		actions:     NewActionController(maxConcurrentActions),
	}

	// Set default sort
	tui.projectView.Sort.Set(projectSortCPU, false)
	tui.containerView.Sort.Set(containerSortCPU, false)

	// Set status bar layout
	status.SetLayout(containerLayout)

	// Add pages
	pages.AddPage("projectList", projectLayout, true, true)
	pages.AddPage("containerList", containerLayout, true, false)
	pages.AddPage("logs", logLayout, true, false)
	pages.AddPage("modal", disappearedModal, false, false)
	pages.AddPage("help", helpModal, true, false)

	// Hook to update column widths on resize
	app.SetBeforeDrawFunc(func(screen tcell.Screen) bool {
		screenWidth, _ := screen.Size()
		tui.tableWidth.Store(int32(screenWidth - 2))
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
	sortCol, sortAsc := t.projectView.Sort.Get()
	nameIndicator := t.getSortIndicator(sortCol == projectSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == projectSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == projectSortMemory, sortAsc)
	contIndicator := t.getSortIndicator(sortCol == projectSortContainers, sortAsc)

	t.projectView.Table.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.projectView.Table.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.projectView.Table.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.projectView.Table.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]CONT%s[-::-]", contIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetSelectable(false))
	t.projectView.Table.SetFixed(1, 0)
}

func (t *Tui) RenderContainerHeader() {
	sortCol, sortAsc := t.containerView.Sort.Get()
	nameIndicator := t.getSortIndicator(sortCol == containerSortName, sortAsc)
	cpuIndicator := t.getSortIndicator(sortCol == containerSortCPU, sortAsc)
	memIndicator := t.getSortIndicator(sortCol == containerSortMemory, sortAsc)
	statusIndicator := t.getSortIndicator(sortCol == containerSortStatus, sortAsc)

	w := int(t.tableWidth.Load())
	statusMaxWidth := t.calculateStatusWidth(w)

	t.containerView.Table.SetCell(0, 0, tview.NewTableCell(fmt.Sprintf("[cyan::b]NAME%s[-::-]", nameIndicator)).SetAlign(tview.AlignLeft).SetExpansion(3).SetSelectable(false))
	t.containerView.Table.SetCell(0, 1, tview.NewTableCell(fmt.Sprintf("[cyan::b]STATUS%s[-::-]", statusIndicator)).SetAlign(tview.AlignLeft).SetExpansion(0).SetMaxWidth(statusMaxWidth).SetSelectable(false))
	t.containerView.Table.SetCell(0, 2, tview.NewTableCell(fmt.Sprintf("[cyan::b]CPU%s[-::-]", cpuIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.containerView.Table.SetCell(0, 3, tview.NewTableCell(fmt.Sprintf("[cyan::b]MEM%s[-::-]", memIndicator)).SetAlign(tview.AlignRight).SetExpansion(2).SetMaxWidth(7).SetSelectable(false))
	t.containerView.Table.SetFixed(1, 0)
}

func (t *Tui) calculateStatusWidth(tableWidth int) int {
	minOtherWidth := 44
	availableForStatus := tableWidth - minOtherWidth

	if availableForStatus < 4 {
		return 4
	}
	if availableForStatus < 12 {
		return availableForStatus
	}
	return 12
}

func (t *Tui) drawProjects() {
	projects := t.projectView.Data.Values()
	projects = filterProjects(projects, t.projectView.Search.Query())

	sortCol, sortAsc := t.projectView.Sort.Get()
	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		return compareProjects(a, b, sortCol, sortAsc)
	})

	t.projectView.Table.Clear()
	t.RenderProjectHeader()

	for index, project := range projects {
		rowIndex := index + 1

		projectName := project.Name
		if project.CPUPercentage > resourceWarningThreshold || project.MemoryPercentage > resourceWarningThreshold {
			projectName = "[yellow]⚠[-] " + projectName
		}
		t.projectView.Table.SetCell(rowIndex, 0, tview.NewTableCell(projectName))

		t.projectView.Table.SetCell(rowIndex, 1,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", max(0, project.CPUPercentage))).SetAlign(tview.AlignRight))

		t.projectView.Table.SetCell(rowIndex, 2,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", max(0, project.MemoryPercentage))).SetAlign(tview.AlignRight))

		t.projectView.Table.SetCell(rowIndex, 3,
			tview.NewTableCell(fmt.Sprintf("%d/%d", project.ContainersRunning, len(project.ContainersState))).SetAlign(tview.AlignRight))
	}

	t.updateHeader()
}

func (t *Tui) drawContainers() {
	containers := t.containerView.Data.Values()
	currentProjectID := t.nav.ProjectID()

	sortCol, sortAsc := t.containerView.Sort.Get()
	slices.SortStableFunc(containers, func(a, b dto.Container) int {
		return compareContainers(a, b, sortCol, sortAsc)
	})

	t.containerView.Table.Clear()
	t.RenderContainerHeader()

	index := 0
	for _, container := range containers {
		if string(container.Project.ID) != currentProjectID {
			continue
		}

		if !fuzzyMatch(container.Service, t.containerView.Search.Query()) {
			continue
		}

		index++

		serviceName := container.Service
		if container.CPUPercentage > resourceWarningThreshold || container.MemoryPercentage > resourceWarningThreshold {
			serviceName = "[yellow]⚠[-] " + serviceName
		}
		t.containerView.Table.SetCell(index, 0, tview.NewTableCell(serviceName))

		statusText := container.Status
		if statusText == "" {
			statusText = "unknown"
		}

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

		if container.PendingAction != "" {
			displayText = container.PendingAction + "…"
			statusColor = "fuchsia"
		}

		maxWidth := t.calculateStatusWidth(int(t.tableWidth.Load()))
		if len(displayText) > maxWidth {
			if maxWidth > 3 {
				displayText = displayText[:maxWidth-1] + "…"
			} else {
				displayText = "…"
			}
		}

		t.containerView.Table.SetCell(index, 1,
			tview.NewTableCell(fmt.Sprintf("[%s]%s[-]", statusColor, displayText)).SetAlign(tview.AlignLeft))

		t.containerView.Table.SetCell(index, 2,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", container.CPUPercentage)).SetAlign(tview.AlignRight))

		t.containerView.Table.SetCell(index, 3,
			tview.NewTableCell(fmt.Sprintf("%.2f%%", container.MemoryPercentage)).SetAlign(tview.AlignRight))
	}

	t.updateHeader()
}

func (t *Tui) drawContainerLog() {
	t.logView.View.Clear()

	paused := t.logView.Paused.Load()
	filter := t.logView.Filter.Get()
	showTimestamp := t.logView.ShowTimestamp.Load()
	logData := t.logView.Data.Get()

	var logs []string
	for _, line := range logData {
		if filter != "" && !strings.Contains(strings.ToLower(line), strings.ToLower(filter)) {
			continue
		}
		logs = append(logs, colorizeLogLine(line, showTimestamp))
	}

	t.logView.View.SetText(strings.Join(logs, ""))

	if !paused {
		t.logView.View.ScrollToEnd()
	}

	t.updateHeader()
}

func (t *Tui) showStatusMessage(message string) {
	t.status.Show(message, statusMessageDuration, t.app, t.closing.Load)
}

func (t *Tui) setProjectSort(col projectSortColumn) {
	t.projectView.Sort.Toggle(col, col == projectSortName)
	t.drawProjects()
}

func (t *Tui) setContainerSort(col containerSortColumn) {
	t.containerView.Sort.Toggle(col, col == containerSortName || col == containerSortStatus)
	t.drawContainers()
}

func (t *Tui) GetRequestData() <-chan dto.RequestData {
	return t.requestData
}

func (t *Tui) getData(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer itimer.StopTicker(ticker)

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			if t.closing.Load() {
				return
			}

			switch t.nav.View() {
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
	if t.projectRefresh.IsPaused() {
		return
	}

	response := make(chan []dto.Project, 1)
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case t.requestData <- &dto.RequestProjectList{Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending project list request")
		return
	}

	select {
	case projects := <-response:
		t.projectView.Data.UpdateFrom(projects, func(p dto.Project) dto.ProjectID { return p.ID })
		t.app.QueueUpdateDraw(func() {
			t.drawProjects()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for project list response")
	}
}

func (t *Tui) refreshContainerList() {
	if t.containerRefresh.IsPaused() {
		return
	}

	currentProjectID := t.nav.ProjectID()
	response := make(chan []dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

	select {
	case t.requestData <- &dto.RequestProject{ProjectID: dto.ProjectID(currentProjectID), Response: response}:
	case <-timer.C:
		t.logger.Warn("timeout sending container list request")
		return
	}

	select {
	case containers := <-response:
		t.containerView.Data.UpdateFrom(containers, func(c dto.Container) dto.ContainerID { return c.ID })
		t.app.QueueUpdateDraw(func() {
			t.drawContainers()
		})
	case <-timer.C:
		t.logger.Warn("timeout waiting for container list response")
	}
}

func (t *Tui) refreshContainerLog(ctx context.Context) {
	if t.logView.Paused.Load() {
		return
	}

	if t.logView.Disappeared.Load() {
		t.handleDisappearedContainer(ctx)
		return
	}

	currentContainerID := t.nav.ContainerID()
	t.logger.DebugContext(ctx, "fetching logs for container", slog.String("container_id", currentContainerID))

	t.updateLogs(ctx)
}

func (t *Tui) handleDisappearedContainer(ctx context.Context) {
	currentContainerID := t.nav.ContainerID()

	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

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
		t.logger.DebugContext(ctx, "container disappeared again")
		t.tryReconnectContainer(ctx)
		return
	}

	if len(c.Logs) == 0 {
		t.logger.DebugContext(ctx, "still waiting for logs...")
		return
	}

	t.logger.DebugContext(ctx, "got logs, closing modal", slog.Int("log_count", len(c.Logs)))
	t.logView.Disappeared.Store(false)
	t.logView.Data.Set(c.Logs)

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
		t.pages.HidePage("modal")
	})
}

func (t *Tui) tryReconnectContainer(ctx context.Context) {
	currentContainerService := t.nav.ContainerService()
	currentProjectID := t.nav.ProjectID()

	t.logger.DebugContext(ctx, "checking for container reappearance",
		slog.String("service", currentContainerService),
		slog.String("project", currentProjectID))

	responseProject := make(chan []dto.Container, 1)
	timer1 := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer1)

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

	t.logger.InfoContext(ctx, "container reappeared, starting log collection",
		slog.String("service", foundContainer.Service),
		slog.String("new_id", string(foundContainer.ID)))

	t.nav.SetContainerInfo(string(foundContainer.ID), foundContainer.Name, foundContainer.Service)

	response := make(chan dto.Container, 1)
	newContainerID := t.nav.ContainerID()
	timer2 := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer2)

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
	currentContainerID := t.nav.ContainerID()
	currentContainerName := t.nav.ContainerName()

	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

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
		t.logView.Disappeared.Store(true)

		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.disappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}
}

func (t *Tui) updateLogs(ctx context.Context) {
	currentContainerID := t.nav.ContainerID()
	currentContainerName := t.nav.ContainerName()

	response := make(chan dto.Container, 1)
	timer := time.NewTimer(channelTimeout)
	defer itimer.Stop(timer)

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
		t.logView.Disappeared.Store(true)

		displayID := currentContainerID
		if len(displayID) > 12 {
			displayID = displayID[:12]
		}

		t.app.QueueUpdateDraw(func() {
			t.disappearedModal.SetText(fmt.Sprintf("Container %s (%s) no longer exists.\nWaiting for it to reappear or press OK to return to container list.", currentContainerName, displayID))
			t.pages.ShowPage("modal")
		})
		return
	}

	if len(c.Logs) > 0 {
		t.logView.Data.Set(c.Logs)
	}

	t.app.QueueUpdateDraw(func() {
		t.drawContainerLog()
	})
}

func (t *Tui) Render(ctx context.Context) error {
	t.dataWG.Add(1)
	go func() {
		defer t.dataWG.Done()
		t.getData(ctx)
	}()

	if err := t.app.SetRoot(t.pages, true).EnableMouse(true).Run(); err != nil {
		t.logger.ErrorContext(ctx, "error rendering tui", slog.Any("error", err.Error()))
		t.dataWG.Wait()
		return err
	}

	t.cleanup()
	return nil
}

func (t *Tui) cleanup() {
	t.closing.Store(true)

	t.actions.Cancel()
	t.actions.Wait()

	t.dataWG.Wait()

	t.status.Stop()
	t.containerRefresh.Stop()
	t.projectRefresh.Stop()

	close(t.requestData)
}
