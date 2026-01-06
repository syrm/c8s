package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

const (
	refreshInterval      = 2 * time.Second
	refreshPauseDuration = 5 * time.Second
	statusMessageDuration = 5 * time.Second
)

type projectSortColumn int

const (
	projectSortName projectSortColumn = iota
	projectSortCPU
	projectSortMemory
	projectSortContainers
)

type containerSortColumn int

const (
	containerSortName containerSortColumn = iota
	containerSortCPU
	containerSortMemory
	containerSortStatus
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

	app := tview.NewApplication()

	tableProject := tview.NewTable().SetSelectable(true, false)
	tableProject.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	tableProject.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.ColorNavy).
		Foreground(tcell.ColorBlack).
		Bold(true))

	projectSearchInput := tview.NewInputField()
	projectSearchInput.SetLabel("[cyan]Filter: [-]")
	projectSearchInput.SetFieldWidth(0)
	projectSearchInput.SetFieldBackgroundColor(tcell.ColorBlack)
	projectSearchInput.SetFieldTextColor(tcell.ColorWhite)
	projectSearchInput.SetLabelColor(tcell.GetColor("cyan"))

	tableContainer := tview.NewTable().SetSelectable(true, false)
	tableContainer.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	tableContainer.SetSelectedStyle(tcell.StyleDefault.
		Background(tcell.ColorNavy).
		Bold(true))

	containerSearchInput := tview.NewInputField()
	containerSearchInput.SetLabel("[cyan]Filter: [-]")
	containerSearchInput.SetFieldWidth(0)
	containerSearchInput.SetFieldBackgroundColor(tcell.ColorBlack)
	containerSearchInput.SetFieldTextColor(tcell.ColorWhite)
	containerSearchInput.SetLabelColor(tcell.GetColor("cyan"))

	statusBar := tview.NewTextView()
	statusBar.SetDynamicColors(true)
	statusBar.SetTextAlign(tview.AlignCenter)
	statusBar.SetBackgroundColor(tcell.ColorBlack)
	statusBar.SetTextColor(tcell.ColorWhite)

	tableContainerLog := tview.NewTextView()
	tableContainerLog.SetScrollable(true)
	tableContainerLog.SetWordWrap(true)
	tableContainerLog.SetBorder(true).SetBorderColor(tcell.ColorNavy)
	tableContainerLog.SetDynamicColors(true)
	tableContainerLog.SetBackgroundColor(tcell.ColorBlack)
	tableContainerLog.SetTextColor(tcell.ColorWhite)

	logFilterInput := tview.NewInputField()
	logFilterInput.SetLabel("[cyan]Filter: [-]")
	logFilterInput.SetFieldWidth(0)
	logFilterInput.SetFieldBackgroundColor(tcell.ColorBlack)
	logFilterInput.SetFieldTextColor(tcell.ColorWhite)
	logFilterInput.SetLabelColor(tcell.GetColor("cyan"))

	containerDisappearedModal := tview.NewModal().
		SetText("").
		AddButtons([]string{"OK"}).
		SetBackgroundColor(tcell.ColorBlack).
		SetTextColor(tcell.ColorWhite).
		SetButtonBackgroundColor(tcell.ColorDarkCyan).
		SetButtonTextColor(tcell.ColorWhite).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			// Will be set in tui struct
		})

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

	// Create a grid to center the help modal
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

	// Create layouts with header (k9s style)
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

	// Add pages to the pages widget
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

	tableProject.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

		// Sort by column (Shift+N=Name, Shift+C=CPU, Shift+M=Memory, Shift+O=Containers)
		switch r {
		case 'N':
			tui.setProjectSort(projectSortName)
			return nil
		case 'C':
			tui.setProjectSort(projectSortCPU)
			return nil
		case 'M':
			tui.setProjectSort(projectSortMemory)
			return nil
		case 'O':
			tui.setProjectSort(projectSortContainers)
			return nil
		}

		if r == '/' {
			tui.projectSearchActive = true
			tui.projectSearchInput.SetText(tui.projectSearchQuery) // Restore current filter
			tui.projectLayout.AddItem(tui.projectSearchInput, 1, 0, true)
			tui.app.SetFocus(tui.projectSearchInput)
			return nil
		}

		if r == 'c' && tui.projectSearchQuery != "" {
			tui.projectSearchQuery = ""
			tui.drawProjects()
			return nil
		}

		if r == 'h' {
			tui.pages.ShowPage("help")
			tui.app.SetFocus(tui.helpTextView)
			return nil
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableProject.GetSelection()
			tui.tableProjectDataLock.RLock()
			for _, project := range tui.tableProjectData {
				// Column 0 may contain warning symbol, so we check if the project name is in the cell text
				cellText := tableProject.GetCell(rowIndex, 0).Text
				if strings.Contains(cellText, project.Name) {
					tui.currentProjectID = string(project.ID)
					tui.currentProjectName = project.Name
					tui.currentViewLock.Lock()
					tui.currentView = viewProject
					tui.currentViewLock.Unlock()
					break
				}
			}
			tui.tableProjectDataLock.RUnlock()
			// Reset container filter when entering container list
			tui.containerSearchQuery = ""
			tui.containerSearchInput.SetText("")
			tui.tableContainer.Clear()
			tui.drawContainers()
			tui.pages.SwitchToPage("containerList")
			// Reset project refresh pause when leaving view
			tui.projectRefreshPausedLock.Lock()
			tui.projectRefreshPaused = false
			tui.projectRefreshPausedLock.Unlock()
			tui.projectRefreshTimerLock.Lock()
			if tui.projectRefreshTimer != nil {
				tui.projectRefreshTimer.Stop()
				tui.projectRefreshTimer = nil
			}
			tui.projectRefreshTimerLock.Unlock()
			tui.updateHeader()
		}

		// Pause refresh on navigation (Up/Down arrows)
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			tui.pauseProjectRefresh()
			tui.updateHeader()
		}

		return event
	})

	tableContainer.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		r := event.Rune()

		// Sort by column (Shift+N=Name, Shift+C=CPU, Shift+M=Memory, Shift+S=Status)
		switch r {
		case 'N':
			tui.setContainerSort(containerSortName)
			return nil
		case 'C':
			tui.setContainerSort(containerSortCPU)
			return nil
		case 'M':
			tui.setContainerSort(containerSortMemory)
			return nil
		case 'S':
			tui.setContainerSort(containerSortStatus)
			return nil
		}

		if r == '/' {
			tui.containerSearchActive = true
			tui.containerSearchInput.SetText(tui.containerSearchQuery) // Restore current filter
			tui.containerLayout.AddItem(tui.containerSearchInput, 1, 0, true)
			tui.app.SetFocus(tui.containerSearchInput)
			return nil
		}

		if r == 'c' && tui.containerSearchQuery != "" {
			tui.containerSearchQuery = ""
			tui.drawContainers()
			return nil
		}

		if r == 'h' {
			tui.pages.ShowPage("help")
			tui.app.SetFocus(tui.helpTextView)
			return nil
		}

		if r == 's' {
			// Open shell on selected container
			rowIndex, _ := tui.tableContainer.GetSelection()
			if rowIndex > 0 {
				tui.tableContainerDataLock.RLock()
				var selectedContainer *dto.Container
				for _, container := range tui.tableContainerData {
					cellText := tui.tableContainer.GetCell(rowIndex, 0).Text
					if strings.Contains(cellText, container.Service) && container.Status == "running" {
						selectedContainer = &container
						break
					}
				}
				tui.tableContainerDataLock.RUnlock()

				if selectedContainer != nil {
					tui.app.Suspend(func() {
						cmd := exec.Command("docker", "exec", "-it", string(selectedContainer.ID), "/bin/sh")
						cmd.Stdin = os.Stdin
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						_ = cmd.Run()
					})
				}
			}
			return nil
		}

		if r == 'x' {
			rowIndex, _ := tui.tableContainer.GetSelection()
			if rowIndex > 0 {
				tui.tableContainerDataLock.RLock()
				var selectedContainer *dto.Container
				for _, container := range tui.tableContainerData {
					cellText := tui.tableContainer.GetCell(rowIndex, 0).Text
					if strings.Contains(cellText, container.Service) && container.Status == "running" {
						c := container
						selectedContainer = &c
						break
					}
				}
				tui.tableContainerDataLock.RUnlock()

				if selectedContainer != nil {
					// Set pending action
					response := make(chan bool)
					tui.requestData <- &RequestSetPendingAction{
						ContainerID:   selectedContainer.ID,
						PendingAction: "stopping",
						Response:      response,
					}
					<-response

					// Update local cache immediately
					tui.tableContainerDataLock.Lock()
					if c, ok := tui.tableContainerData[selectedContainer.ID]; ok {
						c.PendingAction = "stopping"
						tui.tableContainerData[selectedContainer.ID] = c
					}
					tui.tableContainerDataLock.Unlock()
					tui.drawContainers()

					go func() {
						cmd := exec.Command("docker", "stop", string(selectedContainer.ID))
						_ = cmd.Run()
					}()
				}
			}
			return nil
		}

		if r == 'r' {
			rowIndex, _ := tui.tableContainer.GetSelection()
			if rowIndex > 0 {
				tui.tableContainerDataLock.RLock()
				var selectedContainer *dto.Container
				for _, container := range tui.tableContainerData {
					cellText := tui.tableContainer.GetCell(rowIndex, 0).Text
					if strings.Contains(cellText, container.Service) {
						c := container
						selectedContainer = &c
						break
					}
				}
				tui.tableContainerDataLock.RUnlock()

				if selectedContainer != nil {
					// Set pending action
					response := make(chan bool)
					var action string
					if selectedContainer.Status == "running" {
						action = "restarting"
					} else {
						action = "starting"
					}
					tui.requestData <- &RequestSetPendingAction{
						ContainerID:   selectedContainer.ID,
						PendingAction: action,
						Response:      response,
					}
					<-response

					// Update local cache immediately
					tui.tableContainerDataLock.Lock()
					if c, ok := tui.tableContainerData[selectedContainer.ID]; ok {
						c.PendingAction = action
						tui.tableContainerData[selectedContainer.ID] = c
					}
					tui.tableContainerDataLock.Unlock()
					tui.drawContainers()

					go func() {
						if selectedContainer.Status == "running" {
							cmd := exec.Command("docker", "restart", string(selectedContainer.ID))
							_ = cmd.Run()
						} else {
							cmd := exec.Command("docker", "start", string(selectedContainer.ID))
							_ = cmd.Run()
						}
					}()
				}
			}
			return nil
		}

		if r == 'd' {
			rowIndex, _ := tui.tableContainer.GetSelection()
			if rowIndex > 0 {
				tui.tableContainerDataLock.RLock()
				var selectedContainer *dto.Container
				for _, container := range tui.tableContainerData {
					cellText := tui.tableContainer.GetCell(rowIndex, 0).Text
					if strings.Contains(cellText, container.Service) && container.Status != "running" {
						c := container
						selectedContainer = &c
						break
					}
				}
				tui.tableContainerDataLock.RUnlock()

				if selectedContainer != nil {
					// Set pending action
					response := make(chan bool)
					tui.requestData <- &RequestSetPendingAction{
						ContainerID:   selectedContainer.ID,
						PendingAction: "removing",
						Response:      response,
					}
					<-response

					// Update local cache immediately
					tui.tableContainerDataLock.Lock()
					if c, ok := tui.tableContainerData[selectedContainer.ID]; ok {
						c.PendingAction = "removing"
						tui.tableContainerData[selectedContainer.ID] = c
					}
					tui.tableContainerDataLock.Unlock()
					tui.drawContainers()

					go func() {
						cmd := exec.Command("docker", "rm", string(selectedContainer.ID))
						_ = cmd.Run()
					}()
				}
			}
			return nil
		}

		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.pages.SwitchToPage("projectList")
			tui.currentViewLock.Lock()
			tui.currentView = viewProjectList
			tui.currentViewLock.Unlock()
			tui.currentContainerID = ""
			tui.containerSearchQuery = ""
			tui.containerSearchInput.SetText("")
			tui.containerRefreshPausedLock.Lock()
			tui.containerRefreshPaused = false
			tui.containerRefreshPausedLock.Unlock()
			tui.containerRefreshTimerLock.Lock()
			if tui.containerRefreshTimer != nil {
				tui.containerRefreshTimer.Stop()
				tui.containerRefreshTimer = nil
			}
			tui.containerRefreshTimerLock.Unlock()
			tui.updateHeader()
		}

		if event.Key() == tcell.KeyEnter || event.Key() == tcell.KeyRight {
			rowIndex, _ := tableContainer.GetSelection()
			tui.tableContainerDataLock.RLock()
			for _, container := range tui.tableContainerData {
				// Column 0 contains symbols + service name, so we check if the service name is in the cell text
				cellText := tableContainer.GetCell(rowIndex, 0).Text
				if strings.Contains(cellText, container.Service) {
					tui.currentContainerID = string(container.ID)
					tui.currentContainerName = container.Name
					tui.currentContainerService = container.Service
					break
				}
			}
			tui.tableContainerDataLock.RUnlock()
			tui.drawContainerLog()
			tui.pages.SwitchToPage("logs")
			tui.currentViewLock.Lock()
			tui.currentView = viewContainerLog
			tui.currentViewLock.Unlock()
			tui.updateHeader()
		}

		// Pause refresh on navigation (Up/Down arrows)
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown {
			tui.pauseContainerRefresh()
			tui.updateHeader()
		}

		return event
	})

	tableContainerLog.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Key() == tcell.KeyLeft {
			tui.tableContainer.Clear()
			tui.drawContainers()
			tui.pages.SwitchToPage("containerList")
			tui.currentViewLock.Lock()
			tui.currentView = viewProject
			tui.currentViewLock.Unlock()
			tui.currentContainerID = ""
			tui.logPausedLock.Lock()
			tui.logPaused = false
			tui.logPausedLock.Unlock()
			tui.logFilterLock.Lock()
			tui.logFilter = ""
			tui.logFilterLock.Unlock()
			tui.logFilterInput.SetText("")
			tui.logLayout.RemoveItem(tui.logFilterInput)
			tui.tableContainerLogData = nil // Clear logs to free memory
			tui.containerDisappearedLock.Lock()
			tui.containerDisappeared = false
			tui.containerDisappearedLock.Unlock()
			// Update header immediately after view change
			tui.updateHeader()
		}

		if event.Rune() == 'p' {
			tui.logPausedLock.Lock()
			tui.logPaused = !tui.logPaused
			tui.logPausedLock.Unlock()
			tui.drawContainerLog()
			tui.updateHeader()
		}

		if event.Rune() == '/' {
			tui.logLayout.AddItem(tui.logFilterInput, 1, 0, true)
			tui.app.SetFocus(tui.logFilterInput)
		}

		if event.Rune() == 'c' {
			// Clear filter with 'c' key
			tui.logFilterLock.Lock()
			if tui.logFilter != "" {
				tui.logFilter = ""
				tui.logFilterLock.Unlock()
				tui.logFilterInput.SetText("")
				tui.logLayout.RemoveItem(tui.logFilterInput)
				tui.drawContainerLog()
				tui.updateHeader()
			} else {
				tui.logFilterLock.Unlock()
			}
		}

		if event.Rune() == 't' {
			tui.logShowTimestampLock.Lock()
			tui.logShowTimestamp = !tui.logShowTimestamp
			tui.logShowTimestampLock.Unlock()
			tui.drawContainerLog()
			tui.updateHeader()
		}

		if event.Rune() == 'h' {
			tui.pages.ShowPage("help")
			tui.app.SetFocus(tui.helpTextView)
			return nil
		}

		return event
	})

	logFilterInput.SetDoneFunc(func(key tcell.Key) {
		tui.logFilterLock.Lock()
		if key == tcell.KeyEnter {
			tui.logFilter = logFilterInput.GetText()
		} else {
			tui.logFilter = ""
			logFilterInput.SetText("")
		}
		tui.logFilterLock.Unlock()
		tui.logLayout.RemoveItem(tui.logFilterInput)
		tui.app.SetFocus(tui.tableContainerLog)
		tui.drawContainerLog()
		tui.updateHeader()
	})

	containerDisappearedModal.SetDoneFunc(func(buttonIndex int, buttonLabel string) {
		tui.containerDisappearedLock.Lock()
		tui.containerDisappeared = false
		tui.containerDisappearedLock.Unlock()

		tui.pages.HidePage("modal")
		tui.tableContainer.Clear()
		tui.drawContainers()
		tui.pages.SwitchToPage("containerList")
		tui.currentViewLock.Lock()
		tui.currentView = viewProject
		tui.currentViewLock.Unlock()
		tui.currentContainerID = ""
		tui.currentContainerName = ""
		tui.currentContainerService = ""
		tui.logPausedLock.Lock()
		tui.logPaused = false
		tui.logPausedLock.Unlock()
		tui.logFilterLock.Lock()
		tui.logFilter = ""
		tui.logFilterLock.Unlock()
		tui.logFilterInput.SetText("")
		tui.logLayout.RemoveItem(tui.logFilterInput)
		tui.tableContainerLogData = nil
		tui.updateHeader()
	})

	helpTextView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc || event.Rune() == 'q' {
			tui.pages.HidePage("help")
			return nil
		}
		return event
	})

	projectSearchInput.SetChangedFunc(func(text string) {
		tui.projectSearchQuery = text
		tui.drawProjects()
	})

	projectSearchInput.SetDoneFunc(func(key tcell.Key) {
		tui.projectSearchActive = false
		tui.projectLayout.RemoveItem(tui.projectSearchInput)
		tui.app.SetFocus(tui.tableProject)
		if key == tcell.KeyEsc {
			tui.projectSearchQuery = ""
			tui.projectSearchInput.SetText("")
		}
		tui.drawProjects()
	})

	containerSearchInput.SetChangedFunc(func(text string) {
		tui.containerSearchQuery = text
		tui.drawContainers()
	})

	containerSearchInput.SetDoneFunc(func(key tcell.Key) {
		tui.containerSearchActive = false
		tui.containerLayout.RemoveItem(tui.containerSearchInput)
		tui.app.SetFocus(tui.tableContainer)
		if key == tcell.KeyEsc {
			tui.containerSearchQuery = ""
			tui.containerSearchInput.SetText("")
		}
		tui.drawContainers()
	})

	return tui
}

// fuzzyMatch checks if all characters in query appear in order in text
// Example: "cr" matches "container" because 'c' and 'r' appear in order
func fuzzyMatch(text, query string) bool {
	if query == "" {
		return true
	}

	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)

	textIdx := 0
	for _, queryChar := range queryLower {
		found := false
		for textIdx < len(textLower) {
			if rune(textLower[textIdx]) == queryChar {
				found = true
				textIdx++
				break
			}
			textIdx++
		}
		if !found {
			return false
		}
	}
	return true
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

	// Filter projects if filter is active
	if t.projectSearchQuery != "" {
		filtered := make([]dto.Project, 0)
		for _, project := range projects {
			if fuzzyMatch(project.Name, t.projectSearchQuery) {
				filtered = append(filtered, project)
			}
		}
		projects = filtered
	}

	slices.SortStableFunc(projects, func(a, b dto.Project) int {
		var cmp int
		switch t.projectSortColumn {
		case projectSortName:
			cmp = strings.Compare(a.Name, b.Name)
		case projectSortCPU:
			if a.CPUPercentage < b.CPUPercentage {
				cmp = -1
			} else if a.CPUPercentage > b.CPUPercentage {
				cmp = 1
			}
		case projectSortMemory:
			if a.MemoryPercentage < b.MemoryPercentage {
				cmp = -1
			} else if a.MemoryPercentage > b.MemoryPercentage {
				cmp = 1
			}
		case projectSortContainers:
			if a.ContainersRunning < b.ContainersRunning {
				cmp = -1
			} else if a.ContainersRunning > b.ContainersRunning {
				cmp = 1
			}
		}

		// If ascending, keep order; if descending, reverse
		if !t.projectSortAsc {
			cmp = -cmp
		}

		// Secondary sort by name if primary comparison is equal
		if cmp == 0 {
			cmp = strings.Compare(a.Name, b.Name)
		}

		return cmp
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
		var cmp int
		switch t.containerSortColumn {
		case containerSortName:
			cmp = strings.Compare(a.Service, b.Service)
		case containerSortCPU:
			if a.CPUPercentage < b.CPUPercentage {
				cmp = -1
			} else if a.CPUPercentage > b.CPUPercentage {
				cmp = 1
			}
		case containerSortMemory:
			if a.MemoryPercentage < b.MemoryPercentage {
				cmp = -1
			} else if a.MemoryPercentage > b.MemoryPercentage {
				cmp = 1
			}
		case containerSortStatus:
			cmp = strings.Compare(a.Status, b.Status)
		}

		// If ascending, keep order; if descending, reverse
		if !t.containerSortAsc {
			cmp = -cmp
		}

		// Secondary sort by name if primary comparison is equal
		if cmp == 0 {
			cmp = strings.Compare(a.Service, b.Service)
		}

		return cmp
	})
	t.tableContainerDataLock.RUnlock()

	t.tableContainer.Clear()
	t.tableProjectDataLock.RLock()
	t.RenderContainerHeader(t.tableProjectData[dto.ProjectID(t.currentProjectID)].Name)
	t.tableProjectDataLock.RUnlock()

	index := 0
	for _, container := range containers {
		if string(container.Project.ID) != t.currentProjectID {
			continue
		}

		// Filter containers if filter is active (fuzzy match)
		if t.containerSearchQuery != "" {
			if !fuzzyMatch(container.Service, t.containerSearchQuery) {
				continue
			}
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

func formatJSONLog(line string, showTimestamp bool) (string, bool) {
	// Find the JSON part (after Docker timestamp if present)
	jsonStart := strings.Index(line, "{")
	if jsonStart == -1 {
		return "", false
	}

	dockerTimestamp := strings.TrimSpace(line[:jsonStart])
	jsonPart := line[jsonStart:]

	var logData map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &logData); err != nil {
		return "", false
	}

	// Extract common fields
	var logTimestamp, level, msg string
	var extras []string

	// Timestamp fields (from the log itself)
	for _, key := range []string{"time", "timestamp", "ts", "@timestamp", "t"} {
		if v, ok := logData[key]; ok {
			logTimestamp = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Level fields
	for _, key := range []string{"level", "lvl", "severity", "loglevel"} {
		if v, ok := logData[key]; ok {
			level = strings.ToUpper(fmt.Sprintf("%v", v))
			delete(logData, key)
			break
		}
	}

	// Message fields
	for _, key := range []string{"msg", "message", "text"} {
		if v, ok := logData[key]; ok {
			msg = fmt.Sprintf("%v", v)
			delete(logData, key)
			break
		}
	}

	// Collect remaining fields sorted by key
	var keys []string
	for k := range logData {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := logData[k]
		vStr := fmt.Sprintf("%v", v)
		// Escape brackets for tview
		vStr = strings.ReplaceAll(vStr, "[", "[[]")
		extras = append(extras, fmt.Sprintf("[blue]%s[-]=%s", k, vStr))
	}

	// Build formatted line
	var parts []string

	// Show exactly one timestamp if enabled
	// Priority: log's own timestamp (formatted like Docker) > Docker's timestamp
	if showTimestamp {
		if logTimestamp != "" {
			// Try to parse and format like Docker (RFC3339Nano)
			formattedTs := formatTimestamp(logTimestamp)
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", formattedTs))
		} else if dockerTimestamp != "" {
			parts = append(parts, fmt.Sprintf("[gray]%s[-]", dockerTimestamp))
		}
	}

	if level != "" {
		levelColor := getLevelColor(level)
		parts = append(parts, fmt.Sprintf("[%s]%-5s[-]", levelColor, level))
	}
	if msg != "" {
		msg = strings.ReplaceAll(msg, "[", "[[]")
		parts = append(parts, msg)
	}
	if len(extras) > 0 {
		parts = append(parts, strings.Join(extras, " "))
	}

	return strings.Join(parts, " ") + "\n", true
}

func formatTimestamp(ts string) string {
	// Try common timestamp formats and convert to Docker format (RFC3339Nano)
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t.Format(time.RFC3339Nano)
		}
	}

	// If parsing fails, return as-is
	return ts
}

func getLevelColor(level string) string {
	switch strings.ToUpper(level) {
	case "ERROR", "ERR", "FATAL", "PANIC", "CRITICAL":
		return "red"
	case "WARN", "WARNING":
		return "yellow"
	case "INFO":
		return "green"
	case "DEBUG", "TRACE":
		return "gray"
	default:
		return "white"
	}
}

// extractTimestampPrefix tries to extract a timestamp from the beginning of a string
// Returns the timestamp, the remaining string, and whether a timestamp was found
func extractTimestampPrefix(s string) (timestamp string, rest string, found bool) {
	// Common timestamp patterns at the start of log lines
	// Docker format: "2024-01-15T10:30:00.123456789Z " or "2024-01-15T10:30:00+01:00 "
	// Log formats: "2024-01-15T10:30:00Z ", "2024-01-15 10:30:00 "

	if len(s) < 20 {
		return "", s, false
	}

	// Try to find space after timestamp
	spaceIdx := -1
	for i := 19; i < len(s) && i < 40; i++ {
		if s[i] == ' ' {
			spaceIdx = i
			break
		}
	}

	if spaceIdx == -1 {
		return "", s, false
	}

	potentialTs := s[:spaceIdx]

	// Try to parse it as a timestamp
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}

	for _, format := range formats {
		if _, err := time.Parse(format, potentialTs); err == nil {
			return potentialTs, s[spaceIdx+1:], true
		}
	}

	return "", s, false
}

func colorizeLogLine(line string, showTimestamp bool) string {
	// Try to parse as JSON first
	if formatted, ok := formatJSONLog(line, showTimestamp); ok {
		return formatted
	}

	// For non-JSON logs, handle timestamps properly
	// Docker adds timestamp at the beginning, and the log itself might have its own timestamp

	// First, extract Docker's timestamp
	dockerTs, afterDockerTs, hasDockerTs := extractTimestampPrefix(line)

	// Then check if the remaining content has its own timestamp
	logTs, content, hasLogTs := extractTimestampPrefix(afterDockerTs)

	var displayLine string
	if showTimestamp {
		// Priority: log's own timestamp (formatted) > Docker's timestamp
		if hasLogTs {
			formattedTs := formatTimestamp(logTs)
			displayLine = formattedTs + " " + content
		} else if hasDockerTs {
			displayLine = dockerTs + " " + afterDockerTs
		} else {
			displayLine = line
		}
	} else {
		// Don't show any timestamp
		if hasLogTs {
			displayLine = content
		} else if hasDockerTs {
			displayLine = afterDockerTs
		} else {
			displayLine = line
		}
	}

	lower := strings.ToLower(displayLine)

	// Escape tview color tags in the original line
	displayLine = strings.ReplaceAll(displayLine, "[", "[[]")

	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "panic"):
		return "[red]" + displayLine + "[-]"
	case strings.Contains(lower, "warn"):
		return "[yellow]" + displayLine + "[-]"
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return "[gray]" + displayLine + "[-]"
	case strings.Contains(lower, "info"):
		return "[green]" + displayLine + "[-]"
	default:
		return displayLine
	}
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

func (t *Tui) updateHeader() {
	t.currentViewLock.RLock()
	currentView := t.currentView
	t.currentViewLock.RUnlock()

	var text string
	switch currentView {
	case viewProjectList:
		t.tableProjectDataLock.RLock()
		count := len(t.tableProjectData)
		t.tableProjectDataLock.RUnlock()

		filter := ""
		if t.projectSearchQuery != "" {
			filter = fmt.Sprintf(" [white](filter: %s)[-]", t.projectSearchQuery)
		}

		t.projectRefreshPausedLock.RLock()
		paused := ""
		if t.projectRefreshPaused {
			paused = " [fuchsia]PAUSED[-]"
		}
		t.projectRefreshPausedLock.RUnlock()

		text = fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Projects([fuchsia]%d[-])[-]%s%s", count, filter, paused)

	case viewProject:
		t.tableContainerDataLock.RLock()
		count := len(t.tableContainerData)
		t.tableContainerDataLock.RUnlock()

		projectName := "unknown"
		if t.currentProjectID != "" {
			t.tableProjectDataLock.RLock()
			for _, project := range t.tableProjectData {
				if string(project.ID) == t.currentProjectID {
					projectName = project.Name
					break
				}
			}
			t.tableProjectDataLock.RUnlock()
		}

		filter := ""
		if t.containerSearchQuery != "" {
			filter = fmt.Sprintf(" [white](filter: %s)[-]", t.containerSearchQuery)
		}

		t.containerRefreshPausedLock.RLock()
		paused := ""
		if t.containerRefreshPaused {
			paused = " [fuchsia]PAUSED[-]"
		}
		t.containerRefreshPausedLock.RUnlock()

		text = fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Containers([fuchsia]%d[-])[-] [white](%s)[-]%s%s",
			count, projectName, filter, paused)

	case viewContainerLog:
		status := ""

		t.logPausedLock.RLock()
		if t.logPaused {
			status += " [fuchsia]PAUSED[-]"
		}
		t.logPausedLock.RUnlock()

		t.logFilterLock.RLock()
		if t.logFilter != "" {
			status += fmt.Sprintf(" [white](filter: %s)[-]", t.logFilter)
		}
		t.logFilterLock.RUnlock()

		t.logShowTimestampLock.RLock()
		if t.logShowTimestamp {
			status += " [white](time)[-]"
		}
		t.logShowTimestampLock.RUnlock()

		projectName := "unknown"
		if t.currentProjectID != "" {
			t.tableProjectDataLock.RLock()
			for _, project := range t.tableProjectData {
				if string(project.ID) == t.currentProjectID {
					projectName = project.Name
					break
				}
			}
			t.tableProjectDataLock.RUnlock()
		}

		text = fmt.Sprintf(" [white::b]c8s[-::] [white]|[-] [white]Logs[-] [white]%s[-] [white](%s)[-]%s",
			t.currentContainerService, projectName, status)
	}

	t.headerView.SetText(text)
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
