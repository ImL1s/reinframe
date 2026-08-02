package adapter

import (
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// PendingItem is one queued intervention with lifecycle metadata.
type PendingItem struct {
	Intervention protocol.Intervention
	State        DeliveryState
	EnqueuedAt   time.Time
	ExpiresAt    time.Time
	// Result is set after a delivery attempt (or synthetic unsupported/expiry).
	Result *InterventionResult
}

// PendingQueue is an in-memory per-session pending intervention queue with
// InterventionID dedupe and TTL expiry. Safe for concurrent use.
type PendingQueue struct {
	mu      sync.Mutex
	byID    map[string]*PendingItem
	bySess  map[string][]string // sessionID → ordered intervention IDs
	nowFunc func() time.Time
}

// NewPendingQueue creates an empty pending queue.
func NewPendingQueue() *PendingQueue {
	return &PendingQueue{
		byID:    make(map[string]*PendingItem),
		bySess:  make(map[string][]string),
		nowFunc: func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the time source (tests only).
func (q *PendingQueue) SetClock(now func() time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if now != nil {
		q.nowFunc = now
	}
}

// EnqueueResult describes the outcome of Enqueue.
type EnqueueResult struct {
	Item       *PendingItem
	Suppressed bool
	Expired    bool
}

// Enqueue adds an intervention for a session.
// Duplicate InterventionID (already known) is marked SUPPRESSED and not re-queued.
// Items whose ExpiresAt is already past are marked EXPIRED and not delivered later.
func (q *PendingQueue) Enqueue(intervention protocol.Intervention, ttl time.Duration) EnqueueResult {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.nowFunc()
	if existing, ok := q.byID[intervention.InterventionID]; ok {
		// Duplicate: do not re-deliver. Surface a suppressed clone view.
		suppressed := *existing
		suppressed.State = StateSuppressed
		return EnqueueResult{Item: &suppressed, Suppressed: true}
	}

	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	expires := now.Add(ttl)
	item := &PendingItem{
		Intervention: intervention,
		State:        StatePending,
		EnqueuedAt:   now,
		ExpiresAt:    expires,
	}

	if !expires.After(now) {
		item.State = StateExpired
		q.byID[intervention.InterventionID] = item
		// Not added to session delivery order.
		return EnqueueResult{Item: item, Expired: true}
	}

	q.byID[intervention.InterventionID] = item
	sid := intervention.SessionID
	q.bySess[sid] = append(q.bySess[sid], intervention.InterventionID)
	return EnqueueResult{Item: item}
}

// Get returns the item for an intervention ID, if present.
func (q *PendingQueue) Get(interventionID string) (*PendingItem, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.byID[interventionID]
	if !ok {
		return nil, false
	}
	// Return a shallow copy of the header to avoid external mutation of maps.
	cp := *item
	return &cp, true
}

// NextPending returns the next PENDING, non-expired item for a session and
// transitions it to DELIVERING. Expired PENDING items become EXPIRED.
// Returns nil when nothing is deliverable.
func (q *PendingQueue) NextPending(sessionID string) *PendingItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	now := q.nowFunc()
	ids := q.bySess[sessionID]
	for i, id := range ids {
		item := q.byID[id]
		if item == nil {
			continue
		}
		if item.State != StatePending {
			continue
		}
		if !item.ExpiresAt.After(now) {
			item.State = StateExpired
			continue
		}
		item.State = StateDelivering
		// Remove from front portion by slicing past this index for session order;
		// keep trailing IDs (including non-pending) for lookup stability.
		q.bySess[sessionID] = append(ids[:i], ids[i+1:]...)
		cp := *item
		return &cp
	}
	return nil
}

// UpdateState sets the lifecycle state (and optional result) for an intervention.
func (q *PendingQueue) UpdateState(interventionID string, state DeliveryState, result *InterventionResult) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	item, ok := q.byID[interventionID]
	if !ok {
		return false
	}
	item.State = state
	if result != nil {
		item.Result = result
	}
	return true
}

// PendingAdvisoryID returns the InterventionID of the first PENDING or DELIVERING
// advisory for the session, if any (for HookPolicy.PendingAdvisoryInterventionID).
func (q *PendingQueue) PendingAdvisoryID(sessionID string) string {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.nowFunc()
	for _, id := range q.bySess[sessionID] {
		item := q.byID[id]
		if item == nil {
			continue
		}
		if item.State == StatePending || item.State == StateDelivering {
			if item.State == StatePending && !item.ExpiresAt.After(now) {
				item.State = StateExpired
				continue
			}
			return item.Intervention.InterventionID
		}
	}
	// Also scan byID for DELIVERING items removed from session order.
	for _, item := range q.byID {
		if item.Intervention.SessionID != sessionID {
			continue
		}
		if item.State == StateDelivering {
			return item.Intervention.InterventionID
		}
	}
	return ""
}
