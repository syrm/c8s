package tui

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/syrm/c8s/internal/model"
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
	requestData chan model.RequestData

	// Actions
	actions *ActionController

	// Screen width
	tableWidth atomic.Int32

	// Lifecycle
	dataWG    sync.WaitGroup
	closing   atomic.Bool
	closeOnce sync.Once
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
		},
		containerView: ContainerView{
			Table:  tableContainer,
			Layout: containerLayout,
			Search: containerSearch,
		},
		logView: LogViewState{
			View:        logView,
			Layout:      logLayout,
			FilterInput: logFilterInput,
		},

		disappearedModal: disappearedModal,
		helpModal:        helpModal,
		helpTextView:     helpTextView,

		requestData: make(chan model.RequestData),
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

func (t *Tui) Render(ctx context.Context) error {
	// Initialize ActionController with parent context
	t.actions.Start(ctx)

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

	// Use sync.Once to ensure channel is closed exactly once
	t.closeOnce.Do(func() {
		close(t.requestData)
	})
}
