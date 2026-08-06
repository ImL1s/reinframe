package evaluation

import (
	"fmt"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
)

// Challenge dataset / report schemas (#140 Lane A).
const (
	ChallengeDatasetSchemaVersion = "reinframe.challenge_eval_case.v1"
	ChallengeReportSchemaVersion  = "reinframe.challenge_eval_report.v1"
	ChallengeLaneDeterministic    = "lane_a_deterministic"
)

// Challenge case kinds for metrics denominators.
const (
	ChallengeKindLegitimateAppeal   = "legitimate_appeal"
	ChallengeKindInvalidAppeal      = "invalid_appeal"
	ChallengeKindBypassAttempt      = "bypass_attempt"
	ChallengeKindNonAppealable      = "non_appealable"
	ChallengeKindHumanReview        = "human_review"
	ChallengeKindHealthyNoChallenge = "healthy_no_challenge"
	ChallengeKindReducedScope       = "reduced_scope"
	ChallengeKindDuplicateRetry     = "duplicate_retry"
)

// ChallengeCase is one versioned offline challenge evaluation fixture (#140 Lane A).
type ChallengeCase struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	Source        string `json:"source"` // synthetic | hand_labeled | anonymized_replay
	Kind          string `json:"kind"`
	Description   string `json:"description,omitempty"`
	PolicyClass   string `json:"policy_class"`
	BlockClass    string `json:"block_class"`
	ReasonCode    string `json:"reason_code,omitempty"`
	SessionID     string `json:"session_id"`
	Branch        string `json:"branch,omitempty"`
	PolicyVersion string `json:"policy_version,omitempty"`
	RulesetHash   string `json:"ruleset_hash,omitempty"`

	// Initial proposed action that was blocked.
	Proposed ProposedActionFixture `json:"proposed"`

	// Known evidence IDs available to justification validation.
	KnownEvidenceIDs []string `json:"known_evidence_ids,omitempty"`

	// Expected open path.
	ExpectAppealability string `json:"expect_appealability"` // APPEALABLE | NON_APPEALABLE | HUMAN_REVIEW | none
	ExpectOpenState     string `json:"expect_open_state,omitempty"`
	ExpectOpenError     bool   `json:"expect_open_error,omitempty"` // non-appealable returns error

	// Optional justification (omit for "no justification" paths).
	Justification      *JustificationFixture `json:"justification,omitempty"`
	ExpectJustifyOK    *bool                 `json:"expect_justify_ok,omitempty"`
	ExpectJustifyError string                `json:"expect_justify_error,omitempty"` // substring

	// Retry proposed action after justification (or without).
	RetryProposed        *ProposedActionFixture `json:"retry_proposed,omitempty"`
	ExpectRelationship   string                 `json:"expect_relationship,omitempty"` // same|reduced_scope|different|bypass
	ExpectRetryState     string                 `json:"expect_retry_state,omitempty"`
	ExpectStage2         string                 `json:"expect_stage2,omitempty"` // ALLOW|BLOCK
	ExpectRejectedReason string                 `json:"expect_rejected_reason,omitempty"`
	// DuplicateRetry when true issues the same retry twice and expects idempotent second.
	DuplicateRetry bool `json:"duplicate_retry,omitempty"`
	// RetryUserException applies Stage-2 UserException on re-eval (not justification auto-allow).
	RetryUserException bool `json:"retry_user_exception,omitempty"`

	// Healthy counterexample: expect no challenge opportunity (not used for open path).
	ExpectNoChallenge bool `json:"expect_no_challenge,omitempty"`
}

// ProposedActionFixture is a JSON-friendly ProposedAction subset for fixtures.
type ProposedActionFixture struct {
	ActionID          string   `json:"action_id,omitempty"`
	ToolName          string   `json:"tool_name"`
	ToolClass         string   `json:"tool_class"`
	Command           string   `json:"command,omitempty"`
	FilePath          string   `json:"file_path,omitempty"`
	Arguments         []string `json:"arguments,omitempty"`
	WorkspaceRevision string   `json:"workspace_revision,omitempty"`
	ContractRevision  int      `json:"contract_revision,omitempty"`
}

// JustificationFixture is a JSON-friendly justification payload.
type JustificationFixture struct {
	ConcreteValue            string   `json:"concrete_value"`
	PreventedFailureOrThreat string   `json:"prevented_failure_or_threat"`
	EstimatedCost            string   `json:"estimated_cost"`
	AlternativesConsidered   string   `json:"alternatives_considered,omitempty"`
	ScopeLimit               string   `json:"scope_limit"`
	VerificationPlan         string   `json:"verification_plan"`
	RollbackPlan             string   `json:"rollback_plan"`
	SupportingEvidenceIDs    []string `json:"supporting_evidence_event_ids,omitempty"`
}

// ToProposedAction maps fixture → adapter.ProposedAction.
func (p ProposedActionFixture) ToProposedAction(sessionID string) adapter.ProposedAction {
	aid := p.ActionID
	if aid == "" {
		aid = "pa-" + shortID(p.ToolName, p.Command, p.FilePath)
	}
	tc := p.ToolClass
	if tc == "" {
		tc = adapter.ToolClassShell
	}
	ws := p.WorkspaceRevision
	if ws == "" {
		ws = "ws-1"
	}
	return adapter.ProposedAction{
		SchemaVersion:     adapter.ProposedActionSchemaVersion,
		SessionID:         sessionID,
		ActionID:          aid,
		ToolName:          p.ToolName,
		ToolClass:         tc,
		Command:           p.Command,
		FilePath:          p.FilePath,
		Arguments:         append([]string(nil), p.Arguments...),
		WorkspaceRevision: ws,
		ContractRevision:  p.ContractRevision,
		Source:            "synthetic",
		ParseStatus:       "ok",
	}
}

func shortID(parts ...string) string {
	return strings.ReplaceAll(strings.Join(parts, "-"), " ", "_")
}

// ValidateChallengeCase rejects unknown kinds and incomplete expectations.
func ValidateChallengeCase(c ChallengeCase) error {
	if c.SchemaVersion != ChallengeDatasetSchemaVersion {
		return fmt.Errorf("schema_version want %s got %q", ChallengeDatasetSchemaVersion, c.SchemaVersion)
	}
	if strings.TrimSpace(c.CaseID) == "" {
		return fmt.Errorf("case_id required")
	}
	switch c.Kind {
	case ChallengeKindLegitimateAppeal, ChallengeKindInvalidAppeal, ChallengeKindBypassAttempt,
		ChallengeKindNonAppealable, ChallengeKindHumanReview, ChallengeKindHealthyNoChallenge,
		ChallengeKindReducedScope, ChallengeKindDuplicateRetry:
	default:
		return fmt.Errorf("unknown kind %q", c.Kind)
	}
	switch c.Source {
	case "synthetic", "hand_labeled", "anonymized_replay", "":
	default:
		return fmt.Errorf("unknown source %q", c.Source)
	}
	if c.ExpectNoChallenge {
		return nil
	}
	if c.SessionID == "" {
		return fmt.Errorf("session_id required")
	}
	if c.Proposed.ToolName == "" {
		return fmt.Errorf("proposed.tool_name required")
	}
	switch c.ExpectAppealability {
	case challenge.AppealAppealable, challenge.AppealNonAppealable, challenge.AppealHumanReview, "none":
	default:
		return fmt.Errorf("expect_appealability invalid")
	}
	return nil
}

// ChallengeCaseResult is one case outcome (layers scored separately).
type ChallengeCaseResult struct {
	CaseID string `json:"case_id"`
	Kind   string `json:"kind"`

	OpenOK            bool   `json:"open_ok"`
	OpenError         string `json:"open_error,omitempty"`
	ObservedAppeal    string `json:"observed_appealability,omitempty"`
	ObservedOpenState string `json:"observed_open_state,omitempty"`
	OpenMatch         bool   `json:"open_match"`

	JustifyAttempted bool   `json:"justify_attempted"`
	JustifyOK        bool   `json:"justify_ok"`
	JustifyError     string `json:"justify_error,omitempty"`
	JustifyMatch     bool   `json:"justify_match"`

	RetryAttempted     bool   `json:"retry_attempted"`
	ObservedRelation   string `json:"observed_relationship,omitempty"`
	ObservedRetryState string `json:"observed_retry_state,omitempty"`
	ObservedStage2     string `json:"observed_stage2,omitempty"`
	ObservedRejected   string `json:"observed_rejected_reason,omitempty"`
	IdempotentReplay   bool   `json:"idempotent_replay,omitempty"`
	RetryMatch         bool   `json:"retry_match"`

	// Layer pass flags (not collapsed into one score).
	PassOpen    bool `json:"pass_open"`
	PassJustify bool `json:"pass_justify"`
	PassRetry   bool `json:"pass_retry"`
	PassCase    bool `json:"pass_case"`

	AddedTurns int `json:"added_turns"`
}

// ChallengeMetrics are denominator-correct counters (#140).
type ChallengeMetrics struct {
	CasesTotal int `json:"cases_total"`

	// Challenge quality
	AppealableCases       int `json:"appealable_cases"`
	OpenAppealMatch       int `json:"open_appeal_match"`
	NonAppealableCases    int `json:"non_appealable_cases"`
	NonAppealableRoutedOK int `json:"non_appealable_routed_ok"`
	HumanReviewCases      int `json:"human_review_cases"`
	HumanReviewRoutedOK   int `json:"human_review_routed_ok"`
	ValidAppealAccepted   int `json:"valid_appeal_accepted"`
	ValidAppealAttempts   int `json:"valid_appeal_attempts"`
	InvalidAppealRejected int `json:"invalid_appeal_rejected"`
	InvalidAppealAttempts int `json:"invalid_appeal_attempts"`

	// Bypass / rewrite binding
	BypassAttempts        int `json:"bypass_attempts"`
	RewriteBoundOK        int `json:"rewrite_bound_ok"`  // syntax rewrite stays bound (may ALLOW once)
	HostileRejectOK       int `json:"hostile_reject_ok"` // different-target must not ALLOW
	ReducedScopeCases     int `json:"reduced_scope_cases"`
	ReducedScopeOK        int `json:"reduced_scope_ok"`
	DuplicateRetryCases   int `json:"duplicate_retry_cases"`
	DuplicateIdempotentOK int `json:"duplicate_idempotent_ok"`

	// Recovery
	AllowedOnce        int `json:"allowed_once"`
	RejectedAfterRetry int `json:"rejected_after_retry"`
	HumanEscalation    int `json:"human_escalation"`

	// Cost/UX (deterministic accounting — no fabricated tokens)
	TotalAddedTurns int  `json:"total_added_turns"`
	ProviderCalls   int  `json:"provider_calls"` // Lane A uses fake reeval; usually 0
	HardGateEnabled bool `json:"hard_gate_enabled"`
}

// ChallengeReport is the immutable Lane A report.
type ChallengeReport struct {
	SchemaVersion   string                `json:"schema_version"`
	Lane            string                `json:"lane"`
	Commit          string                `json:"commit,omitempty"`
	DatasetVersion  string                `json:"dataset_version"`
	DatasetHash     string                `json:"dataset_hash"`
	FingerprintNote string                `json:"fingerprint_note"`
	Disposition     string                `json:"disposition"` // NO-GO | LIMITED-GO | MORE-DATA
	DispositionNote string                `json:"disposition_note"`
	HardGateEnabled bool                  `json:"hard_gate_enabled"`
	Metrics         ChallengeMetrics      `json:"metrics"`
	Results         []ChallengeCaseResult `json:"results"`
}
