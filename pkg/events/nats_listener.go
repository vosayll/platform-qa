package events

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

// Event represents an asynchronous domain event
type Event struct {
	Subject   string                 `json:"subject"`
	OrderID   string                 `json:"order_id"`
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

// EventPredicate is a function that determines if an event matches criteria
type EventPredicate func(e *Event) bool

// NatsEventListener handles connection and subscription to NATS JetStream
type NatsEventListener struct {
	mu          sync.RWMutex
	nc          *nats.Conn
	js          nats.JetStreamContext
	eventStream chan *Event
	history     []*Event
	isMock      bool
}

// NewNatsEventListener connects to NATS or runs in standalone/mock mode if connection is not available
func NewNatsEventListener(natsURL string) (*NatsEventListener, error) {
	listener := &NatsEventListener{
		eventStream: make(chan *Event, 1000),
		history:     make([]*Event, 0),
	}

	nc, err := nats.Connect(natsURL, nats.Timeout(2*time.Second))
	if err != nil {
		// Fallback to local in-memory event bus
		listener.isMock = true
		return listener, nil
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		listener.isMock = true
		return listener, nil
	}

	listener.nc = nc
	listener.js = js
	return listener, nil
}

// PublishEvent publishes an event to NATS or in-memory channel
func (l *NatsEventListener) PublishEvent(event *Event) error {
	l.mu.Lock()
	l.history = append(l.history, event)
	l.mu.Unlock()

	select {
	case l.eventStream <- event:
	default:
	}

	if l.nc != nil && !l.isMock {
		data, err := json.Marshal(event)
		if err != nil {
			return err
		}
		return l.nc.Publish(event.Subject, data)
	}

	return nil
}

// AwaitEvent waits for an event that satisfies the predicate within a specified timeout
func (l *NatsEventListener) AwaitEvent(ctx context.Context, predicate EventPredicate, timeout time.Duration) (*Event, error) {
	// First check history
	l.mu.RLock()
	for _, e := range l.history {
		if predicate(e) {
			l.mu.RUnlock()
			return e, nil
		}
	}
	l.mu.RUnlock()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("timeout (%s) waiting for async event", timeout)
		case e := <-l.eventStream:
			if predicate(e) {
				return e, nil
			}
		}
	}
}

// Close closes connection
func (l *NatsEventListener) Close() {
	if l.nc != nil {
		l.nc.Close()
	}
}
