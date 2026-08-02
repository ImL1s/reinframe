package adapter

import (
	"context"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FakeActuator is a thread-safe InterventionActuator for unit tests.
// It records Deliver calls and can simulate unsupported capability, reject,
// delay/timeout, and custom result overrides.
type FakeActuator struct {
	mu sync.Mutex

	// Calls is the ordered list of interventions passed to Deliver.
	Calls []protocol.Intervention

	// Unsupported when true makes Deliver return unsupported_capability.
	Unsupported bool
	// UnsupportedMessage is included in InterventionResult.Message when Unsupported.
	UnsupportedMessage string

	// Reject when true makes Deliver return accepted=false with agent_rejected.
	Reject bool

	// Delay is applied before completing Deliver (respects ctx cancellation).
	Delay time.Duration

	// DeliveryMode overrides DefaultDeliveryMode when non-empty.
	DeliveryMode string

	// AutoAck when true (default) sets AckStatusAcked and AckAt on success.
	// When false, success returns AckStatusPending with no AckAt.
	AutoAck bool

	// ResultHook, if set, completely overrides the result after recording the call
	// (still respects Delay and ctx cancellation before the hook runs).
	ResultHook func(ctx context.Context, intervention protocol.Intervention) (InterventionResult, error)
}

// NewFakeActuator returns a FakeActuator with AutoAck enabled.
func NewFakeActuator() *FakeActuator {
	return &FakeActuator{AutoAck: true}
}

// Deliver implements InterventionActuator.
func (f *FakeActuator) Deliver(ctx context.Context, intervention protocol.Intervention) (InterventionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	f.mu.Lock()
	f.Calls = append(f.Calls, intervention)
	unsupported := f.Unsupported
	unsupportedMsg := f.UnsupportedMessage
	reject := f.Reject
	delay := f.Delay
	mode := f.DeliveryMode
	autoAck := f.AutoAck
	hook := f.ResultHook
	f.mu.Unlock()

	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			now := time.Now().UTC()
			return InterventionResult{
				InterventionID: intervention.InterventionID,
				Accepted:       false,
				DeliveryMode:   pickMode(mode, intervention.ActionType),
				DeliveredAt:    now,
				AckStatus:      AckStatusTimedOut,
				ErrorClass:     ErrorClassTimeout,
				Message:        "deliver timed out",
			}, ctx.Err()
		}
	}

	if hook != nil {
		return hook(ctx, intervention)
	}

	now := time.Now().UTC()
	dm := pickMode(mode, intervention.ActionType)

	if unsupported {
		msg := unsupportedMsg
		if msg == "" {
			msg = "capability not supported by target agent"
		}
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   dm,
			DeliveredAt:    now,
			AckStatus:      AckStatusUnsupported,
			ErrorClass:     ErrorClassUnsupportedCapability,
			Message:        msg,
		}, nil
	}

	if reject {
		ackAt := now
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   dm,
			DeliveredAt:    now,
			AckStatus:      AckStatusRejected,
			AckAt:          &ackAt,
			ErrorClass:     ErrorClassAgentRejected,
			Message:        "agent rejected intervention",
		}, nil
	}

	res := InterventionResult{
		InterventionID: intervention.InterventionID,
		Accepted:       true,
		DeliveryMode:   dm,
		DeliveredAt:    now,
		AckStatus:      AckStatusPending,
		ErrorClass:     ErrorClassNone,
	}
	if autoAck {
		ackAt := now
		res.AckStatus = AckStatusAcked
		res.AckAt = &ackAt
	}
	return res, nil
}

// CallCount returns the number of recorded Deliver calls.
func (f *FakeActuator) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// LastCall returns a copy of the most recent intervention, or false if none.
func (f *FakeActuator) LastCall() (protocol.Intervention, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return protocol.Intervention{}, false
	}
	return f.Calls[len(f.Calls)-1], true
}

// Reset clears recorded calls.
func (f *FakeActuator) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = nil
}

func pickMode(override, actionType string) string {
	if override != "" {
		return override
	}
	return DefaultDeliveryMode(actionType)
}
