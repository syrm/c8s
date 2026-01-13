package tui

import (
	"sync"
	"sync/atomic"

	"github.com/rivo/tview"
)

// LogViewState manages log view state.
type LogViewState struct {
	View          *tview.TextView
	Layout        *tview.Flex
	FilterInput   *tview.InputField
	
	// Data protected by mu
	data          []string
	filter        string
	mu            sync.RWMutex

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

func (l *LogViewState) SetData(data []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Copy slice
	l.data = make([]string, len(data))
	copy(l.data, data)
}

func (l *LogViewState) GetData() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]string, len(l.data))
	copy(result, l.data)
	return result
}

func (l *LogViewState) SetFilter(f string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.filter = f
}

func (l *LogViewState) GetFilter() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.filter
}
