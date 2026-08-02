package adapter

import (
	"context"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// EventSource is the inbound stream of canonical agent events from a Target Agent adapter.
//
// Events returns a receive-only channel of protocol.AgentEvent values. The channel
// is closed when the source ends or ctx is cancelled. Implementations must not
// emit raw harness JSON; only protocol.AgentEvent is allowed.
type EventSource interface {
	Events(ctx context.Context) (<-chan protocol.AgentEvent, error)
}
