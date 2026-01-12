// Package timer provides utilities for working with time.Timer and time.Ticker
// with proper cleanup to prevent resource leaks.
package timer

import "time"

// Stop stops a timer if it hasn't already fired, preventing resource leaks.
// Safe to call multiple times on the same timer or with nil.
func Stop(t *time.Timer) {
	if t == nil {
		return
	}
	if !t.Stop() {
		// If the timer already fired, drain the channel to prevent goroutine leak
		select {
		case <-t.C:
		default:
		}
	}
}

// StopTicker stops a ticker and drains its channel to prevent resource leaks.
// Safe to call multiple times on the same ticker or with nil.
func StopTicker(t *time.Ticker) {
	if t == nil {
		return
	}
	t.Stop()
	// Drain the channel to prevent goroutine leak
	select {
	case <-t.C:
	default:
	}
}

// New creates a new timer with the given duration.
// Always call defer timer.Stop(t) after creating a timer.
func New(d time.Duration) *time.Timer {
	return time.NewTimer(d)
}
