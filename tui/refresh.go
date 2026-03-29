package tui

import (
	"sync"
	"sync/atomic"
	"time"
)

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
