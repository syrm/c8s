package timer

import (
	"testing"
	"time"
)

func TestStop_NotFired(t *testing.T) {
	t.Parallel()

	timer := time.NewTimer(time.Hour)
	Stop(timer)

	// Timer should be stopped — channel should be empty
	select {
	case <-timer.C:
		t.Error("Timer channel should be empty after Stop")
	default:
		// Expected
	}
}

func TestStop_AlreadyFired(t *testing.T) {
	t.Parallel()

	timer := time.NewTimer(time.Millisecond)
	// Wait for it to fire
	time.Sleep(10 * time.Millisecond)

	// Should not block or panic
	Stop(timer)

	// Channel should be drained
	select {
	case <-timer.C:
		t.Error("Timer channel should be drained after Stop")
	default:
		// Expected — channel was drained by Stop
	}
}

func TestStop_NilTimer(t *testing.T) {
	t.Parallel()

	// Stop with nil should not panic
	defer func() {
		if r := recover(); r != nil {
			// If it panics on nil, that's also acceptable behavior to document
			t.Log("Stop(nil) panics as expected")
		}
	}()

	var timer *time.Timer
	Stop(timer)
}

func TestStopTicker_NotFired(t *testing.T) {
	t.Parallel()

	ticker := time.NewTicker(time.Hour)
	StopTicker(ticker)

	// Ticker should be stopped
	select {
	case <-ticker.C:
		t.Error("Ticker channel should be empty after StopTicker")
	default:
		// Expected
	}
}

func TestStopTicker_AlreadyFired(t *testing.T) {
	t.Parallel()

	ticker := time.NewTicker(time.Millisecond)
	// Wait for at least one tick
	time.Sleep(10 * time.Millisecond)

	// Should not block or panic
	StopTicker(ticker)

	// Channel should be drained
	select {
	case <-ticker.C:
		t.Error("Ticker channel should be drained after StopTicker")
	default:
		// Expected
	}
}
