package tui

import (
	"sync"
	"time"

	"github.com/rivo/tview"
)

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
