package tui

import (
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/syrm/c8s/dto"
)

// setupStylesOnce ensures styles are only configured once.
var setupStylesOnce sync.Once

// setupStyles configures tview global styles (k9s-like appearance).
func setupStyles() {
	setupStylesOnce.Do(func() {
		// K9s-style borders
		tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
		tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
		tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
		tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
		tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
		tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

		// K9s color scheme
		tview.Styles.PrimitiveBackgroundColor = tcell.ColorBlack
		tview.Styles.ContrastBackgroundColor = tcell.ColorBlack
		tview.Styles.MoreContrastBackgroundColor = tcell.ColorBlack
		tview.Styles.BorderColor = tcell.ColorDarkCyan
		tview.Styles.TitleColor = tcell.GetColor("cyan")
		tview.Styles.GraphicsColor = tcell.GetColor("cyan")
		tview.Styles.PrimaryTextColor = tcell.ColorWhite
		tview.Styles.SecondaryTextColor = tcell.ColorLightGray
		tview.Styles.TertiaryTextColor = tcell.ColorGray
		tview.Styles.InverseTextColor = tcell.ColorBlack
		tview.Styles.ContrastSecondaryTextColor = tcell.ColorDarkCyan
	})
}

// createSearchInput creates a styled search input field.
func createSearchInput() *tview.InputField {
	input := tview.NewInputField()
	input.SetLabel("[cyan]Filter: [-]")
	input.SetFieldWidth(0)
	input.SetFieldBackgroundColor(tcell.ColorBlack)
	input.SetFieldTextColor(tcell.ColorWhite)
	input.SetLabelColor(tcell.GetColor("cyan"))
	return input
}

// createTable creates a styled table with selection support.
func createTable() *tview.Table {
	table := tview.NewTable().SetSelectable(true, false)
	table.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	table.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.ColorNavy).
		Bold(true))
	return table
}

// setupProjectTableHandler sets up keyboard handlers for the project table.
func (t *Tui) setupProjectTableHandler() {
	t.tableProject.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

		// Sort by column
		switch r {
		case 'N':
			t.setProjectSort(projectSortName)
			return nil
		case 'C':
			t.setProjectSort(projectSortCPU)
			return nil
		case 'M':
			t.setProjectSort(projectSortMemory)
			return nil
		case 'O':
			t.setProjectSort(projectSortContainers)
			return nil
		}

		if r == '/' {
			t.projectSearchInput.SetText(t.getProjectSearchQuery())
			t.projectLayout.AddItem(t.projectSearchInput, 1, 0, true)
			t.app.SetFocus(t.projectSearchInput)
			return nil
		}

		if r == 'c' && t.getProjectSearchQuery() != "" {
			t.setProjectSearchQuery("")
			t.drawProjects()
			return nil
		}

		if r == 'h' {
			t.pages.ShowPage("help")
			t.app.SetFocus(t.helpTextView)
			return nil
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			t.enterContainerView()
		}

		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			t.pauseProjectRefresh()
			t.updateHeader()
		}

		return event
	})
}

// enterContainerView handles navigation from project list to container list.
func (t *Tui) enterContainerView() {
	rowIndex, rowCount := t.tableProject.GetSelection()
	// Validate row index: must be > 0 (skip header) and < rowCount
	if rowIndex <= 0 || rowIndex >= rowCount {
		return
	}
	cell := t.tableProject.GetCell(rowIndex, 0)
	if cell == nil || cell.Text == "" {
		return
	}
	cellText := stripWarningPrefix(cell.Text)

	var found bool
	t.tableProjectDataLock.RLock()
	for _, project := range t.tableProjectData {
		if cellText == project.Name {
			t.setCurrentProjectID(string(project.ID))
			t.setCurrentProjectName(project.Name)
			t.setCurrentView(viewProject)
			found = true
			break
		}
	}
	t.tableProjectDataLock.RUnlock()

	if !found {
		return
	}

	t.setContainerSearchQuery("")
	t.containerSearchInput.SetText("")
	t.tableContainer.Clear()
	t.drawContainers()
	t.pages.SwitchToPage("containerList")

	t.setProjectRefreshPaused(false)
	t.stopProjectRefreshTimer()
	t.updateHeader()
}

// setupContainerTableHandler sets up keyboard handlers for the container table.
func (t *Tui) setupContainerTableHandler() {
	t.tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

		// Sort by column
		switch r {
		case 'N':
			t.setContainerSort(containerSortName)
			return nil
		case 'C':
			t.setContainerSort(containerSortCPU)
			return nil
		case 'M':
			t.setContainerSort(containerSortMemory)
			return nil
		case 'S':
			t.setContainerSort(containerSortStatus)
			return nil
		}

		if r == '/' {
			t.containerSearchInput.SetText(t.getContainerSearchQuery())
			t.containerLayout.AddItem(t.containerSearchInput, 1, 0, true)
			t.app.SetFocus(t.containerSearchInput)
			return nil
		}

		if r == 'c' && t.getContainerSearchQuery() != "" {
			t.setContainerSearchQuery("")
			t.drawContainers()
			return nil
		}

		if r == 'h' {
			t.pages.ShowPage("help")
			t.app.SetFocus(t.helpTextView)
			return nil
		}

		if r == 's' {
			t.handleContainerShell()
			return nil
		}

		if r == 'x' {
			t.handleContainerStop()
			return nil
		}

		if r == 'r' {
			t.handleContainerRestart()
			return nil
		}

		if r == 'd' {
			t.handleContainerRemove()
			return nil
		}

		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			t.exitContainerView()
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			t.enterLogView()
		}

		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			t.pauseContainerRefresh()
			t.updateHeader()
		}

		return event
	})
}

// exitContainerView handles navigation from container list back to project list.
func (t *Tui) exitContainerView() {
	t.pages.SwitchToPage("projectList")
	t.setCurrentView(viewProjectList)
	t.setCurrentContainerID("")
	t.setContainerSearchQuery("")
	t.containerSearchInput.SetText("")
	t.setContainerRefreshPaused(false)
	t.stopContainerRefreshTimer()
	t.updateHeader()
}

// enterLogView handles navigation from container list to log view.
func (t *Tui) enterLogView() {
	rowIndex, rowCount := t.tableContainer.GetSelection()
	// Validate row index: must be > 0 (skip header) and < rowCount
	if rowIndex <= 0 || rowIndex >= rowCount {
		return
	}
	cell := t.tableContainer.GetCell(rowIndex, 0)
	if cell == nil || cell.Text == "" {
		return
	}
	cellText := stripWarningPrefix(cell.Text)

	var found bool
	t.tableContainerDataLock.RLock()
	for _, container := range t.tableContainerData {
		if cellText == container.Service {
			t.setCurrentContainerInfo(string(container.ID), container.Name, container.Service)
			found = true
			break
		}
	}
	t.tableContainerDataLock.RUnlock()

	if !found {
		return
	}

	t.drawContainerLog()
	t.pages.SwitchToPage("logs")
	t.setCurrentView(viewContainerLog)
	t.updateHeader()
}

// setupLogViewHandler sets up keyboard handlers for the log view.
func (t *Tui) setupLogViewHandler() {
	t.tableContainerLog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			t.exitLogView()
		}

		if event.Rune() == 'p' {
			t.toggleLogPaused()
			t.drawContainerLog()
			t.updateHeader()
		}

		if event.Rune() == '/' {
			t.logLayout.AddItem(t.logFilterInput, 1, 0, true)
			t.app.SetFocus(t.logFilterInput)
		}

		if event.Rune() == 'c' {
			if t.getLogFilter() != "" {
				t.setLogFilter("")
				t.logFilterInput.SetText("")
				t.logLayout.RemoveItem(t.logFilterInput)
				t.drawContainerLog()
				t.updateHeader()
			}
		}

		if event.Rune() == 't' {
			t.toggleLogShowTimestamp()
			t.drawContainerLog()
			t.updateHeader()
		}

		if event.Rune() == 'h' {
			t.pages.ShowPage("help")
			t.app.SetFocus(t.helpTextView)
			return nil
		}

		return event
	})
}

// exitLogView handles navigation from log view back to container list.
func (t *Tui) exitLogView() {
	// Stop log collection for the current container
	currentContainerID := t.getCurrentContainerID()
	if currentContainerID != "" {
		timer := time.NewTimer(channelTimeout)
		select {
		case t.requestData <- &dto.RequestStopLogCollection{ContainerID: dto.ContainerID(currentContainerID)}:
			timer.Stop()
		case <-timer.C:
			// Timeout is acceptable here, we're exiting anyway
		}
	}

	t.tableContainer.Clear()
	t.drawContainers()
	t.pages.SwitchToPage("containerList")
	t.setCurrentView(viewProject)
	t.setCurrentContainerID("")
	t.setLogPaused(false)
	t.setLogFilter("")
	t.logFilterInput.SetText("")
	t.logLayout.RemoveItem(t.logFilterInput)
	t.clearTableContainerLogData()
	t.setContainerDisappeared(false)
	t.updateHeader()
}

// setupSearchCallbacks sets up callbacks for search inputs.
func (t *Tui) setupSearchCallbacks() {
	t.projectSearchInput.SetChangedFunc(func(text string) {
		t.setProjectSearchQuery(text)
		t.drawProjects()
	})

	t.projectSearchInput.SetDoneFunc(func(key tcell.Key) {
		t.projectLayout.RemoveItem(t.projectSearchInput)
		t.app.SetFocus(t.tableProject)
		if key == tcell.KeyEsc {
			t.setProjectSearchQuery("")
			t.projectSearchInput.SetText("")
		}
		t.drawProjects()
	})

	t.containerSearchInput.SetChangedFunc(func(text string) {
		t.setContainerSearchQuery(text)
		t.drawContainers()
	})

	t.containerSearchInput.SetDoneFunc(func(key tcell.Key) {
		t.containerLayout.RemoveItem(t.containerSearchInput)
		t.app.SetFocus(t.tableContainer)
		if key == tcell.KeyEsc {
			t.setContainerSearchQuery("")
			t.containerSearchInput.SetText("")
		}
		t.drawContainers()
	})

	t.logFilterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			t.setLogFilter(t.logFilterInput.GetText())
		} else {
			t.setLogFilter("")
			t.logFilterInput.SetText("")
		}
		t.logLayout.RemoveItem(t.logFilterInput)
		t.app.SetFocus(t.tableContainerLog)
		t.drawContainerLog()
		t.updateHeader()
	})
}

// setupModalCallbacks sets up callbacks for modals.
func (t *Tui) setupModalCallbacks() {
	t.containerDisappearedModal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		t.setContainerDisappeared(false)

		t.pages.HidePage("modal")
		t.tableContainer.Clear()
		t.drawContainers()
		t.pages.SwitchToPage("containerList")
		t.setCurrentView(viewProject)
		t.clearCurrentContainerInfo()
		t.setLogPaused(false)
		t.setLogFilter("")
		t.logFilterInput.SetText("")
		t.logLayout.RemoveItem(t.logFilterInput)
		t.clearTableContainerLogData()
		t.updateHeader()
	})

	t.helpTextView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			t.pages.HidePage("help")
			return nil
		}
		return event
	})
}

// Thread-safe helper methods for state management

func (t *Tui) setCurrentView(view currentView) {
	t.currentViewLock.Lock()
	defer t.currentViewLock.Unlock()
	t.currentView = view
}

func (t *Tui) getCurrentView() currentView {
	t.currentViewLock.RLock()
	defer t.currentViewLock.RUnlock()
	return t.currentView
}

func (t *Tui) setLogPaused(paused bool) {
	t.logPaused.Store(paused)
}

func (t *Tui) getLogPaused() bool {
	return t.logPaused.Load()
}

func (t *Tui) toggleLogPaused() {
	t.logPaused.Store(!t.logPaused.Load())
}

func (t *Tui) setLogFilter(filter string) {
	t.logFilterLock.Lock()
	defer t.logFilterLock.Unlock()
	t.logFilter = filter
}

func (t *Tui) getLogFilter() string {
	t.logFilterLock.RLock()
	defer t.logFilterLock.RUnlock()
	return t.logFilter
}

func (t *Tui) toggleLogShowTimestamp() {
	t.logShowTimestamp.Store(!t.logShowTimestamp.Load())
}

func (t *Tui) getLogShowTimestamp() bool {
	return t.logShowTimestamp.Load()
}

func (t *Tui) setContainerDisappeared(disappeared bool) {
	t.containerDisappeared.Store(disappeared)
}

func (t *Tui) getContainerDisappeared() bool {
	return t.containerDisappeared.Load()
}

func (t *Tui) setContainerRefreshPaused(paused bool) {
	t.containerRefreshPaused.Store(paused)
}

func (t *Tui) getContainerRefreshPaused() bool {
	return t.containerRefreshPaused.Load()
}

func (t *Tui) stopContainerRefreshTimer() {
	t.containerRefreshTimerLock.Lock()
	defer t.containerRefreshTimerLock.Unlock()
	if t.containerRefreshTimer != nil {
		t.containerRefreshTimer.Stop()
		t.containerRefreshTimer = nil
	}
}

func (t *Tui) setProjectRefreshPaused(paused bool) {
	t.projectRefreshPaused.Store(paused)
}

func (t *Tui) getProjectRefreshPaused() bool {
	return t.projectRefreshPaused.Load()
}

func (t *Tui) stopProjectRefreshTimer() {
	t.projectRefreshTimerLock.Lock()
	defer t.projectRefreshTimerLock.Unlock()
	if t.projectRefreshTimer != nil {
		t.projectRefreshTimer.Stop()
		t.projectRefreshTimer = nil
	}
}

// Thread-safe helpers for current container/project state

func (t *Tui) getCurrentProjectID() string {
	t.currentContainerLock.RLock()
	defer t.currentContainerLock.RUnlock()
	return t.currentProjectID
}

func (t *Tui) setCurrentProjectID(id string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentProjectID = id
}

func (t *Tui) getCurrentContainerID() string {
	t.currentContainerLock.RLock()
	defer t.currentContainerLock.RUnlock()
	return t.currentContainerID
}

func (t *Tui) setCurrentContainerID(id string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentContainerID = id
}

func (t *Tui) getCurrentContainerName() string {
	t.currentContainerLock.RLock()
	defer t.currentContainerLock.RUnlock()
	return t.currentContainerName
}

func (t *Tui) setCurrentContainerName(name string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentContainerName = name
}

func (t *Tui) getCurrentContainerService() string {
	t.currentContainerLock.RLock()
	defer t.currentContainerLock.RUnlock()
	return t.currentContainerService
}

func (t *Tui) setCurrentContainerService(service string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentContainerService = service
}

func (t *Tui) setCurrentContainerInfo(id, name, service string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentContainerID = id
	t.currentContainerName = name
	t.currentContainerService = service
}

func (t *Tui) clearCurrentContainerInfo() {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentContainerID = ""
	t.currentContainerName = ""
	t.currentContainerService = ""
}

// Thread-safe helpers for tableContainerLogData

func (t *Tui) getTableContainerLogData() []string {
	t.tableContainerLogDataLock.RLock()
	defer t.tableContainerLogDataLock.RUnlock()
	// Return a copy to avoid race conditions
	result := make([]string, len(t.tableContainerLogData))
	copy(result, t.tableContainerLogData)
	return result
}

func (t *Tui) setTableContainerLogData(data []string) {
	t.tableContainerLogDataLock.Lock()
	defer t.tableContainerLogDataLock.Unlock()
	// Copy data to avoid race conditions if caller modifies original slice
	t.tableContainerLogData = make([]string, len(data))
	copy(t.tableContainerLogData, data)
}

func (t *Tui) clearTableContainerLogData() {
	t.tableContainerLogDataLock.Lock()
	defer t.tableContainerLogDataLock.Unlock()
	t.tableContainerLogData = nil
}

// Thread-safe helpers for search queries

func (t *Tui) getProjectSearchQuery() string {
	t.projectSearchQueryLock.RLock()
	defer t.projectSearchQueryLock.RUnlock()
	return t.projectSearchQuery
}

func (t *Tui) setProjectSearchQuery(query string) {
	t.projectSearchQueryLock.Lock()
	defer t.projectSearchQueryLock.Unlock()
	t.projectSearchQuery = query
}

func (t *Tui) getContainerSearchQuery() string {
	t.containerSearchQueryLock.RLock()
	defer t.containerSearchQueryLock.RUnlock()
	return t.containerSearchQuery
}

func (t *Tui) setContainerSearchQuery(query string) {
	t.containerSearchQueryLock.Lock()
	defer t.containerSearchQueryLock.Unlock()
	t.containerSearchQuery = query
}

// Thread-safe helpers for sort state

func (t *Tui) getProjectSort() (projectSortColumn, bool) {
	t.projectSortLock.RLock()
	defer t.projectSortLock.RUnlock()
	return t.projectSortColumn, t.projectSortAsc
}

func (t *Tui) setProjectSortState(col projectSortColumn, asc bool) {
	t.projectSortLock.Lock()
	defer t.projectSortLock.Unlock()
	t.projectSortColumn = col
	t.projectSortAsc = asc
}

func (t *Tui) getContainerSort() (containerSortColumn, bool) {
	t.containerSortLock.RLock()
	defer t.containerSortLock.RUnlock()
	return t.containerSortColumn, t.containerSortAsc
}

func (t *Tui) setContainerSortState(col containerSortColumn, asc bool) {
	t.containerSortLock.Lock()
	defer t.containerSortLock.Unlock()
	t.containerSortColumn = col
	t.containerSortAsc = asc
}

// Thread-safe helper for currentProjectName (uses currentContainerLock)

func (t *Tui) getCurrentProjectName() string {
	t.currentContainerLock.RLock()
	defer t.currentContainerLock.RUnlock()
	return t.currentProjectName
}

func (t *Tui) setCurrentProjectName(name string) {
	t.currentContainerLock.Lock()
	defer t.currentContainerLock.Unlock()
	t.currentProjectName = name
}
