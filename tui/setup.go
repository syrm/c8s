package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/syrm/c8s/dto"
)

// setupStyles configures tview global styles (k9s-like appearance).
func setupStyles() {
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
			t.projectSearchActive = true
			t.projectSearchInput.SetText(t.projectSearchQuery)
			t.projectLayout.AddItem(t.projectSearchInput, 1, 0, true)
			t.app.SetFocus(t.projectSearchInput)
			return nil
		}

		if r == 'c' && t.projectSearchQuery != "" {
			t.projectSearchQuery = ""
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
	rowIndex, _ := t.tableProject.GetSelection()
	t.tableProjectDataLock.RLock()
	for _, project := range t.tableProjectData {
		cellText := t.tableProject.GetCell(rowIndex, 0).Text
		if fuzzyMatch(cellText, project.Name) {
			t.currentProjectID = string(project.ID)
			t.currentProjectName = project.Name
			t.setCurrentView(viewProject)
			break
		}
	}
	t.tableProjectDataLock.RUnlock()

	t.containerSearchQuery = ""
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
			t.containerSearchActive = true
			t.containerSearchInput.SetText(t.containerSearchQuery)
			t.containerLayout.AddItem(t.containerSearchInput, 1, 0, true)
			t.app.SetFocus(t.containerSearchInput)
			return nil
		}

		if r == 'c' && t.containerSearchQuery != "" {
			t.containerSearchQuery = ""
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
	t.currentContainerID = ""
	t.containerSearchQuery = ""
	t.containerSearchInput.SetText("")
	t.setContainerRefreshPaused(false)
	t.stopContainerRefreshTimer()
	t.updateHeader()
}

// enterLogView handles navigation from container list to log view.
func (t *Tui) enterLogView() {
	rowIndex, _ := t.tableContainer.GetSelection()
	t.tableContainerDataLock.RLock()
	for _, container := range t.tableContainerData {
		cellText := t.tableContainer.GetCell(rowIndex, 0).Text
		if fuzzyMatch(cellText, container.Service) {
			t.currentContainerID = string(container.ID)
			t.currentContainerName = container.Name
			t.currentContainerService = container.Service
			break
		}
	}
	t.tableContainerDataLock.RUnlock()
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
	t.tableContainer.Clear()
	t.drawContainers()
	t.pages.SwitchToPage("containerList")
	t.setCurrentView(viewProject)
	t.currentContainerID = ""
	t.setLogPaused(false)
	t.setLogFilter("")
	t.logFilterInput.SetText("")
	t.logLayout.RemoveItem(t.logFilterInput)
	t.tableContainerLogData = nil
	t.setContainerDisappeared(false)
	t.updateHeader()
}

// setupSearchCallbacks sets up callbacks for search inputs.
func (t *Tui) setupSearchCallbacks() {
	t.projectSearchInput.SetChangedFunc(func(text string) {
		t.projectSearchQuery = text
		t.drawProjects()
	})

	t.projectSearchInput.SetDoneFunc(func(key tcell.Key) {
		t.projectSearchActive = false
		t.projectLayout.RemoveItem(t.projectSearchInput)
		t.app.SetFocus(t.tableProject)
		if key == tcell.KeyEsc {
			t.projectSearchQuery = ""
			t.projectSearchInput.SetText("")
		}
		t.drawProjects()
	})

	t.containerSearchInput.SetChangedFunc(func(text string) {
		t.containerSearchQuery = text
		t.drawContainers()
	})

	t.containerSearchInput.SetDoneFunc(func(key tcell.Key) {
		t.containerSearchActive = false
		t.containerLayout.RemoveItem(t.containerSearchInput)
		t.app.SetFocus(t.tableContainer)
		if key == tcell.KeyEsc {
			t.containerSearchQuery = ""
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
		t.currentContainerID = ""
		t.currentContainerName = ""
		t.currentContainerService = ""
		t.setLogPaused(false)
		t.setLogFilter("")
		t.logFilterInput.SetText("")
		t.logLayout.RemoveItem(t.logFilterInput)
		t.tableContainerLogData = nil
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

// findContainerByService finds a container in the current view by service name.
func (t *Tui) findContainerByService(rowIndex int) *dto.Container {
	t.tableContainerDataLock.RLock()
	defer t.tableContainerDataLock.RUnlock()

	cellText := t.tableContainer.GetCell(rowIndex, 0).Text
	for _, container := range t.tableContainerData {
		if fuzzyMatch(cellText, container.Service) {
			c := container
			return &c
		}
	}
	return nil
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
	t.logPausedLock.Lock()
	defer t.logPausedLock.Unlock()
	t.logPaused = paused
}

func (t *Tui) getLogPaused() bool {
	t.logPausedLock.RLock()
	defer t.logPausedLock.RUnlock()
	return t.logPaused
}

func (t *Tui) toggleLogPaused() {
	t.logPausedLock.Lock()
	defer t.logPausedLock.Unlock()
	t.logPaused = !t.logPaused
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
	t.logShowTimestampLock.Lock()
	defer t.logShowTimestampLock.Unlock()
	t.logShowTimestamp = !t.logShowTimestamp
}

func (t *Tui) getLogShowTimestamp() bool {
	t.logShowTimestampLock.RLock()
	defer t.logShowTimestampLock.RUnlock()
	return t.logShowTimestamp
}

func (t *Tui) setContainerDisappeared(disappeared bool) {
	t.containerDisappearedLock.Lock()
	defer t.containerDisappearedLock.Unlock()
	t.containerDisappeared = disappeared
}

func (t *Tui) getContainerDisappeared() bool {
	t.containerDisappearedLock.RLock()
	defer t.containerDisappearedLock.RUnlock()
	return t.containerDisappeared
}

func (t *Tui) setContainerRefreshPaused(paused bool) {
	t.containerRefreshPausedLock.Lock()
	defer t.containerRefreshPausedLock.Unlock()
	t.containerRefreshPaused = paused
}

func (t *Tui) getContainerRefreshPaused() bool {
	t.containerRefreshPausedLock.RLock()
	defer t.containerRefreshPausedLock.RUnlock()
	return t.containerRefreshPaused
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
	t.projectRefreshPausedLock.Lock()
	defer t.projectRefreshPausedLock.Unlock()
	t.projectRefreshPaused = paused
}

func (t *Tui) getProjectRefreshPaused() bool {
	t.projectRefreshPausedLock.RLock()
	defer t.projectRefreshPausedLock.RUnlock()
	return t.projectRefreshPaused
}

func (t *Tui) stopProjectRefreshTimer() {
	t.projectRefreshTimerLock.Lock()
	defer t.projectRefreshTimerLock.Unlock()
	if t.projectRefreshTimer != nil {
		t.projectRefreshTimer.Stop()
		t.projectRefreshTimer = nil
	}
}
