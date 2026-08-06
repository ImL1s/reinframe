package challenge

import (
	"fmt"
	"time"
)

// ApplyEvent is the single pure projector for challenge state transitions.
// Live Service terminalize paths and ReplayFromStore MUST use this so live == replay
// for every mutable field (including Intervention on ALLOWED_ONCE).
func ApplyEvent(rec ChallengeRecord, ev ChallengeEvent) (ChallengeRecord, error) {
	// Idempotent self-transition
	if rec.State != "" && rec.State == ev.ToState && ev.Type != "budget_consumed" {
		rec.UpdatedSequence = ev.Sequence
		rec.UpdatedAt = ev.At
		return rec, nil
	}

	switch ev.Type {
	case "opened":
		rec.State = StateOpen
		rec.Stage2Decision = DecisionBlock
		if rec.Intervention == "" {
			rec.Intervention = InterventionAppealableChallenge
		}
		if rec.RetryBudgetInitial == 0 && rec.RetryBudget == 0 && rec.Appealability != AppealHumanReview {
			rec.RetryBudget = InitialRetryBudget
			rec.RetryBudgetInitial = InitialRetryBudget
		}
		if rec.CreatedSequence == 0 {
			rec.CreatedSequence = ev.Sequence
		}
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = ev.At
		}
	case "justified":
		rec.State = StateJustified
	case "budget_consumed":
		if rec.RetryBudget > 0 {
			rec.RetryBudget--
		}
		if ev.ToState != "" {
			rec.State = ev.ToState
		} else {
			rec.State = StateRetryPending
		}
	case "retry_pending":
		rec.State = StateRetryPending
	case "allowed_once":
		rec.State = StateAllowedOnce
		rec.Stage2Decision = DecisionAllow
		rec.Intervention = InterventionNone // MUST match live path
	case "rejected":
		rec.State = StateRejected
		rec.Stage2Decision = DecisionBlock
	case "human_review":
		rec.State = StateHumanReview
		rec.Stage2Decision = DecisionBlock
		rec.Intervention = InterventionHumanReview
		rec.RetryBudget = 0
	case "abandoned":
		rec.State = StateAbandoned
	case "expired":
		rec.State = StateExpired
	default:
		if ev.ToState != "" {
			rec.State = ev.ToState
		} else {
			return rec, fmt.Errorf("apply event: unknown type %q", ev.Type)
		}
	}
	rec.UpdatedSequence = ev.Sequence
	if !ev.At.IsZero() {
		rec.UpdatedAt = ev.At
	} else {
		rec.UpdatedAt = time.Time{}
	}
	// Durable provider-call audit link (append-only event → projected record).
	if ev.ProviderCallAuditID != "" {
		rec.ProviderCallAuditID = ev.ProviderCallAuditID
	}
	return rec, nil
}
