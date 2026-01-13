package channel

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSend_OK(t *testing.T) {
	ch := make(chan int, 1)
	ctx := context.Background()

	result := Send(ctx, ch, 42, time.Second)

	if result != SendOK {
		t.Errorf("Expected SendOK, got %v", result)
	}

	received := <-ch
	if received != 42 {
		t.Errorf("Expected 42, got %d", received)
	}
}

func TestSend_Timeout(t *testing.T) {
	ch := make(chan int) // Unbuffered, will block
	ctx := context.Background()

	result := Send(ctx, ch, 42, 10*time.Millisecond)

	if result != SendTimeout {
		t.Errorf("Expected SendTimeout, got %v", result)
	}
}

func TestSend_ContextDone(t *testing.T) {
	ch := make(chan int) // Unbuffered, will block
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	result := Send(ctx, ch, 42, time.Second)

	if result != SendContextDone {
		t.Errorf("Expected SendContextDone, got %v", result)
	}
}

func TestReceive_OK(t *testing.T) {
	ch := make(chan int, 1)
	ch <- 42
	ctx := context.Background()

	value, result := Receive(ctx, ch, time.Second)

	if result != ReceiveOK {
		t.Errorf("Expected ReceiveOK, got %v", result)
	}
	if value != 42 {
		t.Errorf("Expected 42, got %d", value)
	}
}

func TestReceive_Timeout(t *testing.T) {
	ch := make(chan int) // Empty channel
	ctx := context.Background()

	value, result := Receive(ctx, ch, 10*time.Millisecond)

	if result != ReceiveTimeout {
		t.Errorf("Expected ReceiveTimeout, got %v", result)
	}
	if value != 0 {
		t.Errorf("Expected zero value, got %d", value)
	}
}

func TestReceive_ContextDone(t *testing.T) {
	ch := make(chan int) // Empty channel
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	value, result := Receive(ctx, ch, time.Second)

	if result != ReceiveContextDone {
		t.Errorf("Expected ReceiveContextDone, got %v", result)
	}
	if value != 0 {
		t.Errorf("Expected zero value, got %d", value)
	}
}

func TestSendReceive_OK(t *testing.T) {
	sendCh := make(chan int, 1)
	recvCh := make(chan string, 1)
	ctx := context.Background()

	// Simulate a responder
	go func() {
		<-sendCh
		recvCh <- "response"
	}()

	resp, ok := SendReceive(ctx, sendCh, 42, recvCh, time.Second)

	if !ok {
		t.Error("Expected SendReceive to succeed")
	}
	if resp != "response" {
		t.Errorf("Expected 'response', got '%s'", resp)
	}
}

func TestSendReceive_SendTimeout(t *testing.T) {
	sendCh := make(chan int) // Unbuffered, will block
	recvCh := make(chan string, 1)
	ctx := context.Background()

	resp, ok := SendReceive(ctx, sendCh, 42, recvCh, 10*time.Millisecond)

	if ok {
		t.Error("Expected SendReceive to fail")
	}
	if resp != "" {
		t.Errorf("Expected empty string, got '%s'", resp)
	}
}

func TestSendReceive_ReceiveTimeout(t *testing.T) {
	sendCh := make(chan int, 1)
	recvCh := make(chan string) // Empty, will timeout
	ctx := context.Background()

	resp, ok := SendReceive(ctx, sendCh, 42, recvCh, 10*time.Millisecond)

	if ok {
		t.Error("Expected SendReceive to fail")
	}
	if resp != "" {
		t.Errorf("Expected empty string, got '%s'", resp)
	}
}

// TestConcurrentSend tests concurrent sends to a channel.
func TestConcurrentSend(t *testing.T) {
	ch := make(chan int, 100)
	ctx := context.Background()

	const numGoroutines = 50

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(val int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				Send(ctx, ch, val, time.Second)
			}
		}(i)
	}

	// Drain channel in background
	done := make(chan struct{})
	go func() {
		count := 0
		for range ch {
			count++
			if count >= numGoroutines*100 {
				break
			}
		}
		close(done)
	}()

	wg.Wait()
	close(ch)
	<-done
}

// TestConcurrentReceive tests concurrent receives from a channel without data races.
func TestConcurrentReceive(t *testing.T) {
	ch := make(chan int, 100)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	const numGoroutines = 10
	const numMessages = 100

	// Send messages continuously
	go func() {
		for i := 0; i < numMessages; i++ {
			select {
			case ch <- i:
			case <-ctx.Done():
				return
			}
		}
	}()

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Multiple concurrent receivers - just verify no race conditions
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, result := Receive(ctx, ch, 100*time.Millisecond)
				if result == ReceiveContextDone {
					return
				}
			}
		}()
	}

	wg.Wait()
	// Test passes if no race conditions are detected
}
