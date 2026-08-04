package challenge

import (
	"fmt"
	"sync"
	"time"
)

// Store is an append-only, concurrent-safe in-memory challenge event log + snapshot.
// Event replay reconstructs identical ChallengeRecord state.
type Store struct {
	mu     sync.Mutex
	seq    int64
	events []ChallengeEvent
	byID   map[string]*ChallengeRecord
	// justifications keyed by challenge id (last accepted)
	justifications map[string]Justification
	// openByFingerprint session|fingerprint → challenge id (active only)
	openByFP map[string]string
}

// NewStore creates an empty store.
func NewStore() *Store {
	return &Store{
		byID:           make(map[string]*ChallengeRecord),
		justifications: make(map[string]Justification),
		openByFP:       make(map[string]string),
	}
}

func (s *Store) nextSeqLocked() int64 {
	s.seq++
	return s.seq
}

// Sequence returns the current max sequence (for expiry checks).
func (s *Store) Sequence() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

// Get returns a copy of the challenge record.
func (s *Store) Get(id string) (ChallengeRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[id]
	if !ok {
		return ChallengeRecord{}, false
	}
	return cloneRecord(*rec), true
}

// GetJustification returns the last accepted justification.
func (s *Store) GetJustification(id string) (Justification, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.justifications[id]
	return j, ok
}

// Events returns a copy of all events for a challenge (ordered by sequence).
func (s *Store) Events(challengeID string) []ChallengeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []ChallengeEvent
	for _, e := range s.events {
		if e.ChallengeID == challengeID {
			out = append(out, e)
		}
	}
	return out
}

// AllEvents returns a full append-only log copy.
func (s *Store) AllEvents() []ChallengeEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]ChallengeEvent, len(s.events))
	copy(out, s.events)
	return out
}

// ActiveByFingerprint finds an open/justified/retry_pending challenge for session+fp.
func (s *Store) ActiveByFingerprint(sessionID, fp string) (ChallengeRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := sessionID + "|" + fp
	id, ok := s.openByFP[key]
	if !ok {
		return ChallengeRecord{}, false
	}
	rec, ok := s.byID[id]
	if !ok {
		return ChallengeRecord{}, false
	}
	switch rec.State {
	case StateOpen, StateJustified, StateRetryPending:
		return cloneRecord(*rec), true
	default:
		return ChallengeRecord{}, false
	}
}

// putLocked appends event and updates snapshot. Caller holds mu.
func (s *Store) putLocked(rec ChallengeRecord, ev ChallengeEvent, just *Justification) {
	s.events = append(s.events, ev)
	cp := cloneRecord(rec)
	s.byID[rec.ChallengeID] = &cp
	if just != nil {
		s.justifications[rec.ChallengeID] = *just
	}
	key := rec.SessionID + "|" + rec.ActionFingerprint
	switch rec.State {
	case StateOpen, StateJustified, StateRetryPending:
		// Hard single-active invariant: expire any other non-terminal with same key.
		// Collect IDs first — never mutate byID while ranging over it.
		var toExpire []string
		for id, existing := range s.byID {
			if id == rec.ChallengeID {
				continue
			}
			if existing.SessionID != rec.SessionID || existing.ActionFingerprint != rec.ActionFingerprint {
				continue
			}
			if isTerminal(existing.State) {
				continue
			}
			toExpire = append(toExpire, id)
		}
		for _, id := range toExpire {
			existing := s.byID[id]
			if existing == nil || isTerminal(existing.State) {
				continue
			}
			exp := cloneRecord(*existing)
			exp.State = StateExpired
			exp.UpdatedSequence = rec.UpdatedSequence
			exp.UpdatedAt = rec.UpdatedAt
			s.byID[id] = &exp
		}
		s.openByFP[key] = rec.ChallengeID
	default:
		if s.openByFP[key] == rec.ChallengeID {
			delete(s.openByFP, key)
		}
	}
}

// Replay rebuilds a ChallengeRecord from append-only events only (ignores snapshot).
// Expired challenges cannot be revived: EXPIRED is terminal even if later events exist
// with lower sequence (we process in order; expiry is terminal).
func Replay(events []ChallengeEvent, seed ChallengeRecord) (ChallengeRecord, error) {
	rec := seed
	// If seed empty, require opened event.
	applied := false
	for _, ev := range events {
		if seed.ChallengeID != "" && ev.ChallengeID != seed.ChallengeID {
			continue
		}
		if !applied && rec.ChallengeID == "" {
			rec.ChallengeID = ev.ChallengeID
			rec.SessionID = ev.SessionID
			rec.SchemaVersion = SchemaChallengeRecord
		}
		// Terminal states cannot transition except no-op.
		if isTerminal(rec.State) && ev.ToState != rec.State {
			// Ignore revival attempts after EXPIRED/REJECTED/etc.
			if rec.State == StateExpired {
				continue
			}
			// Allow only if from matches current (idempotent)
			if ev.FromState != rec.State {
				continue
			}
		}
		if err := validateTransition(rec.State, ev.ToState, ev.Type); err != nil && applied {
			// First event may set OPEN from empty.
			if rec.State != "" {
				return ChallengeRecord{}, fmt.Errorf("replay: %w", err)
			}
		}
		if rec.State == "" && ev.ToState == StateOpen {
			rec.State = StateOpen
			rec.RetryBudget = InitialRetryBudget
			rec.RetryBudgetInitial = InitialRetryBudget
			rec.Stage2Decision = DecisionBlock
			rec.CreatedSequence = ev.Sequence
			rec.CreatedAt = ev.At
			applied = true
		} else {
			rec.State = ev.ToState
			applied = true
		}
		rec.UpdatedSequence = ev.Sequence
		rec.UpdatedAt = ev.At
		if ev.Type == "budget_consumed" {
			if rec.RetryBudget > 0 {
				rec.RetryBudget--
			}
		}
	}
	if !applied {
		return ChallengeRecord{}, fmt.Errorf("replay: no events")
	}
	return rec, nil
}

// ReplayFromStore rebuilds from store events for challengeID using stored snapshots
// of immutable fields from first open event's companion record if available.
func (s *Store) ReplayFromStore(challengeID string) (ChallengeRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var evs []ChallengeEvent
	for _, e := range s.events {
		if e.ChallengeID == challengeID {
			evs = append(evs, e)
		}
	}
	if len(evs) == 0 {
		return ChallengeRecord{}, fmt.Errorf("replay: unknown challenge %s", challengeID)
	}
	// Prefer snapshot for immutable metadata; re-apply transitions for state/budget.
	snap, ok := s.byID[challengeID]
	seed := ChallengeRecord{
		SchemaVersion: SchemaChallengeRecord,
		ChallengeID:   challengeID,
		SessionID:     evs[0].SessionID,
	}
	if ok {
		seed = *snap
		// reset mutable fields for pure replay
		seed.State = ""
		seed.JustificationHash = ""
		seed.UpdatedSequence = 0
	}
	// Pure transition replay
	rec := seed
	rec.State = ""
	// Budget initial from snapshot when present (human-review opens with 0).
	if ok {
		rec.RetryBudgetInitial = snap.RetryBudgetInitial
		rec.RetryBudget = snap.RetryBudgetInitial
	} else {
		rec.RetryBudgetInitial = InitialRetryBudget
		rec.RetryBudget = InitialRetryBudget
	}
	for _, ev := range evs {
		// Seed immutable metadata from snapshot once at open.
		if ev.Type == "opened" && ok {
			rec.ActionFingerprint = snap.ActionFingerprint
			rec.OriginalActionID = snap.OriginalActionID
			rec.BlockClass = snap.BlockClass
			rec.ReasonCode = snap.ReasonCode
			rec.Appealability = snap.Appealability
			rec.PolicyClass = snap.PolicyClass
			rec.RequiredClaims = append([]string(nil), snap.RequiredClaims...)
			rec.SideEffectClass = snap.SideEffectClass
			rec.TargetResources = append([]string(nil), snap.TargetResources...)
			rec.WorkspaceRevision = snap.WorkspaceRevision
			rec.ContractRevision = snap.ContractRevision
			rec.Branch = snap.Branch
			rec.PolicyVersion = snap.PolicyVersion
			rec.RulesetHash = snap.RulesetHash
			rec.PolicyHash = snap.PolicyHash
			rec.ExpiresAtSequence = snap.ExpiresAtSequence
			rec.RetryBudgetInitial = snap.RetryBudgetInitial
			rec.RetryBudget = snap.RetryBudgetInitial
			rec.Intervention = snap.Intervention
			if rec.Intervention == "" && snap.Appealability == AppealHumanReview {
				rec.Intervention = InterventionHumanReview
			}
			if rec.Intervention == "" {
				rec.Intervention = InterventionAppealableChallenge
			}
		}
		if ev.Type == "justified" && ok {
			rec.JustificationHash = snap.JustificationHash
		}
		var err error
		rec, err = ApplyEvent(rec, ev)
		if err != nil {
			return ChallengeRecord{}, err
		}
		// Persist consumed retry identity from terminal event payload or snapshot.
		if (ev.Type == "allowed_once" || ev.Type == "rejected" || ev.Type == "human_review") && ev.PayloadHash != "" {
			rec.ConsumedRetryKey = ev.PayloadHash
		}
		if ok && snap.ConsumedRetryKey != "" && rec.ConsumedRetryKey == "" && isTerminal(rec.State) {
			rec.ConsumedRetryKey = snap.ConsumedRetryKey
		}
		if ok && snap.OperationDigest != "" {
			rec.OperationDigest = snap.OperationDigest
		}
	}
	return rec, nil
}

func cloneRecord(r ChallengeRecord) ChallengeRecord {
	cp := r
	if r.RequiredClaims != nil {
		cp.RequiredClaims = append([]string(nil), r.RequiredClaims...)
	}
	if r.TargetResources != nil {
		cp.TargetResources = append([]string(nil), r.TargetResources...)
	}
	return cp
}

func isTerminal(st ChallengeState) bool {
	switch st {
	case StateAllowedOnce, StateRejected, StateHumanReview, StateAbandoned, StateExpired:
		return true
	default:
		return false
	}
}

// validateTransition checks allowed edges.
func validateTransition(from, to ChallengeState, evType string) error {
	if from == "" && to == StateOpen {
		return nil
	}
	allowed := map[ChallengeState]map[ChallengeState]bool{
		StateOpen: {
			StateJustified:   true,
			StateRejected:    true,
			StateAbandoned:   true,
			StateExpired:     true,
			StateHumanReview: true,
		},
		StateJustified: {
			StateRetryPending: true,
			StateAbandoned:    true,
			StateExpired:      true,
			StateRejected:     true,
			StateHumanReview:  true,
		},
		StateRetryPending: {
			StateAllowedOnce: true,
			StateRejected:    true,
			StateHumanReview: true,
		},
	}
	if m, ok := allowed[from]; ok && m[to] {
		return nil
	}
	// Idempotent self-transition
	if from == to && from != "" {
		return nil
	}
	return fmt.Errorf("invalid transition %s -> %s (event %s)", from, to, evType)
}

// appendEvent is a helper used by Service under lock via store methods.
func (s *Store) appendTransition(rec ChallengeRecord, from ChallengeState, to ChallengeState, typ, corr, cause, payloadHash, note string, at time.Time, just *Justification) ChallengeRecord {
	seq := s.nextSeqLocked()
	rec.State = to
	rec.UpdatedSequence = seq
	rec.UpdatedAt = at
	ev := ChallengeEvent{
		SchemaVersion: SchemaChallengeEvent,
		Sequence:      seq,
		ChallengeID:   rec.ChallengeID,
		SessionID:     rec.SessionID,
		Type:          typ,
		FromState:     from,
		ToState:       to,
		CorrelationID: corr,
		CausationID:   cause,
		PayloadHash:   payloadHash,
		Note:          note,
		At:            at,
	}
	s.putLocked(rec, ev, just)
	return rec
}
