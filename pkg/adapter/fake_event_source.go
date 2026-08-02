package adapter

import (
	"context"
	"errors"
	"sync"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FakeEventSource is a thread-safe in-memory EventSource for unit tests.
// Call Inject to push events; Close ends the source.
type FakeEventSource struct {
	mu     sync.Mutex
	inbox  chan protocol.AgentEvent
	closed bool
}

// NewFakeEventSource creates a FakeEventSource with the given inbox buffer size.
// Prefer buffer >= 1 so Inject does not block waiting for a consumer.
func NewFakeEventSource(buffer int) *FakeEventSource {
	if buffer < 1 {
		buffer = 1
	}
	return &FakeEventSource{
		inbox: make(chan protocol.AgentEvent, buffer),
	}
}

// Inject pushes an event into the source. Returns an error if the source is closed
// or the inbox buffer is full.
func (f *FakeEventSource) Inject(ev protocol.AgentEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return errors.New("fake event source closed")
	}
	select {
	case f.inbox <- ev:
		return nil
	default:
		return errors.New("fake event source inbox full")
	}
}

// Close marks the source closed and closes the inbox channel once.
func (f *FakeEventSource) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return
	}
	f.closed = true
	close(f.inbox)
}

// Events implements EventSource. The returned channel closes when ctx is done
// or the source is Closed.
func (f *FakeEventSource) Events(ctx context.Context) (<-chan protocol.AgentEvent, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	out := make(chan protocol.AgentEvent)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-f.inbox:
				if !ok {
					return
				}
				select {
				case out <- ev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
