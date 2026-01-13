package tui

import (
	"strings"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/syrm/c8s/dto"
)

// setupStylesOnce ensures styles are only configured once.
var setupStylesOnce sync.Once

// setupStyles configures tview global styles (k9s-like appearance).
func setupStyles() {
	setupStylesOnce.Do(func() {
		tview.Borders.HorizontalFocus = tview.BoxDrawingsLightHorizontal
		tview.Borders.VerticalFocus = tview.BoxDrawingsLightVertical
		tview.Borders.TopLeftFocus = tview.BoxDrawingsLightDownAndRight
		tview.Borders.TopRightFocus = tview.BoxDrawingsLightDownAndLeft
		tview.Borders.BottomLeftFocus = tview.BoxDrawingsLightUpAndRight
		tview.Borders.BottomRightFocus = tview.BoxDrawingsLightUpAndLeft

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

// createStatusBarView creates the status bar text view.
func createStatusBarView() *tview.TextView {
	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetTextAlign(tview.AlignCenter)
	statusBar.SetBackgroundColor(tcell.ColorBlack)
	statusBar.SetTextColor(tcell.ColorWhite)
	return statusBar
}

// escapeColorTags escapes brackets to prevent tview from interpreting them as color tags.
func escapeColorTags(s string) string {
	return strings.ReplaceAll(s, "[", "[[]")
}

// setupProjectTableHandler sets up keyboard handlers for the project table.
func (t *Tui) setupProjectTableHandler() {
	t.projectView.Table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

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
			t.projectView.Search.Input.SetText(t.projectView.Search.Query())
			t.projectView.Layout.AddItem(t.projectView.Search.Input, 1, 0, true)
			t.app.SetFocus(t.projectView.Search.Input)
			return nil
		}

		if r == 'c' && t.projectView.Search.Query() != "" {
			t.projectView.Search.SetQuery("")
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
			t.projectRefresh.Pause(refreshPauseDuration, t.closing.Load)
			t.updateHeader()
		}

		return event
	})
}

// enterContainerView handles navigation from project list to container list.
func (t *Tui) enterContainerView() {
	rowIndex, _ := t.projectView.Table.GetSelection()
	if rowIndex <= 0 {
		return
	}
	cell := t.projectView.Table.GetCell(rowIndex, 0)
	if cell == nil || cell.Text == "" {
		return
	}
	cellText := stripWarningPrefix(cell.Text)

	projects := t.projectView.Data.Values()
	var found bool
	for _, project := range projects {
		if cellText == project.Name {
			t.nav.SetProjectID(string(project.ID))
			t.nav.SetProjectName(project.Name)
			t.nav.SetView(viewProject)
			found = true
			break
		}
	}

	if !found {
		return
	}

	t.containerView.Search.SetQuery("")
	t.containerView.Search.Input.SetText("")
	t.containerView.Table.Clear()
	t.drawContainers()
	t.pages.SwitchToPage("containerList")

	t.projectRefresh.SetPaused(false)
	t.projectRefresh.Stop()
	t.updateHeader()
}

// setupContainerTableHandler sets up keyboard handlers for the container table.
func (t *Tui) setupContainerTableHandler() {
	t.containerView.Table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

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
			t.containerView.Search.Input.SetText(t.containerView.Search.Query())
			t.containerView.Layout.AddItem(t.containerView.Search.Input, 1, 0, true)
			t.app.SetFocus(t.containerView.Search.Input)
			return nil
		}

		if r == 'c' && t.containerView.Search.Query() != "" {
			t.containerView.Search.SetQuery("")
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
			t.containerRefresh.Pause(refreshPauseDuration, t.closing.Load)
			t.updateHeader()
		}

		return event
	})
}

// exitContainerView handles navigation from container list back to project list.
func (t *Tui) exitContainerView() {
	t.pages.SwitchToPage("projectList")
	t.nav.SetView(viewProjectList)
	t.nav.SetContainerID("")
	t.containerView.Search.SetQuery("")
	t.containerView.Search.Input.SetText("")
	t.containerRefresh.SetPaused(false)
	t.containerRefresh.Stop()
	t.updateHeader()
}

// enterLogView handles navigation from container list to log view.
func (t *Tui) enterLogView() {
	rowIndex, _ := t.containerView.Table.GetSelection()
	if rowIndex <= 0 {
		return
	}
	cell := t.containerView.Table.GetCell(rowIndex, 0)
	if cell == nil || cell.Text == "" {
		return
	}
	cellText := stripWarningPrefix(cell.Text)

	containers := t.containerView.Data.Values()
	var found bool
	for _, container := range containers {
		if cellText == container.Service {
			t.nav.SetContainerInfo(string(container.ID), container.Name, container.Service)
			found = true
			break
		}
	}

	if !found {
		return
	}

	t.drawContainerLog()
	t.pages.SwitchToPage("logs")
	t.nav.SetView(viewContainerLog)
	t.updateHeader()
}

// setupLogViewHandler sets up keyboard handlers for the log view.
func (t *Tui) setupLogViewHandler() {
	t.logView.View.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			t.exitLogView()
		}

		if event.Rune() == 'p' {
			t.logView.TogglePaused()
			t.drawContainerLog()
			t.updateHeader()
		}

		if event.Rune() == '/' {
			t.logView.Layout.AddItem(t.logView.FilterInput, 1, 0, true)
			t.app.SetFocus(t.logView.FilterInput)
		}

		if event.Rune() == 'c' {
			if t.logView.Filter.Get() != "" {
				t.logView.Filter.Set("")
				t.logView.FilterInput.SetText("")
				t.logView.Layout.RemoveItem(t.logView.FilterInput)
				t.drawContainerLog()
				t.updateHeader()
			}
		}

		if event.Rune() == 't' {
			t.logView.ToggleTimestamp()
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
	currentContainerID := t.nav.ContainerID()
	if currentContainerID != "" {
		t.sendRequest(&dto.RequestStopLogCollection{ContainerID: dto.ContainerID(currentContainerID)})
	}

	t.containerView.Table.Clear()
	t.drawContainers()
	t.pages.SwitchToPage("containerList")
	t.nav.SetView(viewProject)
	t.nav.SetContainerID("")
	t.logView.Paused.Store(false)
	t.logView.Filter.Set("")
	t.logView.FilterInput.SetText("")
	t.logView.Layout.RemoveItem(t.logView.FilterInput)
	t.logView.Data.Clear()
	t.logView.Disappeared.Store(false)
	t.updateHeader()
}

// setupSearchCallbacks sets up callbacks for search inputs.
func (t *Tui) setupSearchCallbacks() {
	t.projectView.Search.Input.SetChangedFunc(func(text string) {
		t.projectView.Search.SetQuery(text)
		t.drawProjects()
	})

	t.projectView.Search.Input.SetDoneFunc(func(key tcell.Key) {
		t.projectView.Layout.RemoveItem(t.projectView.Search.Input)
		t.app.SetFocus(t.projectView.Table)
		if key == tcell.KeyEsc {
			t.projectView.Search.SetQuery("")
			t.projectView.Search.Input.SetText("")
		}
		t.drawProjects()
	})

	t.containerView.Search.Input.SetChangedFunc(func(text string) {
		t.containerView.Search.SetQuery(text)
		t.drawContainers()
	})

	t.containerView.Search.Input.SetDoneFunc(func(key tcell.Key) {
		t.containerView.Layout.RemoveItem(t.containerView.Search.Input)
		t.app.SetFocus(t.containerView.Table)
		if key == tcell.KeyEsc {
			t.containerView.Search.SetQuery("")
			t.containerView.Search.Input.SetText("")
		}
		t.drawContainers()
	})

	t.logView.FilterInput.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			t.logView.Filter.Set(t.logView.FilterInput.GetText())
		} else {
			t.logView.Filter.Set("")
			t.logView.FilterInput.SetText("")
		}
		t.logView.Layout.RemoveItem(t.logView.FilterInput)
		t.app.SetFocus(t.logView.View)
		t.drawContainerLog()
		t.updateHeader()
	})
}

// setupModalCallbacks sets up callbacks for modals.
func (t *Tui) setupModalCallbacks() {
	t.disappearedModal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		t.logView.Disappeared.Store(false)

		t.pages.HidePage("modal")
		t.containerView.Table.Clear()
		t.drawContainers()
		t.pages.SwitchToPage("containerList")
		t.nav.SetView(viewProject)
		t.nav.ClearContainerInfo()
		t.logView.Paused.Store(false)
		t.logView.Filter.Set("")
		t.logView.FilterInput.SetText("")
		t.logView.Layout.RemoveItem(t.logView.FilterInput)
		t.logView.Data.Clear()
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
