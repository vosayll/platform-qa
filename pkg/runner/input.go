package runner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// InputWaitTimeout bounds how long a run pauses while waiting for the
// operator-entered client verification code.
const InputWaitTimeout = 3 * time.Minute

// InputBroker collects one-shot operator inputs (verification codes) keyed by
// run ID. Waiters are served strictly in arrival order, so a batch with several
// concurrent client logins resolves as a sequence of prompts instead of
// failing fast.
type InputBroker struct {
	mu      sync.Mutex
	waiters map[string][]chan string
	pending map[string]string // runID -> code delivered before any waiter registered
}

// NewInputBroker creates an empty broker
func NewInputBroker() *InputBroker {
	return &InputBroker{
		waiters: make(map[string][]chan string),
		pending: make(map[string]string),
	}
}

// Deliver hands the code to the oldest waiter of runID. When nobody waits yet,
// the code is buffered and consumed by the next Wait (ordering-safe).
func (b *InputBroker) Deliver(runID, code string) bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	ws := b.waiters[runID]
	if len(ws) == 0 {
		b.pending[runID] = code
		return false
	}
	ch := ws[0]
	b.waiters[runID] = ws[1:]
	if len(b.waiters[runID]) == 0 {
		delete(b.waiters, runID)
	}
	select {
	case ch <- code:
	default:
	}
	return true
}

// Wait registers the caller as waiting for runID and blocks until Deliver,
// ctx cancellation or timeout.
func (b *InputBroker) Wait(ctx context.Context, runID string, timeout time.Duration) (string, error) {
	b.mu.Lock()
	if code, ok := b.pending[runID]; ok {
		delete(b.pending, runID)
		b.mu.Unlock()
		return code, nil
	}
	ch := make(chan string, 1)
	b.waiters[runID] = append(b.waiters[runID], ch)
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		ws := b.waiters[runID]
		for i, w := range ws {
			if w == ch {
				b.waiters[runID] = append(ws[:i], ws[i+1:]...)
				break
			}
		}
		if len(b.waiters[runID]) == 0 {
			delete(b.waiters, runID)
		}
		b.mu.Unlock()
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case code := <-ch:
		return code, nil
	case <-ctx.Done():
		return "", fmt.Errorf("ожидание кода прервано: %w", ctx.Err())
	case <-timer.C:
		return "", fmt.Errorf("код верификации не введён за %d минут — перезапустите прогон и введите код из Telegram", int(timeout.Minutes()))
	}
}

// InputRef binds the shared broker to the currently executing run and travels
// through context. It implements fixtures.InputRequester; the context key it
// is stored under lives in the fixtures package so fixtures can discover it
// without importing runner (which would be an import cycle).
type InputRef struct {
	Broker *InputBroker
	RunID  string
}

// RequestVerificationCode notifies the operator first (notify must be invoked
// BEFORE waiting so the UI prompt appears), then blocks until the code is
// delivered or the wait times out.
func (r *InputRef) RequestVerificationCode(ctx context.Context, notify func(runID, phone string), phone string) (string, error) {
	if r == nil || r.Broker == nil {
		return "", fmt.Errorf("брокер ручного ввода недоступен")
	}
	if notify != nil {
		notify(r.RunID, phone)
	}
	return r.Broker.Wait(ctx, r.RunID, InputWaitTimeout)
}
