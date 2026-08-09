package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

// AdvisoryDeliveryConfig configures safe-turn advisory delivery (#68 + #108).
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
	// Ledger is optional append-only durable delivery log (#108). When set,
	// AlreadyDelivered InterventionIDs are suppressed without calling Actuator,
	// and successful intermediate/terminal results are recorded.
	Ledger *DurableAdviceLedger
	// DedupeHostFamily pins host family into ledger dedupe keys (session/host/action-bound).
	// Should match the actuator's HostFamily (e.g. GrokLiveHostFamily) so restart suppress matches writes.
	DedupeHostFamily string
}

// AdvisoryDelivery owns the pending queue and turn-boundary delivery path.
type AdvisoryDelivery struct {
	actuator               InterventionActuator
	alerter                HumanAlerter
	supportsAdviceDelivery bool
	defaultTTL             time.Duration
	queue                  *PendingQueue
	ledger                 *DurableAdviceLedger
	dedupeHostFamily       string
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
		ledger:                 cfg.Ledger,
		dedupeHostFamily:       cfg.DedupeHostFamily,
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

	// Durable restart dedupe: session/host/action-bound (#200).
	if d.ledger != nil {
		host := d.dedupeHostFamily
		if item.Result != nil && item.Result.HostFamily != "" {
			host = item.Result.HostFamily
		}
		if d.ledger.AlreadyDeliveredKey(item.Intervention.InterventionID, sessionID, host, item.Intervention.Fingerprint) {
			now := time.Now().UTC()
			res := InterventionResult{
				InterventionID: item.Intervention.InterventionID,
				Accepted:       false,
				DeliveryMode:   DefaultDeliveryMode(item.Intervention.ActionType),
				DeliveredAt:    now,
				AckStatus:      AckStatusRejected,
				ErrorClass:     ErrorClassNone,
				Message:        "duplicate InterventionID suppressed by durable ledger",
				AckLayer:       ACKLayerNone,
			}
			d.queue.UpdateState(item.Intervention.InterventionID, StateSuppressed, &res)
			_ = d.ledger.RecordResult(StateDelivering, sessionID, res, StateSuppressed)
			out := *item
			out.State = StateSuppressed
			out.Result = &res
			return &out, res, nil
		}
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
	if res.HostFamily == "" {
		res.HostFamily = d.dedupeHostFamily
	}
	state := mapResultToState(res, err)
	d.queue.UpdateState(item.Intervention.InterventionID, state, &res)
	if d.ledger != nil {
		fp := item.Intervention.Fingerprint
		if lerr := d.ledger.RecordResultWithSource(StateDelivering, sessionID, res, state, "", "", "", fp); lerr != nil {
			// Host may have accepted; durable commit failed — AMBIGUOUS, never auto-redeliver.
			res.Message = strings.TrimSpace(res.Message + "; durable_write_failed")
			res.ErrorClass = ErrorClassTransport
			out := *item
			out.State = StateAmbiguous
			out.Result = &res
			d.queue.UpdateState(item.Intervention.InterventionID, StateAmbiguous, &res)
			// Best-effort record ambiguous so restart suppresses redelivery (same bound key).
			_ = d.ledger.RecordResultWithSource(StateDelivering, sessionID, res, StateAmbiguous, "", "", "", fp)
			if err == nil {
				err = fmt.Errorf("%w: %v", ErrDurableWriteFailed, lerr)
			}
			return &out, res, err
		}
	}
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

// Acknowledge is the legacy API. It cannot mint explicit ACK (#200).
// Prefer AcknowledgeSource with a closed AcknowledgeRequest.
func (d *AdvisoryDelivery) Acknowledge(interventionID, status string) error {
	if status == AckStatusAcked {
		return ErrBareAcknowledgeExplicit
	}
	return d.AcknowledgeSource(AcknowledgeRequest{
		SchemaVersion:  AcknowledgeRequestSchemaV1,
		InterventionID: interventionID,
		SourceKind:     "legacy_bare",
		SourceEventID:  "legacy:" + interventionID + ":" + status,
		Status:         status,
		AckLayer:       ACKLayerNone,
	})
}

// AcknowledgeSource records a source-bound ACK/reject/timeout (#200).
// Grok profiles cannot request AckLayer=explicit.
func (d *AdvisoryDelivery) AcknowledgeSource(req AcknowledgeRequest) error {
	if req.SchemaVersion == "" {
		req.SchemaVersion = AcknowledgeRequestSchemaV1
	}
	if err := ValidateAcknowledgeRequest(req); err != nil {
		return err
	}
	item, ok := d.queue.Get(req.InterventionID)
	if !ok {
		return fmt.Errorf("unknown intervention %q", req.InterventionID)
	}
	if !awaitingExplicitACK(item.State) {
		return fmt.Errorf("intervention %q not awaiting ack (state=%s; only DELIVERING/TRANSPORT_ACCEPTED/SESSION_VISIBLE accepted)", req.InterventionID, item.State)
	}
	// Target session pin when delivery already recorded one.
	if item.Result != nil && item.Result.TargetSessionID != "" && req.TargetSession != "" &&
		item.Result.TargetSessionID != req.TargetSession {
		return fmt.Errorf("target session mismatch")
	}

	now := time.Now().UTC()
	if !req.ObservedAt.IsZero() {
		now = req.ObservedAt.UTC()
	}
	res := InterventionResult{
		InterventionID:  req.InterventionID,
		DeliveryMode:    DefaultDeliveryMode(item.Intervention.ActionType),
		DeliveredAt:     now,
		ErrorClass:      ErrorClassNone,
		HostFamily:      req.HostFamily,
		HostVersion:     req.HostVersion,
		Profile:         req.Profile,
		TargetSessionID: req.TargetSession,
	}
	if item.Result != nil {
		// Preserve host delivery pins; overlay ACK fields.
		prev := *item.Result
		res = prev
		res.HostFamily = pickNonEmpty(req.HostFamily, prev.HostFamily)
		res.HostVersion = pickNonEmpty(req.HostVersion, prev.HostVersion)
		res.Profile = pickNonEmpty(req.Profile, prev.Profile)
		res.TargetSessionID = pickNonEmpty(req.TargetSession, prev.TargetSessionID)
	}

	layer := req.AckLayer
	if layer == "" && req.Status == AckStatusAcked {
		layer = ACKLayerSessionVisible
	}
	// Cap layer to profile max.
	max := ProfileMaxACKLayer(res.HostFamily, res.Profile)
	if layer == ACKLayerExplicit && max != ACKLayerExplicit {
		return ErrExplicitACKNotSupported
	}
	if layerRank(layer) > layerRank(max) {
		layer = max
	}

	var state DeliveryState
	switch req.Status {
	case AckStatusAcked:
		res.Accepted = true
		res.AckStatus = AckStatusAcked
		res.AckLayer = layer
		switch layer {
		case ACKLayerExplicit:
			state = StateExplicitACK
		case ACKLayerSessionVisible:
			state = StateSessionVisible
		case ACKLayerTransport:
			state = StateTransportAccepted
		default:
			state = StateAcked
		}
		ackAt := now
		res.AckAt = &ackAt
		res.Message = fmt.Sprintf("source_bound ack kind=%s event=%s layer=%s", req.SourceKind, req.SourceEventID, layer)
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
		return fmt.Errorf("invalid ack status %q", req.Status)
	}

	// Durable first, then memory — avoid advanced in-memory state without log (#200).
	if d.ledger != nil {
		fp := item.Intervention.Fingerprint
		if err := d.ledger.RecordResultWithSource(item.State, item.Intervention.SessionID, res, state,
			req.SourceKind, req.SourceEventID, req.CorrelationID, fp); err != nil {
			return fmt.Errorf("%w: %v", ErrDurableWriteFailed, err)
		}
	}
	d.queue.UpdateState(req.InterventionID, state, &res)
	return nil
}

func layerRank(layer string) int {
	switch layer {
	case ACKLayerNone:
		return 0
	case ACKLayerTransport:
		return 1
	case ACKLayerSessionVisible:
		return 2
	case ACKLayerBehavioral:
		return 3
	case ACKLayerExplicit:
		return 4
	default:
		return 0
	}
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
