package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

// Service is the host-neutral challenge workflow API (#131).
type Service struct {
	store  *Store
	reeval ReEvaluator
	now    func() time.Time
	// idMu serializes Open id generation only; store has its own lock.
	idSeq uint64
	idMu  sync.Mutex
	// terminalCh maps challenge ID → closed when the challenge reaches a terminal state.
	// Concurrent AttemptRetry waiters block on this instead of a fixed poll budget.
	termMu     sync.Mutex
	terminalCh map[string]chan struct{}
}

// ServiceConfig configures Service.
type ServiceConfig struct {
	Store  *Store
	ReEval ReEvaluator
	Now    func() time.Time
}

// NewService builds a challenge service.
func NewService(cfg ServiceConfig) *Service {
	st := cfg.Store
	if st == nil {
		st = NewStore()
	}
	re := cfg.ReEval
	if re == nil {
		re = DefaultReEvaluator{}
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{
		store:      st,
		reeval:     re,
		now:        now,
		terminalCh: make(map[string]chan struct{}),
	}
}

// Store exposes the underlying store for tests/replay.
func (s *Service) Store() *Store { return s.store }

// Open creates a durable challenge after a BLOCK, or returns non-appealable/human-review
// intervention without a challenge when class forbids self-appeal.
// Public Stage2 decision remains BLOCK for challenge open path.
func (s *Service) Open(ctx context.Context, req OpenRequest) (ChallengeRecord, error) {
	if ctx != nil && ctx.Err() != nil {
		return ChallengeRecord{}, ctx.Err()
	}
	if strings.TrimSpace(req.SessionID) == "" {
		req.SessionID = req.Proposed.SessionID
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return ChallengeRecord{}, fmt.Errorf("challenge open: session_id required")
	}
	if err := ValidateProposedForChallenge(req.Proposed); err != nil {
		return ChallengeRecord{}, fmt.Errorf("challenge open: %w", err)
	}
	req.PolicyClass = NormalizePolicyClass(req.PolicyClass)
	req.BlockClass = NormalizeBlockClass(req.BlockClass)
	if req.BlockClass == "" {
		req.BlockClass = BlockClassProductivityGeneric
	}
	if req.ReasonCode == "" {
		req.ReasonCode = "BLOCK"
	}
	if req.PolicyClass == PolicyClassSecurity {
		if !IsHardDenyClass(req.BlockClass) && !IsIrreversibleClass(req.BlockClass) {
			req.BlockClass = BlockClassUnknownSecurity
		}
	}

	claims := req.RequiredClaims
	if len(claims) == 0 {
		claims = DefaultRequiredClaims(req.BlockClass)
	}
	var err error
	claims, err = ValidateRequiredClaims(claims)
	if err != nil {
		return ChallengeRecord{}, fmt.Errorf("challenge open: %w", err)
	}

	appeal, _ := ClassifyAppealability(req.BlockClass, req.Proposed)
	fp, err := ComputeFingerprint(FingerprintInput{
		Proposed:          req.Proposed,
		SessionID:         req.SessionID,
		Branch:            req.Branch,
		WorkspaceRevision: req.Proposed.WorkspaceRevision,
		ContractRevision:  req.Proposed.ContractRevision,
	})
	if err != nil {
		return ChallengeRecord{}, fmt.Errorf("challenge open: %w", err)
	}
	policyHash := hashPolicy(req.PolicyVersion, req.RulesetHash, req.BlockClass)

	// Non-appealable: no durable appeal challenge.
	if appeal == AppealNonAppealable {
		now := s.now()
		rec := ChallengeRecord{
			SchemaVersion:     SchemaChallengeRecord,
			SessionID:         req.SessionID,
			ActionFingerprint: fp.Fingerprint,
			OriginalActionID:  req.Proposed.ActionID,
			PolicyClass:       req.PolicyClass,
			BlockClass:        req.BlockClass,
			ReasonCode:        req.ReasonCode,
			State:             StateRejected,
			Appealability:     AppealNonAppealable,
			Intervention:      InterventionNone,
			Stage2Decision:    DecisionBlock,
			SideEffectClass:   fp.SideEffectClass,
			TargetResources:   fp.TargetResources,
			OperationDigest:   fp.OperationDigest,
			WorkspaceRevision: req.Proposed.WorkspaceRevision,
			ContractRevision:  req.Proposed.ContractRevision,
			Branch:            req.Branch,
			PolicyVersion:     req.PolicyVersion,
			RulesetHash:       req.RulesetHash,
			PolicyHash:        policyHash,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		return rec, fmt.Errorf("challenge open: non-appealable block class %s", req.BlockClass)
	}
	if appeal == AppealHumanReview {
		now := s.now()
		rec := ChallengeRecord{
			SchemaVersion:     SchemaChallengeRecord,
			ChallengeID:       s.newID("hr"),
			SessionID:         req.SessionID,
			ActionFingerprint: fp.Fingerprint,
			OriginalActionID:  req.Proposed.ActionID,
			PolicyClass:       req.PolicyClass,
			BlockClass:        req.BlockClass,
			ReasonCode:        req.ReasonCode,
			RequiredClaims:    append([]string(nil), claims...),
			State:             StateHumanReview,
			Appealability:     AppealHumanReview,
			Intervention:      InterventionHumanReview,
			Stage2Decision:    DecisionBlock,
			SideEffectClass:   fp.SideEffectClass,
			TargetResources:   fp.TargetResources,
			OperationDigest:   fp.OperationDigest,
			WorkspaceRevision: req.Proposed.WorkspaceRevision,
			ContractRevision:  req.Proposed.ContractRevision,
			Branch:            req.Branch,
			PolicyVersion:     req.PolicyVersion,
			RulesetHash:       req.RulesetHash,
			PolicyHash:        policyHash,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		s.store.mu.Lock()
		rec = s.store.appendTransition(rec, "", StateHumanReview, "human_review", req.CorrelationID, req.Proposed.ActionID, fp.Fingerprint, "open_human_review", now, nil)
		s.store.mu.Unlock()
		s.signalTerminal(rec.ChallengeID)
		return rec, nil
	}

	// Active reuse only when correctness-relevant identity matches.
	if existing, ok := s.store.ActiveByFingerprint(req.SessionID, fp.Fingerprint); ok {
		if policyIdentityMatch(existing, req, claims, policyHash) {
			return existing, nil
		}
		// Supersede stale challenge — expire so it cannot be revived.
		s.store.mu.Lock()
		if cur, ok := s.store.byID[existing.ChallengeID]; ok && !isTerminal(cur.State) {
			now := s.now()
			_ = s.store.appendTransition(cloneRecord(*cur), cur.State, StateExpired, "expired", req.CorrelationID, existing.ChallengeID, fp.Fingerprint, "superseded_by_policy_change", now, nil)
			s.store.mu.Unlock()
			s.signalTerminal(existing.ChallengeID)
		} else {
			s.store.mu.Unlock()
		}
	}

	now := s.now()
	id := s.newID("ch")
	rec := ChallengeRecord{
		SchemaVersion:      SchemaChallengeRecord,
		ChallengeID:        id,
		SessionID:          req.SessionID,
		ActionFingerprint:  fp.Fingerprint,
		OriginalActionID:   req.Proposed.ActionID,
		PolicyClass:        req.PolicyClass,
		BlockClass:         req.BlockClass,
		ReasonCode:         req.ReasonCode,
		RequiredClaims:     append([]string(nil), claims...),
		RetryBudget:        InitialRetryBudget,
		RetryBudgetInitial: InitialRetryBudget,
		State:              StateOpen,
		Appealability:      AppealAppealable,
		Intervention:       InterventionAppealableChallenge,
		Stage2Decision:     DecisionBlock,
		SideEffectClass:    fp.SideEffectClass,
		TargetResources:    fp.TargetResources,
		OperationDigest:    fp.OperationDigest,
		WorkspaceRevision:  req.Proposed.WorkspaceRevision,
		ContractRevision:   req.Proposed.ContractRevision,
		Branch:             req.Branch,
		PolicyVersion:      req.PolicyVersion,
		RulesetHash:        req.RulesetHash,
		PolicyHash:         policyHash,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	key := req.SessionID + "|" + fp.Fingerprint
	if id2, ok := s.store.openByFP[key]; ok {
		if r, ok := s.store.byID[id2]; ok {
			if policyIdentityMatch(*r, req, claims, policyHash) {
				return cloneRecord(*r), nil
			}
		}
	}
	seq := s.store.nextSeqLocked()
	rec.CreatedSequence = seq
	rec.UpdatedSequence = seq
	if req.ExpiresAfterSequences > 0 {
		rec.ExpiresAtSequence = seq + req.ExpiresAfterSequences
	}
	ev := ChallengeEvent{
		SchemaVersion: SchemaChallengeEvent,
		Sequence:      seq,
		ChallengeID:   rec.ChallengeID,
		SessionID:     rec.SessionID,
		Type:          "opened",
		FromState:     "",
		ToState:       StateOpen,
		CorrelationID: req.CorrelationID,
		CausationID:   req.Proposed.ActionID,
		PayloadHash:   fp.Fingerprint,
		Note:          "open_appealable",
		At:            now,
	}
	s.store.putLocked(rec, ev, nil)
	return cloneRecord(rec), nil
}

// Justify accepts a closed justification for an OPEN challenge.
// Does not auto-ALLOW.
func (s *Service) Justify(ctx context.Context, j Justification, knownEvidence []string) (ChallengeRecord, error) {
	if ctx != nil && ctx.Err() != nil {
		return ChallengeRecord{}, ctx.Err()
	}
	rec, ok := s.store.Get(j.ChallengeID)
	if !ok {
		return ChallengeRecord{}, fmt.Errorf("justify: unknown challenge %s", j.ChallengeID)
	}
	// Expiry check
	if err := s.expireIfNeeded(&rec); err != nil {
		return rec, err
	}
	if rec.State == StateExpired {
		return rec, fmt.Errorf("justify: challenge expired")
	}
	// Idempotent: already justified with same hash
	clean, err := ValidateJustification(j, knownEvidence, rec.RequiredClaims)
	if err != nil {
		return ChallengeRecord{}, err
	}
	if clean.ChallengeID != rec.ChallengeID {
		return ChallengeRecord{}, fmt.Errorf("justify: challenge_id mismatch")
	}
	jh := HashJustification(clean)

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	cur, ok := s.store.byID[rec.ChallengeID]
	if !ok {
		return ChallengeRecord{}, fmt.Errorf("justify: unknown challenge")
	}
	rec = cloneRecord(*cur)
	if rec.State == StateExpired {
		return rec, fmt.Errorf("justify: challenge expired")
	}
	if rec.State == StateJustified && rec.JustificationHash == jh {
		return rec, nil // idempotent
	}
	if rec.State != StateOpen {
		return rec, fmt.Errorf("justify: invalid state %s", rec.State)
	}
	from := rec.State
	rec.JustificationHash = jh
	now := s.now()
	rec = s.store.appendTransition(rec, from, StateJustified, "justified", clean.ChallengeID, rec.OriginalActionID, jh, "justification_accepted", now, &clean)
	return cloneRecord(rec), nil
}

// AttemptRetry consumes one retry budget atomically for a semantically equivalent action.
// Retry without justification is rejected. Concurrent duplicates yield one business outcome.
func (s *Service) AttemptRetry(ctx context.Context, req RetryRequest) (RetryResult, error) {
	if ctx != nil && ctx.Err() != nil {
		return RetryResult{}, ctx.Err()
	}
	if strings.TrimSpace(req.ChallengeID) == "" {
		return RetryResult{}, fmt.Errorf("retry: challenge_id required")
	}
	if err := ValidateProposedForChallenge(req.Proposed); err != nil {
		return RetryResult{Stage2Decision: DecisionBlock, RejectedReason: "lossy_proposed_action"}, fmt.Errorf("retry: %w", err)
	}

	rec, ok := s.store.Get(req.ChallengeID)
	if !ok {
		return RetryResult{}, fmt.Errorf("retry: unknown challenge")
	}
	if err := s.expireIfNeeded(&rec); err != nil {
		return RetryResult{Record: rec, Stage2Decision: DecisionBlock, RejectedReason: "expired"}, err
	}
	if err := checkOwnership(rec, req); err != nil {
		return RetryResult{
			Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
			RejectedReason: "ownership_mismatch",
		}, err
	}

	fpOwner, err := ComputeFingerprint(FingerprintInput{
		Proposed:          req.Proposed,
		SessionID:         rec.SessionID,
		Branch:            rec.Branch,
		WorkspaceRevision: firstNonEmpty(req.Proposed.WorkspaceRevision, rec.WorkspaceRevision),
		ContractRevision:  pickContract(req.Proposed.ContractRevision, rec.ContractRevision),
	})
	if err != nil {
		return RetryResult{Record: rec, Stage2Decision: DecisionBlock, RejectedReason: "fingerprint_error"}, err
	}
	origFP := FingerprintResult{
		Fingerprint:     rec.ActionFingerprint,
		SideEffectClass: rec.SideEffectClass,
		TargetResources: rec.TargetResources,
		OperationDigest: rec.OperationDigest,
	}
	rel := ClassifyRelationship(origFP, fpOwner)
	attemptKey := retryAttemptKey(rec.ChallengeID, req, fpOwner.Fingerprint)

	if isTargetSuperset(fpOwner.TargetResources, rec.TargetResources) {
		return RetryResult{
			Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
			Relationship: RelDifferent, RejectedReason: "scope_expansion",
		}, fmt.Errorf("retry: target scope expansion is not bound to challenge")
	}
	if rel == RelReducedScope || rel == RelDifferent {
		return RetryResult{
			Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
			Relationship: rel, RejectedReason: "not_same_semantic_action",
		}, fmt.Errorf("retry: action relationship %s is not bound to challenge", rel)
	}

	s.store.mu.Lock()
	cur, ok := s.store.byID[req.ChallengeID]
	if !ok {
		s.store.mu.Unlock()
		return RetryResult{}, fmt.Errorf("retry: unknown challenge")
	}
	rec = cloneRecord(*cur)

	if rec.State == StateExpired {
		s.store.mu.Unlock()
		return RetryResult{Record: rec, Stage2Decision: DecisionBlock, Relationship: rel, RejectedReason: "expired"}, fmt.Errorf("retry: expired")
	}

	// Terminal handling — true one-shot ALLOW: only exact retry identity may replay.
	if rec.State == StateAllowedOnce || rec.State == StateRejected || rec.State == StateHumanReview || rec.State == StateAbandoned {
		s.store.mu.Unlock()
		if rec.State == StateAllowedOnce {
			if rec.ConsumedRetryKey == "" {
				// Missing identity must not be unlimited replay.
				return RetryResult{
					Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
					Relationship: rel, RejectedReason: "already_consumed",
				}, fmt.Errorf("retry: already_consumed (missing retry identity)")
			}
			if attemptKey == rec.ConsumedRetryKey {
				return RetryResult{
					Record: rec, Stage2Decision: DecisionAllow, Intervention: rec.Intervention,
					Relationship: rel, IdempotentReplay: true, RejectedReason: "idempotent_replay",
				}, nil
			}
			return RetryResult{
				Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
				Relationship: rel, RejectedReason: "retry_budget_exhausted",
			}, fmt.Errorf("retry: retry_budget_exhausted / already_consumed")
		}
		// Other terminals: exact key may idempotently replay; new keys stay blocked.
		if rec.ConsumedRetryKey != "" && attemptKey == rec.ConsumedRetryKey {
			return RetryResult{
				Record: rec, Stage2Decision: rec.Stage2Decision, Intervention: rec.Intervention,
				Relationship: rel, IdempotentReplay: true, RejectedReason: "already_terminal",
			}, nil
		}
		return RetryResult{
			Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
			Relationship: rel, RejectedReason: "already_terminal",
		}, nil
	}

	if rec.State == StateOpen {
		from := rec.State
		now := s.now()
		rec = s.store.appendTransition(rec, from, StateRejected, "rejected", req.CorrelationID, req.Proposed.ActionID, fpOwner.Fingerprint, "retry_without_justification", now, nil)
		rec.Stage2Decision = DecisionBlock
		s.store.byID[rec.ChallengeID].Stage2Decision = DecisionBlock
		out := cloneRecord(rec)
		s.store.mu.Unlock()
		s.signalTerminal(out.ChallengeID)
		return RetryResult{
			Record: out, Stage2Decision: DecisionBlock, Intervention: InterventionAppealableChallenge,
			Relationship: rel, RejectedReason: "retry_without_justification",
		}, nil
	}

	if rec.State == StateJustified && rec.RetryBudget <= 0 {
		from := rec.State
		now := s.now()
		rec = s.store.appendTransition(rec, from, StateRejected, "rejected", req.CorrelationID, req.Proposed.ActionID, fpOwner.Fingerprint, "budget_exhausted", now, nil)
		rec.Stage2Decision = DecisionBlock
		s.store.byID[rec.ChallengeID].Stage2Decision = DecisionBlock
		out := cloneRecord(rec)
		s.store.mu.Unlock()
		s.signalTerminal(out.ChallengeID)
		return RetryResult{
			Record: out, Stage2Decision: DecisionBlock, Relationship: rel, RejectedReason: "budget_exhausted",
		}, nil
	}

	if rec.State == StateRetryPending {
		s.store.mu.Unlock()
		final, werr := s.waitTerminal(ctx, req.ChallengeID)
		if werr != nil {
			return RetryResult{RejectedReason: "context_canceled", Relationship: rel}, werr
		}
		// After wait, apply one-shot identity rules against terminal state.
		if final.State == StateAllowedOnce {
			if final.ConsumedRetryKey != "" && attemptKey == final.ConsumedRetryKey {
				return RetryResult{
					Record: final, Stage2Decision: DecisionAllow, Intervention: final.Intervention,
					Relationship: rel, IdempotentReplay: true, RejectedReason: "duplicate_retry",
				}, nil
			}
			return RetryResult{
				Record: final, Stage2Decision: DecisionBlock, Intervention: final.Intervention,
				Relationship: rel, IdempotentReplay: true, RejectedReason: "retry_budget_exhausted",
			}, fmt.Errorf("retry: retry_budget_exhausted")
		}
		return RetryResult{
			Record: final, Stage2Decision: final.Stage2Decision, Intervention: final.Intervention,
			Relationship: rel, IdempotentReplay: true, RejectedReason: "duplicate_retry",
		}, nil
	}

	if rec.State != StateJustified {
		s.store.mu.Unlock()
		return RetryResult{Record: rec, Stage2Decision: DecisionBlock, Relationship: rel, RejectedReason: "invalid_state"}, fmt.Errorf("retry: invalid state %s", rec.State)
	}

	// Consume budget atomically and enter RETRY_PENDING
	from := rec.State
	now := s.now()
	if rec.RetryBudget > 0 {
		rec.RetryBudget--
	}
	seq1 := s.store.nextSeqLocked()
	rec.UpdatedSequence = seq1
	rec.UpdatedAt = now
	rec.State = StateRetryPending
	evBudget := ChallengeEvent{
		SchemaVersion: SchemaChallengeEvent,
		Sequence:      seq1,
		ChallengeID:   rec.ChallengeID,
		SessionID:     rec.SessionID,
		Type:          "budget_consumed",
		FromState:     from,
		ToState:       StateRetryPending,
		CorrelationID: req.CorrelationID,
		CausationID:   req.Proposed.ActionID,
		PayloadHash:   attemptKey,
		Note:          "one_shot_budget",
		At:            now,
	}
	s.store.putLocked(rec, evBudget, nil)
	just := s.store.justifications[rec.ChallengeID]
	pending := cloneRecord(rec)
	s.store.mu.Unlock()

	var reCtx *ReEvalContext
	if req.ReEval != nil {
		reCtx = req.ReEval
	}
	re, err := s.reeval.ReEvaluate(ctx, pending, req.Proposed, &just, reCtx)
	if err != nil {
		re = ReEvalResult{Stage2Decision: DecisionBlock, Reason: "reeval_error"}
	}

	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	cur2, ok := s.store.byID[req.ChallengeID]
	if !ok {
		return RetryResult{}, fmt.Errorf("retry: lost challenge")
	}
	rec = cloneRecord(*cur2)
	if rec.State == StateAllowedOnce || rec.State == StateRejected || rec.State == StateHumanReview {
		s.signalTerminal(rec.ChallengeID)
		if rec.State == StateAllowedOnce && rec.ConsumedRetryKey != "" && attemptKey == rec.ConsumedRetryKey {
			return RetryResult{
				Record: rec, Stage2Decision: DecisionAllow, Intervention: rec.Intervention,
				Relationship: rel, IdempotentReplay: true,
			}, nil
		}
		if rec.State == StateAllowedOnce {
			return RetryResult{
				Record: rec, Stage2Decision: DecisionBlock, Intervention: rec.Intervention,
				Relationship: rel, IdempotentReplay: true, RejectedReason: "retry_budget_exhausted",
			}, fmt.Errorf("retry: retry_budget_exhausted")
		}
		return RetryResult{
			Record: rec, Stage2Decision: rec.Stage2Decision, Intervention: rec.Intervention,
			Relationship: rel, IdempotentReplay: true,
		}, nil
	}
	if rec.State != StateRetryPending {
		return RetryResult{Record: rec, Stage2Decision: DecisionBlock, Relationship: rel, RejectedReason: "state_changed"}, fmt.Errorf("retry: unexpected state %s", rec.State)
	}

	now2 := s.now()
	from2 := rec.State
	switch {
	case re.Intervention == InterventionHumanReview:
		rec.Stage2Decision = DecisionBlock
		rec.Intervention = InterventionHumanReview
		rec.ConsumedRetryKey = attemptKey
		rec = s.store.appendTransition(rec, from2, StateHumanReview, "human_review", req.CorrelationID, req.Proposed.ActionID, attemptKey, re.Reason, now2, nil)
		s.store.byID[rec.ChallengeID].Stage2Decision = DecisionBlock
		s.store.byID[rec.ChallengeID].Intervention = InterventionHumanReview
		s.store.byID[rec.ChallengeID].ConsumedRetryKey = attemptKey
	case re.Stage2Decision == DecisionAllow:
		rec.Stage2Decision = DecisionAllow
		rec.Intervention = InterventionNone
		rec.ConsumedRetryKey = attemptKey
		rec = s.store.appendTransition(rec, from2, StateAllowedOnce, "allowed_once", req.CorrelationID, req.Proposed.ActionID, attemptKey, re.Reason, now2, nil)
		s.store.byID[rec.ChallengeID].Stage2Decision = DecisionAllow
		s.store.byID[rec.ChallengeID].Intervention = InterventionNone
		s.store.byID[rec.ChallengeID].ConsumedRetryKey = attemptKey
	default:
		rec.Stage2Decision = DecisionBlock
		rec.ConsumedRetryKey = attemptKey
		rec = s.store.appendTransition(rec, from2, StateRejected, "rejected", req.CorrelationID, req.Proposed.ActionID, attemptKey, re.Reason, now2, nil)
		s.store.byID[rec.ChallengeID].Stage2Decision = DecisionBlock
		s.store.byID[rec.ChallengeID].ConsumedRetryKey = attemptKey
	}
	out := cloneRecord(*s.store.byID[rec.ChallengeID])
	s.signalTerminal(out.ChallengeID)
	return RetryResult{
		Record: out, Stage2Decision: out.Stage2Decision, Intervention: out.Intervention, Relationship: rel,
	}, nil
}

// Abandon marks an open/justified challenge abandoned.
func (s *Service) Abandon(ctx context.Context, challengeID, corr string) (ChallengeRecord, error) {
	s.store.mu.Lock()
	cur, ok := s.store.byID[challengeID]
	if !ok {
		s.store.mu.Unlock()
		return ChallengeRecord{}, fmt.Errorf("abandon: unknown challenge")
	}
	rec := cloneRecord(*cur)
	if isTerminal(rec.State) {
		s.store.mu.Unlock()
		return rec, nil
	}
	from := rec.State
	now := s.now()
	rec = s.store.appendTransition(rec, from, StateAbandoned, "abandoned", corr, challengeID, "", "user_abandon", now, nil)
	out := cloneRecord(rec)
	s.store.mu.Unlock()
	s.signalTerminal(out.ChallengeID)
	return out, nil
}

// ExpireDue marks challenges past ExpiresAtSequence as EXPIRED. Cannot be revived.
func (s *Service) ExpireDue(ctx context.Context) int {
	s.store.mu.Lock()
	n := 0
	seq := s.store.seq
	now := s.now()
	var expired []string
	for id, rec := range s.store.byID {
		if rec.ExpiresAtSequence > 0 && seq >= rec.ExpiresAtSequence && !isTerminal(rec.State) {
			from := rec.State
			cp := cloneRecord(*rec)
			_ = s.store.appendTransition(cp, from, StateExpired, "expired", "", id, "", "sequence_expiry", now, nil)
			expired = append(expired, id)
			n++
		}
	}
	s.store.mu.Unlock()
	for _, id := range expired {
		s.signalTerminal(id)
	}
	return n
}

// Get returns the current record.
func (s *Service) Get(id string) (ChallengeRecord, bool) {
	return s.store.Get(id)
}

// Replay reconstructs state from events.
func (s *Service) Replay(id string) (ChallengeRecord, error) {
	return s.store.ReplayFromStore(id)
}

// Audit builds a closed audit record.
func (s *Service) Audit(id string) (AuditRecord, error) {
	rec, ok := s.store.Get(id)
	if !ok {
		return AuditRecord{}, fmt.Errorf("audit: unknown challenge")
	}
	return AuditRecord{
		SchemaVersion:     SchemaChallengeAudit,
		ChallengeID:       rec.ChallengeID,
		SessionID:         rec.SessionID,
		State:             string(rec.State),
		Stage2Decision:    rec.Stage2Decision,
		Intervention:      rec.Intervention,
		ActionFingerprint: rec.ActionFingerprint,
		JustificationHash: rec.JustificationHash,
		PolicyHash:        rec.PolicyHash,
		RulesetHash:       rec.RulesetHash,
		InputHash:         hashInput(rec),
		FingerprintHash:   rec.ActionFingerprint,
	}, nil
}

// CacheKey builds cache identity inputs for this challenge.
func (s *Service) CacheKey(id string, evidence []string, modelID, promptHash string) (CacheKeyInputs, error) {
	rec, ok := s.store.Get(id)
	if !ok {
		return CacheKeyInputs{}, fmt.Errorf("cache key: unknown challenge")
	}
	return BuildCacheKeyInputs(rec, evidence, modelID, promptHash), nil
}

// Ensure Stage2 decision is only ALLOW|BLOCK (compile-time documentation helper).
func ValidStage2Decision(d string) bool {
	return d == classifier.DecisionAllow || d == classifier.DecisionBlock
}

func (s *Service) newID(prefix string) string {
	s.idMu.Lock()
	s.idSeq++
	n := s.idSeq
	s.idMu.Unlock()
	now := s.now().UnixNano()
	h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d-%d", prefix, now, n)))
	return prefix + "-" + hex.EncodeToString(h[:8])
}

func (s *Service) expireIfNeeded(rec *ChallengeRecord) error {
	if rec.ExpiresAtSequence <= 0 {
		return nil
	}
	if s.store.Sequence() < rec.ExpiresAtSequence {
		return nil
	}
	if isTerminal(rec.State) {
		return nil
	}
	s.store.mu.Lock()
	cur, ok := s.store.byID[rec.ChallengeID]
	if !ok {
		s.store.mu.Unlock()
		return nil
	}
	if isTerminal(cur.State) {
		*rec = cloneRecord(*cur)
		s.store.mu.Unlock()
		return nil
	}
	from := cur.State
	now := s.now()
	updated := s.store.appendTransition(cloneRecord(*cur), from, StateExpired, "expired", "", rec.ChallengeID, "", "sequence_expiry", now, nil)
	*rec = cloneRecord(updated)
	id := rec.ChallengeID
	s.store.mu.Unlock()
	s.signalTerminal(id)
	return fmt.Errorf("challenge expired")
}

// waitTerminal blocks until the challenge reaches a terminal state.
// On ctx cancel/deadline returns the context error — never nil with RETRY_PENDING.
func (s *Service) waitTerminal(ctx context.Context, id string) (ChallengeRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		rec, ok := s.store.Get(id)
		if ok && isTerminal(rec.State) {
			return rec, nil
		}
		ch := s.terminalWaitCh(id)
		rec, ok = s.store.Get(id)
		if ok && isTerminal(rec.State) {
			return rec, nil
		}
		select {
		case <-ctx.Done():
			return ChallengeRecord{}, ctx.Err()
		case <-ch:
		}
	}
}

// terminalWaitCh returns a channel that is closed when the challenge becomes terminal.
func (s *Service) terminalWaitCh(id string) <-chan struct{} {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	if s.terminalCh == nil {
		s.terminalCh = make(map[string]chan struct{})
	}
	ch, ok := s.terminalCh[id]
	if !ok {
		ch = make(chan struct{})
		s.terminalCh[id] = ch
	}
	return ch
}

// signalTerminal unblocks all waitTerminal callers for id.
func (s *Service) signalTerminal(id string) {
	if id == "" {
		return
	}
	s.termMu.Lock()
	defer s.termMu.Unlock()
	if s.terminalCh == nil {
		s.terminalCh = make(map[string]chan struct{})
	}
	ch, ok := s.terminalCh[id]
	if !ok {
		// Create already-closed so late waiters observe terminal immediately.
		ch = make(chan struct{})
		close(ch)
		s.terminalCh[id] = ch
		return
	}
	select {
	case <-ch:
		// already closed
	default:
		close(ch)
	}
}

func hashPolicy(ver, ruleset, block string) string {
	h := sha256.Sum256([]byte(ver + "|" + ruleset + "|" + block))
	return hex.EncodeToString(h[:8])
}

func hashInput(rec ChallengeRecord) string {
	h := sha256.Sum256([]byte(rec.ChallengeID + "|" + rec.SessionID + "|" + string(rec.State) + "|" + rec.JustificationHash))
	return hex.EncodeToString(h[:8])
}

func pickContract(a, b int) int {
	if a != 0 {
		return a
	}
	return b
}

// checkOwnership enforces session (and optional request session) binding.
func checkOwnership(rec ChallengeRecord, req RetryRequest) error {
	if sid := strings.TrimSpace(req.SessionID); sid != "" && sid != rec.SessionID {
		return fmt.Errorf("retry: session_id mismatch (challenge=%s request=%s)", rec.SessionID, sid)
	}
	if sid := strings.TrimSpace(req.Proposed.SessionID); sid != "" && sid != rec.SessionID {
		return fmt.Errorf("retry: proposed session_id mismatch (challenge=%s proposed=%s)", rec.SessionID, sid)
	}
	if b := strings.TrimSpace(req.Branch); b != "" && rec.Branch != "" && b != rec.Branch {
		return fmt.Errorf("retry: branch mismatch (challenge=%s request=%s)", rec.Branch, b)
	}
	return nil
}

// isTargetSuperset reports whether cand has every original target plus at least one extra.
func isTargetSuperset(cand, original []string) bool {
	if len(cand) <= len(original) || len(original) == 0 {
		return false
	}
	set := map[string]struct{}{}
	for _, t := range cand {
		set[t] = struct{}{}
	}
	for _, t := range original {
		if _, ok := set[t]; !ok {
			return false
		}
	}
	return true
}

// retryAttemptKey is the durable one-shot retry identity.
// Missing CorrelationID and RetryRequestID are encoded as empty length-prefixed fields
// so they cannot be confused with unlimited idempotent replay.
func retryAttemptKey(challengeID string, req RetryRequest, fingerprint string) string {
	h := sha256.New()
	fields := []struct{ k, v string }{
		{"challenge_id", challengeID},
		{"action_id", req.Proposed.ActionID},
		{"correlation_id", req.CorrelationID},
		{"retry_request_id", req.RetryRequestID},
		{"fingerprint", fingerprint},
	}
	_, _ = fmt.Fprintf(h, "n=%d", len(fields))
	for _, f := range fields {
		_, _ = fmt.Fprintf(h, ";k=%d:%s;v=%d:%s", len(f.k), f.k, len(f.v), f.v)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// policyIdentityMatch compares correctness-relevant fields for active challenge reuse.
func policyIdentityMatch(existing ChallengeRecord, req OpenRequest, claims []string, policyHash string) bool {
	if existing.PolicyClass != req.PolicyClass {
		return false
	}
	if existing.BlockClass != req.BlockClass {
		return false
	}
	if existing.PolicyVersion != req.PolicyVersion {
		return false
	}
	if existing.RulesetHash != req.RulesetHash {
		return false
	}
	if existing.PolicyHash != policyHash {
		return false
	}
	if existing.ReasonCode != req.ReasonCode {
		return false
	}
	if existing.Branch != req.Branch {
		return false
	}
	if existing.WorkspaceRevision != req.Proposed.WorkspaceRevision {
		return false
	}
	if existing.ContractRevision != req.Proposed.ContractRevision {
		return false
	}
	if len(existing.RequiredClaims) != len(claims) {
		return false
	}
	for i := range claims {
		if existing.RequiredClaims[i] != claims[i] {
			return false
		}
	}
	return true
}
