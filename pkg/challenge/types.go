package challenge

import (
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// Closed schema versions (#131).
const (
	SchemaChallengeRecord = "reinframe.challenge_record.v1"
	SchemaJustification   = "reinframe.challenge_justification.v1"
	SchemaChallengeEvent  = "reinframe.challenge_event.v1"
	SchemaChallengeAudit  = "reinframe.challenge_audit.v1"
	SchemaCacheKeyInputs  = "reinframe.challenge_cache_key_inputs.v1"
)

// InitialRetryBudget is the durable one-shot retry budget (#131).
const InitialRetryBudget = 1

// ChallengeState is the closed state machine.
type ChallengeState string

const (
	StateOpen         ChallengeState = "OPEN"
	StateJustified    ChallengeState = "JUSTIFIED"
	StateRetryPending ChallengeState = "RETRY_PENDING"
	StateAllowedOnce  ChallengeState = "ALLOWED_ONCE"
	StateRejected     ChallengeState = "REJECTED"
	StateHumanReview  ChallengeState = "HUMAN_REVIEW"
	StateAbandoned    ChallengeState = "ABANDONED"
	StateExpired      ChallengeState = "EXPIRED"
)

// Intervention metadata layered on BLOCK (not a Stage 2 decision).
const (
	InterventionNone                = "none"
	InterventionAppealableChallenge = "APPEALABLE_CHALLENGE"
	InterventionHumanReview         = "HUMAN_REVIEW"
)

// Appealability class.
const (
	AppealAppealable    = "APPEALABLE"
	AppealNonAppealable = "NON_APPEALABLE"
	AppealHumanReview   = "HUMAN_REVIEW"
)

// Policy / block class codes used for appealability routing.
const (
	BlockClassScopeDrift          = "SCOPE_DRIFT"
	BlockClassOverSOP             = "OVER_SOP"
	BlockClassExpensiveHardening  = "EXPENSIVE_HARDENING"
	BlockClassRepeatedExploration = "REPEATED_EXPLORATION"
	BlockClassEvidenceGap         = "EVIDENCE_GAP"
	BlockClassSecretExfiltration  = "SECRET_EXFILTRATION"
	BlockClassExplicitDeny        = "EXPLICIT_DENY"
	BlockClassCrossWorkspace      = "CROSS_WORKSPACE"
	BlockClassProductionDeploy    = "PRODUCTION_DEPLOY"
	BlockClassPayment             = "PAYMENT"
	BlockClassRemoteDeletion      = "REMOTE_DELETION"
	BlockClassPermissionChange    = "PERMISSION_CHANGE"
	BlockClassUnknownSecurity     = "UNKNOWN_SECURITY"
	BlockClassProductivityGeneric = "PRODUCTIVITY_BLOCK"
)

// Side-effect classes for semantic fingerprinting (closed).
const (
	SideEffectNone         = "none"
	SideEffectDeleteTree   = "delete_tree"
	SideEffectDeleteFile   = "delete_file"
	SideEffectWriteFile    = "write_file"
	SideEffectShellGeneric = "shell_generic"
	SideEffectRead         = "read"
	SideEffectSearch       = "search"
	SideEffectTestSuite    = "test_suite"
	SideEffectGitMutate    = "git_mutate"
	SideEffectNetwork      = "network"
	SideEffectDeploy       = "deploy"
	SideEffectPayment      = "payment"
	SideEffectPermission   = "permission_change"
	SideEffectUnknown      = "unknown"
)

// SemanticRelationship between a retry ProposedAction and the original challenge.
const (
	RelSame         = "same"
	RelReducedScope = "reduced_scope"
	RelDifferent    = "different"
	RelBypass       = "bypass" // syntax rewrite attempting same effect under different surface
)

// Stage 2 decisions — re-export closed set for convenience; never add CHALLENGE.
const (
	DecisionAllow = classifier.DecisionAllow // ALLOW
	DecisionBlock = classifier.DecisionBlock // BLOCK
)

// ChallengeRecord is the closed, versioned durable challenge contract.
type ChallengeRecord struct {
	SchemaVersion      string         `json:"schema_version"`
	ChallengeID        string         `json:"challenge_id"`
	SessionID          string         `json:"session_id"`
	ActionFingerprint  string         `json:"action_fingerprint"`
	OriginalActionID   string         `json:"original_action_id"`
	PolicyClass        string         `json:"policy_class"`
	BlockClass         string         `json:"block_class"`
	ReasonCode         string         `json:"reason_code"`
	RequiredClaims     []string       `json:"required_claims,omitempty"`
	RetryBudget        int            `json:"retry_budget"`
	RetryBudgetInitial int            `json:"retry_budget_initial"`
	State              ChallengeState `json:"state"`
	Appealability      string         `json:"appealability"`
	// Intervention is none | APPEALABLE_CHALLENGE | HUMAN_REVIEW (not Stage 2).
	Intervention string `json:"intervention"`
	// Stage2Decision remains ALLOW|BLOCK only — always BLOCK while challenge is open workflow.
	Stage2Decision string `json:"stage2_decision"`

	WorkspaceRevision string   `json:"workspace_revision,omitempty"`
	ContractRevision  int      `json:"contract_revision,omitempty"`
	Branch            string   `json:"branch,omitempty"`
	SideEffectClass   string   `json:"side_effect_class,omitempty"`
	TargetResources   []string `json:"target_resources,omitempty"`

	PolicyVersion string `json:"policy_version,omitempty"`
	RulesetHash   string `json:"ruleset_hash,omitempty"`
	PolicyHash    string `json:"policy_hash,omitempty"`

	JustificationHash string `json:"justification_hash,omitempty"`
	// ConsumedRetryKey is the durable identity of the one-shot retry that was processed.
	// Empty means no retry has been finalized. Exact key match may idempotently replay.
	ConsumedRetryKey string `json:"consumed_retry_key,omitempty"`
	// OperationDigest is stored for relationship comparison across restarts.
	OperationDigest   string `json:"operation_digest,omitempty"`
	ExpiresAtSequence int64  `json:"expires_at_sequence,omitempty"`
	CreatedSequence   int64  `json:"created_sequence"`
	UpdatedSequence   int64  `json:"updated_sequence"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Justification is the closed external decision-summary submission.
// No private chain-of-thought fields are defined or accepted.
type Justification struct {
	SchemaVersion              string   `json:"schema_version"`
	ChallengeID                string   `json:"challenge_id"`
	ConcreteValue              string   `json:"concrete_value"`
	PreventedFailureOrThreat   string   `json:"prevented_failure_or_threat"`
	EstimatedCost              string   `json:"estimated_cost"`
	AlternativesConsidered     string   `json:"alternatives_considered"`
	ScopeLimit                 string   `json:"scope_limit"`
	VerificationPlan           string   `json:"verification_plan"`
	RollbackPlan               string   `json:"rollback_plan"`
	SupportingEvidenceEventIDs []string `json:"supporting_evidence_event_ids"`
}

// ChallengeEvent is an append-only transition record.
type ChallengeEvent struct {
	SchemaVersion string         `json:"schema_version"`
	Sequence      int64          `json:"sequence"`
	ChallengeID   string         `json:"challenge_id"`
	SessionID     string         `json:"session_id"`
	Type          string         `json:"type"`
	FromState     ChallengeState `json:"from_state,omitempty"`
	ToState       ChallengeState `json:"to_state"`
	// Correlation / causation (bounded ids only).
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
	// PayloadHash is a hash of event-specific bounded fields (no secrets).
	PayloadHash string    `json:"payload_hash,omitempty"`
	Note        string    `json:"note,omitempty"`
	At          time.Time `json:"at"`
}

// OpenRequest opens a challenge after a productivity BLOCK.
type OpenRequest struct {
	SessionID        string
	Proposed         adapter.ProposedAction
	BlockClass       string
	ReasonCode       string
	PolicyClass      string
	RequiredClaims   []string
	PolicyVersion    string
	RulesetHash      string
	Branch           string
	KnownEvidenceIDs []string
	// ExpiresAfterSequences: if >0, challenge expires when store sequence reaches created+N.
	ExpiresAfterSequences int64
	CorrelationID         string
}

// RetryRequest attempts one semantically equivalent retry.
type RetryRequest struct {
	ChallengeID string
	SessionID   string
	// Branch when non-empty must match the challenge binding (ownership).
	Branch string
	// RetryRequestID optional dedicated id for attempt identity (bounded).
	RetryRequestID string
	Proposed       adapter.ProposedAction
	CorrelationID  string
	// ReEval carries optional stage inputs; if nil, DefaultReEvaluator is used.
	ReEval *ReEvalContext
}

// RetryResult is the business outcome of AttemptRetry.
type RetryResult struct {
	Record           ChallengeRecord
	Stage2Decision   string // ALLOW | BLOCK only
	Intervention     string
	Relationship     string
	IdempotentReplay bool // true when duplicate concurrent/idempotent retry
	RejectedReason   string
}

// AuditRecord is a closed audit blob without secrets or unrestricted prompts.
type AuditRecord struct {
	SchemaVersion     string `json:"schema_version"`
	ChallengeID       string `json:"challenge_id"`
	SessionID         string `json:"session_id"`
	State             string `json:"state"`
	Stage2Decision    string `json:"stage2_decision"`
	Intervention      string `json:"intervention"`
	ActionFingerprint string `json:"action_fingerprint"`
	JustificationHash string `json:"justification_hash,omitempty"`
	PolicyHash        string `json:"policy_hash,omitempty"`
	RulesetHash       string `json:"ruleset_hash,omitempty"`
	InputHash         string `json:"input_hash,omitempty"`
	FingerprintHash   string `json:"fingerprint_hash,omitempty"`
}

// CacheKeyInputs are the identity/invalidation fields required by #131 for a
// future exact-assessment cache (#138). This package does not implement a cache.
type CacheKeyInputs struct {
	SchemaVersion     string `json:"schema_version"`
	SessionID         string `json:"session_id"`
	ChallengeID       string `json:"challenge_id,omitempty"`
	ChallengeState    string `json:"challenge_state,omitempty"`
	ActionFingerprint string `json:"action_fingerprint"`
	JustificationHash string `json:"justification_hash,omitempty"`
	EvidenceIDsHash   string `json:"evidence_ids_hash,omitempty"`
	ContractRevision  int    `json:"contract_revision,omitempty"`
	WorkspaceRevision string `json:"workspace_revision,omitempty"`
	RulesetHash       string `json:"ruleset_hash,omitempty"`
	PolicyHash        string `json:"policy_hash,omitempty"`
	ModelID           string `json:"model_id,omitempty"`
	PromptHash        string `json:"prompt_hash,omitempty"`
	// CanonicalKey is a stable hash of the above (no raw prompts/secrets).
	CanonicalKey string `json:"canonical_key"`
}
