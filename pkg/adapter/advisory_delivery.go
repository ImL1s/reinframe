package adapter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// HumanAlerter is invoked when automated advice delivery is impossible
// (e.g. missing CapAdviceDelivery on observe-only agents).
type HumanAlerter interface {
	Alert(ctx context.Context, sessionID string, intervention protocol.Intervention, reason string) error
}

// NopHumanAlerter is a no-op HumanAlerter for unit tests of non-escalation paths.
// Production / observe-only escalation must not use Nop: DeliverPending will refuse
// to treat a Nop alert as a successful human escalation.
type NopHumanAlerter struct{}

// Alert implements HumanAlerter (no side effects).
func (NopHumanAlerter) Alert(context.Context, string, protocol.Intervention, string) error {
	return nil
}

// ErrNopHumanAlerter is returned when escalation requires a real HumanAlerter
// but configuration still uses NopHumanAlerter (or equivalent silent sink).
var ErrNopHumanAlerter = errors.New("human alerter is NopHumanAlerter; cannot claim escalation succeeded")

// RecordingAlerter records Alert calls for tests.
type RecordingAlerter struct {
	mu    sync.Mutex
	Calls []AlertCall
}

// AlertCall is one recorded HumanAlerter.Alert invocation.
type AlertCall struct {
	SessionID    string
	Intervention protocol.Intervention
	Reason       string
}

// Alert implements HumanAlerter.
func (r *RecordingAlerter) Alert(_ context.Context, sessionID string, intervention protocol.Intervention, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Calls = append(r.Calls, AlertCall{
		SessionID:    sessionID,
		Intervention: intervention,
		Reason:       reason,
	})
	return nil
}

// Len returns the number of recorded alerts.
func (r *RecordingAlerter) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Calls)
}

// Snapshot returns a copy of recorded alerts.
func (r *RecordingAlerter) Snapshot() []AlertCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]AlertCall, len(r.Calls))
	copy(out, r.Calls)
	return out
}

// AdvisoryDeliveryConfig configures safe-turn advisory delivery (#68).
type AdvisoryDeliveryConfig struct {
	// Actuator delivers interventions to the target agent. Required for delivery.
	Actuator InterventionActuator
	// Alerter receives human-escalation alerts.
	// When SupportsAdviceDelivery is false, a non-Nop Alerter is required (production).
	// When SupportsAdviceDelivery is true, Alerter may be nil/Nop (only used on escalate).
	Alerter HumanAlerter
	// SupportsAdviceDelivery simulates CapAdviceDelivery. When false, DeliverPending
	// returns unsupported_capability and invokes Alerter instead of Actuator.
	// Alert failures (and Nop alerter) return errors — never silent "escalated" success.
	SupportsAdviceDelivery bool
	// DefaultTTL is applied when Enqueue is called with ttl <= 0.
	DefaultTTL time.Duration
	// Queue is optional; a new PendingQueue is created when nil.
	Queue *PendingQueue
}

// AdvisoryDelivery owns the pending queue and turn-boundary delivery path.
type AdvisoryDelivery struct {
	actuator               InterventionActuator
	alerter                HumanAlerter
	supportsAdviceDelivery bool
	defaultTTL             time.Duration
	queue                  *PendingQueue
}

// NewAdvisoryDelivery builds an AdvisoryDelivery from config.
func NewAdvisoryDelivery(cfg AdvisoryDeliveryConfig) (*AdvisoryDelivery, error) {
	if cfg.Actuator == nil {
		return nil, errors.New("actuator is required")
	}
	alerter := cfg.Alerter
	if alerter == nil {
		alerter = NopHumanAlerter{}
	}
	ttl := cfg.DefaultTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	q := cfg.Queue
	if q == nil {
		q = NewPendingQueue()
	}
	return &AdvisoryDelivery{
		actuator:               cfg.Actuator,
		alerter:                alerter,
		supportsAdviceDelivery: cfg.SupportsAdviceDelivery,
		defaultTTL:             ttl,
		queue:                  q,
	}, nil
}

// Queue returns the underlying pending queue (shared with hook gate callers).
func (d *AdvisoryDelivery) Queue() *PendingQueue {
	return d.queue
}

// Enqueue queues an intervention for later safe-turn delivery.
func (d *AdvisoryDelivery) Enqueue(intervention protocol.Intervention, ttl time.Duration) EnqueueResult {
	if ttl <= 0 {
		ttl = d.defaultTTL
	}
	return d.queue.Enqueue(intervention, ttl)
}

// DeliverPending delivers the next pending intervention for sessionID at a safe
// turn boundary. Returns the item (with updated state) and the delivery result.
//
// If SupportsAdviceDelivery is false, the actuator is not called; a structured
// unsupported result is recorded and HumanAlerter is invoked.
func (d *AdvisoryDelivery) DeliverPending(ctx context.Context, sessionID string) (*PendingItem, InterventionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	item := d.queue.NextPending(sessionID)
	if item == nil {
		return nil, InterventionResult{}, errors.New("no pending intervention for session")
	}

	// Re-check expiry between dequeue and deliver (clock may have advanced).
	// NextPending already checked, but synthetic path still valid.

	if !d.supportsAdviceDelivery {
		now := time.Now().UTC()
		res := InterventionResult{
			InterventionID: item.Intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   DeliveryModeHumanEscalation,
			DeliveredAt:    now,
			AckStatus:      AckStatusUnsupported,
			ErrorClass:     ErrorClassUnsupportedCapability,
			Message:        fmt.Sprintf("missing capability %s; escalate to human required", CapAdviceDelivery),
		}
		// Refuse silent success: Nop alerter must not count as human notification.
		if _, isNop := d.alerter.(NopHumanAlerter); isNop {
			res.Message = fmt.Sprintf("%s: %v", res.Message, ErrNopHumanAlerter)
			res.ErrorClass = ErrorClassTransport
			d.queue.UpdateState(item.Intervention.InterventionID, StateFailed, &res)
			out := *item
			out.State = StateFailed
			out.Result = &res
			return &out, res, ErrNopHumanAlerter
		}
		if err := d.alerter.Alert(ctx, sessionID, item.Intervention, res.Message); err != nil {
			res.Message = fmt.Sprintf("%s: alerter error: %v", res.Message, err)
			res.ErrorClass = ErrorClassTransport
			d.queue.UpdateState(item.Intervention.InterventionID, StateFailed, &res)
			out := *item
			out.State = StateFailed
			out.Result = &res
			return &out, res, err
		}
		d.queue.UpdateState(item.Intervention.InterventionID, StateFailed, &res)
		out := *item
		out.State = StateFailed
		out.Result = &res
		// Escalation path: delivery to agent unsupported; human alert succeeded.
		return &out, res, nil
	}

	res, err := d.actuator.Deliver(ctx, item.Intervention)
	state := mapResultToState(res, err)
	d.queue.UpdateState(item.Intervention.InterventionID, state, &res)
	out := *item
	out.State = state
	out.Result = &res
	return &out, res, err
}

// awaitingExplicitACK reports whether the item may still receive an explicit ACK.
// TRANSPORT_ACCEPTED / SESSION_VISIBLE remain open for a stronger layer; DELIVERING too.
func awaitingExplicitACK(st DeliveryState) bool {
	switch st {
	case StateDelivering, StateTransportAccepted, StateSessionVisible:
		return true
	default:
		return false
	}
}

// Acknowledge records an external ACK / reject / timeout for a delivery in flight.
// Only DELIVERING / TRANSPORT_ACCEPTED / SESSION_VISIBLE accepted — ACK before Deliver is rejected.
// status must be one of acked | rejected | timed_out.
// Explicit ACK never upgrades transport-only evidence by itself unless status is acked from a pinned source.
func (d *AdvisoryDelivery) Acknowledge(interventionID, status string) error {
	item, ok := d.queue.Get(interventionID)
	if !ok {
		return fmt.Errorf("unknown intervention %q", interventionID)
	}
	if !awaitingExplicitACK(item.State) {
		return fmt.Errorf("intervention %q not awaiting ack (state=%s; only DELIVERING/TRANSPORT_ACCEPTED/SESSION_VISIBLE accepted)", interventionID, item.State)
	}

	now := time.Now().UTC()
	res := InterventionResult{
		InterventionID: interventionID,
		DeliveryMode:   DefaultDeliveryMode(item.Intervention.ActionType),
		DeliveredAt:    now,
		ErrorClass:     ErrorClassNone,
	}
	if item.Result != nil {
		res = *item.Result
	}

	var state DeliveryState
	switch status {
	case AckStatusAcked:
		// External explicit ACK from a pinned source — StateAcked remains the durable terminal.
		// AckLayer is recorded as explicit; transport-only results never reach this branch.
		state = StateAcked
		res.Accepted = true
		res.AckStatus = AckStatusAcked
		res.AckLayer = ACKLayerExplicit
		res.ErrorClass = ErrorClassNone
		ackAt := now
		res.AckAt = &ackAt
	case AckStatusRejected:
		state = StateRejected
		res.Accepted = false
		res.AckStatus = AckStatusRejected
		res.ErrorClass = ErrorClassAgentRejected
		ackAt := now
		res.AckAt = &ackAt
	case AckStatusTimedOut:
		state = StateTimedOut
		res.Accepted = false
		res.AckStatus = AckStatusTimedOut
		res.ErrorClass = ErrorClassTimeout
	default:
		return fmt.Errorf("invalid ack status %q", status)
	}

	d.queue.UpdateState(interventionID, state, &res)
	return nil
}

// Get returns the current queue item snapshot.
func (d *AdvisoryDelivery) Get(interventionID string) (*PendingItem, bool) {
	return d.queue.Get(interventionID)
}

func mapResultToState(res InterventionResult, err error) DeliveryState {
	switch res.AckStatus {
	case AckStatusAcked:
		if res.AckLayer == ACKLayerExplicit {
			return StateExplicitACK
		}
		if res.AckLayer == ACKLayerBehavioral {
			return StateBehavioralACK
		}
		return StateAcked
	case AckStatusRejected:
		return StateRejected
	case AckStatusTimedOut:
		return StateTimedOut
	case AckStatusUnsupported:
		return StateUnsupported
	case AckStatusPending:
		if err != nil {
			return StateFailed
		}
		// Honest intermediate states from host ACK layer (#108).
		switch res.AckLayer {
		case ACKLayerSessionVisible:
			return StateSessionVisible
		case ACKLayerTransport:
			return StateTransportAccepted
		default:
			return StateDelivering
		}
	default:
		if res.ErrorClass == ErrorClassUnsupportedCapability {
			return StateUnsupported
		}
		if res.ErrorClass == ErrorClassTimeout {
			return StateTimedOut
		}
		if res.ErrorClass == ErrorClassTransport || err != nil {
			return StateFailed
		}
		if res.Accepted {
			return StateAcked
		}
		return StateFailed
	}
}
