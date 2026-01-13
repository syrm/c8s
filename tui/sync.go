package tui

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rivo/tview"
)

// SyncValue provides thread-safe access to a value of any type.
type SyncValue[T any] struct {
	value T
	mu    sync.RWMutex
}

func (s *SyncValue[T]) Get() T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.value
}

func (s *SyncValue[T]) Set(v T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = v
}

// SyncSlice provides thread-safe access to a slice with copy-on-read semantics.
type SyncSlice[T any] struct {
	value []T
	mu    sync.RWMutex
}

func (s *SyncSlice[T]) Get() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]T, len(s.value))
	copy(result, s.value)
	return result
}

func (s *SyncSlice[T]) Set(v []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = make([]T, len(v))
	copy(s.value, v)
}

func (s *SyncSlice[T]) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.value = nil
}

// SortState provides thread-safe sort column and direction.
type SortState[T comparable] struct {
	column T
	asc    bool
	mu     sync.RWMutex
}

func (s *SortState[T]) Get() (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.column, s.asc
}

func (s *SortState[T]) Set(col T, asc bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.column = col
	s.asc = asc
}

// Toggle toggles sort direction if same column, otherwise sets new column with default direction.
func (s *SortState[T]) Toggle(col T, defaultAsc bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.column == col {
		s.asc = !s.asc
	} else {
		s.column = col
		s.asc = defaultAsc
	}
}

// PausableRefresh manages a pausable refresh with auto-resume timer.
type PausableRefresh struct {
	paused atomic.Bool
	timer  *time.Timer
	mu     sync.Mutex
}

func (p *PausableRefresh) IsPaused() bool {
	return p.paused.Load()
}

func (p *PausableRefresh) SetPaused(v bool) {
	p.paused.Store(v)
}

// Pause pauses the refresh and sets a timer to auto-resume.
// The checkClosing function should return true if the TUI is closing.
func (p *PausableRefresh) Pause(duration time.Duration, checkClosing func() bool) {
	p.paused.Store(true)

	p.mu.Lock()
	defer p.mu.Unlock()

	// Cancel previous timer if exists
	if p.timer != nil {
		if !p.timer.Stop() {
			select {
			case <-p.timer.C:
			default:
			}
		}
	}

	// Resume after duration
	p.timer = time.AfterFunc(duration, func() {
		if !checkClosing() {
			p.paused.Store(false)
		}
	})
}

func (p *PausableRefresh) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.timer != nil {
		if !p.timer.Stop() {
			select {
			case <-p.timer.C:
			default:
			}
		}
		p.timer = nil
	}
}

// StatusBar manages a status bar with auto-clear timer.
type StatusBar struct {
	view   *tview.TextView
	layout *tview.Flex
	timer  *time.Timer
	mu     sync.Mutex
}

func NewStatusBar() *StatusBar {
	return &StatusBar{
		view: createStatusBarView(),
	}
}

func (s *StatusBar) SetLayout(layout *tview.Flex) {
	s.layout = layout
}

func (s *StatusBar) Show(message string, duration time.Duration, app *tview.Application, checkClosing func() bool) {
	s.view.SetText("[red]" + escapeColorTags(message) + "[-]")
	if s.layout != nil {
		s.layout.AddItem(s.view, 1, 0, false)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.timer != nil {
		if !s.timer.Stop() {
			select {
			case <-s.timer.C:
			default:
			}
		}
	}

	s.timer = time.AfterFunc(duration, func() {
		if checkClosing() {
			return
		}
		app.QueueUpdateDraw(func() {
			if s.layout != nil {
				s.layout.RemoveItem(s.view)
			}
			s.view.SetText("")
		})
	})
}

func (s *StatusBar) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		if !s.timer.Stop() {
			select {
			case <-s.timer.C:
			default:
			}
		}
		s.timer = nil
	}
}

// SearchState manages search input and query.
type SearchState struct {
	Input *tview.InputField
	query SyncValue[string]
}

func NewSearchState() *SearchState {
	return &SearchState{
		Input: createSearchInput(),
	}
}

func (s *SearchState) Query() string {
	return s.query.Get()
}

func (s *SearchState) SetQuery(q string) {
	s.query.Set(q)
}

// NavigationState manages the current navigation state.
type NavigationState struct {
	view             SyncValue[currentView]
	projectID        string
	projectName      string
	containerID      string
	containerName    string
	containerService string
	mu               sync.RWMutex
}

func (n *NavigationState) View() currentView {
	return n.view.Get()
}

func (n *NavigationState) SetView(v currentView) {
	n.view.Set(v)
}

func (n *NavigationState) ProjectID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.projectID
}

func (n *NavigationState) SetProjectID(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projectID = id
}

func (n *NavigationState) ProjectName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.projectName
}

func (n *NavigationState) SetProjectName(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.projectName = name
}

func (n *NavigationState) ContainerID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerID
}

func (n *NavigationState) SetContainerID(id string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = id
}

func (n *NavigationState) ContainerName() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerName
}

func (n *NavigationState) ContainerService() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.containerService
}

func (n *NavigationState) SetContainerInfo(id, name, service string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = id
	n.containerName = name
	n.containerService = service
}

func (n *NavigationState) ClearContainerInfo() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.containerID = ""
	n.containerName = ""
	n.containerService = ""
}

// ActionController manages container action goroutines.
type ActionController struct {
	ctx           context.Context
	cancel        context.CancelFunc
	mu            sync.Mutex
	wg            sync.WaitGroup
	sem           chan struct{}
	maxConcurrent int
}

// NewActionController creates a new ActionController with placeholder context.
// Call Start(ctx) to initialize with a proper parent context.
func NewActionController(maxConcurrent int) *ActionController {
	return &ActionController{
		ctx:           context.Background(), // Placeholder until Start() is called
		sem:           make(chan struct{}, maxConcurrent),
		maxConcurrent: maxConcurrent,
	}
}

// Start initializes the ActionController with a parent context.
// This should be called before using the controller.
func (a *ActionController) Start(parent context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ctx, a.cancel = context.WithCancel(parent)
}

func (a *ActionController) Context() context.Context {
	return a.ctx
}

func (a *ActionController) TryAcquire() bool {
	select {
	case a.sem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *ActionController) Release() {
	<-a.sem
}

func (a *ActionController) Add(delta int) {
	a.wg.Add(delta)
}

func (a *ActionController) Done() {
	a.wg.Done()
}

func (a *ActionController) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancel != nil {
		a.cancel()
		a.cancel = nil
	}
}

func (a *ActionController) Wait() {
	a.wg.Wait()
}

// LogViewState manages log view state.
type LogViewState struct {
	View          *tview.TextView
	Layout        *tview.Flex
	FilterInput   *tview.InputField
	Data          SyncSlice[string]
	Filter        SyncValue[string]
	Paused        atomic.Bool
	ShowTimestamp atomic.Bool
	Disappeared   atomic.Bool
}

func (l *LogViewState) TogglePaused() {
	for {
		old := l.Paused.Load()
		if l.Paused.CompareAndSwap(old, !old) {
			break
		}
	}
}

func (l *LogViewState) ToggleTimestamp() {
	for {
		old := l.ShowTimestamp.Load()
		if l.ShowTimestamp.CompareAndSwap(old, !old) {
			break
		}
	}
}
