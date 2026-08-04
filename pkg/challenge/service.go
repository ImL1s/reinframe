package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

// Service is the host-neutral challenge workflow API (#131).
type Service struct {
	store  *Store
	reeval ReEvaluator
	now    func() time.Time
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
		store:  st,
		reeval: re,
		now:    now,
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
	// Ticket for nonAppealBarrier race detection: sample before heavy work so a
	// delayed Open that loses to a later hard-deny cannot clear that barrier.
	openTicket := s.store.Sequence()
	reqSID := strings.TrimSpace(req.SessionID)
	paSID := strings.TrimSpace(req.Proposed.SessionID)
	if reqSID == "" {
		reqSID = paSID
	}
	// Explicit session mismatch is rejected — never fingerprint a foreign action under another session.
	if reqSID != "" && paSID != "" && reqSID != paSID {
		return ChallengeRecord{}, fmt.Errorf("challenge open: session_id mismatch (request=%s proposed=%s)", reqSID, paSID)
	}
	req.SessionID = reqSID
	if strings.TrimSpace(req.SessionID) == "" {
		return ChallengeRecord{}, fmt.Errorf("challenge open: session_id required")
	}
	if err := ValidateProposedForChallenge(req.Proposed); err != nil {
		return ChallengeRecord{}, fmt.Errorf("challenge open: %w", err)
	}
	req.PolicyClass = NormalizePolicyClass(req.PolicyClass)
	// Do not invent PRODUCTIVITY_GENERIC before appeal routing — empty class must
	// still run action-based inference in ResolveBlockClass / ClassifyAppealability.
	req.BlockClass = NormalizeBlockClass(req.BlockClass)
	if req.ReasonCode == "" {
		req.ReasonCode = "BLOCK"
	}
	if req.PolicyClass == PolicyClassSecurity {
		// SECURITY policy: non-hard/non-irreversible (including empty) → unknown security.
		if req.BlockClass == "" || (!IsHardDenyClass(req.BlockClass) && !IsIrreversibleClass(req.BlockClass)) {
			req.BlockClass = BlockClassUnknownSecurity
		}
	} else {
		// Infer storage class from action when omitted (deploy/secret/etc.).
		req.BlockClass = ResolveBlockClass(req.BlockClass, req.Proposed)
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
	// Expire any active challenge for the same session|fingerprint first so a later
	// hard deny cannot be bypassed via a stale productivity challenge retry.
	if appeal == AppealNonAppealable {
		now := s.now()
		// Expire under lock + durable barrier so a concurrent appealable Open cannot
		// insert after this hard deny returns.
		s.store.mu.Lock()
		s.expireActiveByFingerprintLocked(req.SessionID, fp.Fingerprint, req.CorrelationID, req.Proposed.ActionID, "superseded_by_hard_block", now)
		s.store.markNonAppealBarrierLocked(req.SessionID, fp.Fingerprint, "hard_block", req.PolicyVersion, req.RulesetHash)
		s.store.mu.Unlock()
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
		// Atomic under store.mu: expire any active productivity challenge for the same
		// session|fp, then insert the HUMAN_REVIEW record (no concurrent Open window).
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
		s.expireActiveByFingerprintLocked(req.SessionID, fp.Fingerprint, req.CorrelationID, req.Proposed.ActionID, "superseded_by_human_review", now)
		s.store.markNonAppealBarrierLocked(req.SessionID, fp.Fingerprint, "human_review", req.PolicyVersion, req.RulesetHash)
		rec = s.store.appendTransition(rec, "", StateHumanReview, "human_review", req.CorrelationID, req.Proposed.ActionID, fp.Fingerprint, "open_human_review", now, nil)
		s.store.mu.Unlock()
		s.signalTerminal(rec.ChallengeID)
		return rec, nil
	}

	// All active reuse / supersede for appealable opens happens under store.mu below
	// (no lock-free ActiveByFingerprint path — avoids dual-OPEN races).

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
	// Hard-deny / human-review barrier: reject concurrent/stale weaker Open.
	if note, blocked := s.store.nonAppealBarrierNoteLocked(req.SessionID, fp.Fingerprint, req.PolicyVersion, req.RulesetHash, openTicket); blocked {
		return ChallengeRecord{}, fmt.Errorf("challenge open: non-appealable barrier (%s) for fingerprint", note)
	}
	// Under lock: reclaim any active record with the same action fingerprint.
	// Collect IDs first — do not range+mutate byID (map iteration skips on concurrent map writes).
	// Always expire policy mismatches first; return a single policy match if any remains.
	type hit struct {
		id  string
		rec ChallengeRecord
	}
	var hits []hit
	for id, existing := range s.store.byID {
		if existing.SessionID != req.SessionID || existing.ActionFingerprint != fp.Fingerprint {
			continue
		}
		if isTerminal(existing.State) {
			continue
		}
		hits = append(hits, hit{id: id, rec: cloneRecord(*existing)})
	}
	var match *ChallengeRecord
	for _, h := range hits {
		if policyIdentityMatch(h.rec, req, claims, policyHash) {
			if match == nil {
				m := h.rec
				match = &m
			} else {
				// Duplicate same-policy OPEN (should not happen) — expire extras.
				from := h.rec.State
				_ = s.store.appendTransition(h.rec, from, StateExpired, "expired", req.CorrelationID, h.id, fp.Fingerprint, "superseded_duplicate_open", now, nil)
				s.signalTerminal(h.id)
			}
			continue
		}
		from := h.rec.State
		_ = s.store.appendTransition(h.rec, from, StateExpired, "expired", req.CorrelationID, h.id, fp.Fingerprint, "superseded_by_policy_change", now, nil)
		s.signalTerminal(h.id)
	}
	if match != nil {
		return cloneRecord(*match), nil
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
	// Require durable attempt identity so empty CorrelationID cannot mint unlimited ALLOW replays.
	if strings.TrimSpace(req.CorrelationID) == "" && strings.TrimSpace(req.RetryRequestID) == "" {
		return RetryResult{Stage2Decision: DecisionBlock, RejectedReason: "missing_retry_identity"}, fmt.Errorf("retry: CorrelationID or RetryRequestID required")
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

	// Recheck sequence expiry under the same lock before budget consume / terminal handling
	// (expireIfNeeded above can race with concurrent store sequence advances).
	if rec.ExpiresAtSequence > 0 && s.store.seq >= rec.ExpiresAtSequence && !isTerminal(rec.State) {
		from := rec.State
		now := s.now()
		rec = s.store.appendTransition(rec, from, StateExpired, "expired", req.CorrelationID, req.Proposed.ActionID, fpOwner.Fingerprint, "sequence_expiry_under_lock", now, nil)
		out := cloneRecord(rec)
		s.store.mu.Unlock()
		s.signalTerminal(out.ChallengeID)
		return RetryResult{Record: out, Stage2Decision: DecisionBlock, Relationship: rel, RejectedReason: "expired"}, fmt.Errorf("retry: expired")
	}

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
		// Propagate owner cancellation — do not silently convert to ordinary BLOCK success.
		if ctx != nil && ctx.Err() != nil {
			// Best-effort terminalize budget consumption without claiming a successful re-eval.
			s.store.mu.Lock()
			if cur, ok := s.store.byID[req.ChallengeID]; ok && cur.State == StateRetryPending {
				now := s.now()
				cp := cloneRecord(*cur)
				cp.ConsumedRetryKey = attemptKey
				cp.Stage2Decision = DecisionBlock
				_ = s.store.appendTransition(cp, StateRetryPending, StateRejected, "rejected", req.CorrelationID, req.Proposed.ActionID, attemptKey, "owner_context_canceled", now, nil)
				s.store.byID[req.ChallengeID].ConsumedRetryKey = attemptKey
				s.store.byID[req.ChallengeID].Stage2Decision = DecisionBlock
				s.signalTerminal(req.ChallengeID)
			}
			s.store.mu.Unlock()
			return RetryResult{RejectedReason: "context_canceled", Relationship: rel}, ctx.Err()
		}
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
	// Store-scoped sequence so multi-Service shared Store cannot collide on idSeq=1.
	return s.store.newID(prefix, s.now())
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

// expireActiveByFingerprintLocked expires every non-terminal challenge for session|fp.
// Caller must hold s.store.mu.
func (s *Service) expireActiveByFingerprintLocked(sessionID, fingerprint, corr, cause, note string, now time.Time) {
	var ids []string
	for id, existing := range s.store.byID {
		if existing.SessionID != sessionID || existing.ActionFingerprint != fingerprint {
			continue
		}
		if isTerminal(existing.State) {
			continue
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		cur, ok := s.store.byID[id]
		if !ok || isTerminal(cur.State) {
			continue
		}
		from := cur.State
		_ = s.store.appendTransition(cloneRecord(*cur), from, StateExpired, "expired", corr, cause, fingerprint, note, now, nil)
		// Signal without re-taking store.mu (termMu is separate).
		s.store.signalTerminal(id)
	}
}

// waitTerminal blocks until the challenge reaches a terminal state.
// On ctx cancel/deadline returns the context error — never nil with RETRY_PENDING.
// Uses store-scoped terminal channels so multi-Service shared-Store waiters wake.
func (s *Service) waitTerminal(ctx context.Context, id string) (ChallengeRecord, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		rec, ok := s.store.Get(id)
		if ok && isTerminal(rec.State) {
			return rec, nil
		}
		ch := s.store.terminalWaitCh(id)
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

// signalTerminal unblocks all waitTerminal callers for id (store-scoped).
func (s *Service) signalTerminal(id string) {
	s.store.signalTerminal(id)
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
