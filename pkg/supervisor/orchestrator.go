package supervisor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/state"
)

// AuditEntry is a lightweight in-memory audit record for the M2.0 slice.
type AuditEntry struct {
	At       time.Time
	Session  string
	Category string
	Summary  string
	RefID    string
}

// Config configures Orchestrator.
type Config struct {
	Detector *detector.RepeatedFailureDetector
	Policy   *policy.Engine
	Delivery *adapter.AdvisoryDelivery
	// Store is optional; when non-nil, HandleEvent appends events.
	Store *state.Store
	// BaseHookPolicy is the static deterministic policy (deny lists, scope, …).
	// Pending advisory latch is managed by the orchestrator.
	BaseHookPolicy adapter.HookPolicy
	// InterventionTTL for enqueue (default delivery TTL when <=0).
	InterventionTTL time.Duration
}

// Orchestrator is the composition root for detect → policy → queue → deliver → ACK.
type Orchestrator struct {
	det   *detector.RepeatedFailureDetector
	pol   *policy.Engine
	del   *adapter.AdvisoryDelivery
	store *state.Store
	ttl   time.Duration

	mu           sync.Mutex
	basePolicy   adapter.HookPolicy
	pendingLatch map[string]string // sessionID → interventionID
	audit        []AuditEntry
}

// NewOrchestrator validates config and builds an Orchestrator.
func NewOrchestrator(cfg Config) (*Orchestrator, error) {
	if cfg.Detector == nil {
		return nil, fmt.Errorf("detector is required")
	}
	if cfg.Policy == nil {
		return nil, fmt.Errorf("policy is required")
	}
	if cfg.Delivery == nil {
		return nil, fmt.Errorf("delivery is required")
	}
	return &Orchestrator{
		det:          cfg.Detector,
		pol:          cfg.Policy,
		del:          cfg.Delivery,
		store:        cfg.Store,
		ttl:          cfg.InterventionTTL,
		basePolicy:   cfg.BaseHookPolicy,
		pendingLatch: make(map[string]string),
	}, nil
}

// HandleEvent ingests one agent event: optional store append, detect, slow policy, enqueue.
// Returns the signal (if any) and intervention enqueued (if any).
func (o *Orchestrator) HandleEvent(ctx context.Context, event protocol.AgentEvent) (*protocol.TunnelSignal, *protocol.Intervention, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if o.store != nil {
		if err := o.store.AppendEvent(ctx, &event); err != nil {
			return nil, nil, fmt.Errorf("store append: %w", err)
		}
	}

	sig, ok := o.det.Observe(event)
	if !ok || sig == nil {
		return nil, nil, nil
	}
	o.recordAudit(event.SessionID, "detector", "repeated_failure_signal", sig.SignalID)

	slow, err := o.pol.EvaluateSlow(ctx, policy.SlowInput{Signal: sig})
	if err != nil {
		return sig, nil, err
	}
	if slow.Intervention == nil {
		o.recordAudit(event.SessionID, "policy", "no_intervention:"+slow.Reason, sig.SignalID)
		return sig, nil, nil
	}

	enq := o.del.Enqueue(*slow.Intervention, o.ttl)
	if enq.Suppressed || enq.Expired {
		o.recordAudit(event.SessionID, "queue", fmt.Sprintf("enqueue suppressed=%v expired=%v", enq.Suppressed, enq.Expired), slow.Intervention.InterventionID)
		return sig, slow.Intervention, nil
	}

	o.mu.Lock()
	o.pendingLatch[event.SessionID] = slow.Intervention.InterventionID
	o.mu.Unlock()
	o.recordAudit(event.SessionID, "policy", "enqueued_zoom_out", slow.Intervention.InterventionID)
	return sig, slow.Intervention, nil
}

// EvaluatePreTool runs the fast path with the session's pending-advisory latch applied.
func (o *Orchestrator) EvaluatePreTool(ctx context.Context, req adapter.HookRequest) adapter.HookDecision {
	pol := o.hookPolicyFor(req.SessionID)
	return o.pol.EvaluateFast(ctx, policy.FastInput{Request: req, Policy: pol})
}

// DeliverAtSafeBoundary delivers the next pending advisory for the session.
// If delivery reaches ACKED (e.g. AutoAck actuator), the latch is cleared.
// If delivery stays DELIVERING, latch remains until Acknowledge.
func (o *Orchestrator) DeliverAtSafeBoundary(ctx context.Context, sessionID string) (*adapter.PendingItem, adapter.InterventionResult, error) {
	item, res, err := o.del.DeliverPending(ctx, sessionID)
	if item != nil {
		o.recordAudit(sessionID, "delivery", fmt.Sprintf("state=%s ack=%s", item.State, res.AckStatus), item.Intervention.InterventionID)
		if item.State == adapter.StateAcked {
			o.clearLatch(sessionID, item.Intervention.InterventionID)
		}
		// Human escalation (observe-only) clears latch so hooks are not stuck forever.
		if item.State == adapter.StateFailed && res.DeliveryMode == adapter.DeliveryModeHumanEscalation && err == nil {
			o.clearLatch(sessionID, item.Intervention.InterventionID)
		}
	}
	return item, res, err
}

// Acknowledge records agent ACK/REJECT/TIMEOUT and clears the pending latch on acked.
func (o *Orchestrator) Acknowledge(interventionID, status string) error {
	if err := o.del.Acknowledge(interventionID, status); err != nil {
		return err
	}
	item, ok := o.del.Get(interventionID)
	if ok {
		o.recordAudit(item.Intervention.SessionID, "ack", status, interventionID)
		if status == adapter.AckStatusAcked {
			o.clearLatch(item.Intervention.SessionID, interventionID)
		}
	}
	return nil
}

// PendingInterventionID returns the latched intervention id for a session, if any.
func (o *Orchestrator) PendingInterventionID(sessionID string) string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.pendingLatch[sessionID]
}

// AuditSnapshot returns a copy of in-memory audit entries.
func (o *Orchestrator) AuditSnapshot() []AuditEntry {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]AuditEntry, len(o.audit))
	copy(out, o.audit)
	return out
}

// Delivery exposes the underlying AdvisoryDelivery (tests).
func (o *Orchestrator) Delivery() *adapter.AdvisoryDelivery {
	return o.del
}

func (o *Orchestrator) hookPolicyFor(sessionID string) adapter.HookPolicy {
	o.mu.Lock()
	defer o.mu.Unlock()
	p := o.basePolicy
	if id, ok := o.pendingLatch[sessionID]; ok && id != "" {
		p.PendingAdvisoryInterventionID = id
	}
	return p
}

func (o *Orchestrator) clearLatch(sessionID, interventionID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if cur, ok := o.pendingLatch[sessionID]; ok && (interventionID == "" || cur == interventionID) {
		delete(o.pendingLatch, sessionID)
	}
}

func (o *Orchestrator) recordAudit(session, category, summary, ref string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.audit = append(o.audit, AuditEntry{
		At:       time.Now().UTC(),
		Session:  session,
		Category: category,
		Summary:  summary,
		RefID:    ref,
	})
}
