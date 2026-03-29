// Package channel provides generic helpers for channel operations with timeouts.
package channel

import (
	"context"
	"time"
)

// SendResult represents the result of a send operation.
type SendResult int

const (
	SendOK SendResult = iota
	SendTimeout
	SendContextDone
)

// Send sends a value to a channel with timeout and context cancellation support.
// Returns SendOK if the value was sent, SendTimeout if the timeout expired,
// or SendContextDone if the context was cancelled.
func Send[T any](ctx context.Context, ch chan<- T, value T, timeout time.Duration) SendResult {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case ch <- value:
		return SendOK
	case <-timer.C:
		return SendTimeout
	case <-ctx.Done():
		return SendContextDone
	}
}

// ReceiveResult represents the result of a receive operation.
type ReceiveResult int

const (
	ReceiveOK ReceiveResult = iota
	ReceiveTimeout
	ReceiveContextDone
)

// Receive receives a value from a channel with timeout and context cancellation support.
// Returns the value, ReceiveOK if successful, ReceiveTimeout if the timeout expired,
// or ReceiveContextDone if the context was cancelled.
func Receive[T any](ctx context.Context, ch <-chan T, timeout time.Duration) (T, ReceiveResult) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case v := <-ch:
		return v, ReceiveOK
	case <-timer.C:
		var zero T
		return zero, ReceiveTimeout
	case <-ctx.Done():
		var zero T
		return zero, ReceiveContextDone
	}
}

// SendReceive performs a send followed by a receive, commonly used for request/response patterns.
// Returns the response value and whether the operation was successful.
func SendReceive[TReq, TResp any](
	ctx context.Context,
	sendCh chan<- TReq,
	request TReq,
	recvCh <-chan TResp,
	timeout time.Duration,
) (TResp, bool) {
	if Send(ctx, sendCh, request, timeout) != SendOK {
		var zero TResp
		return zero, false
	}

	resp, result := Receive(ctx, recvCh, timeout)
	return resp, result == ReceiveOK
}
