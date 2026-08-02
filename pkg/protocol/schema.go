package protocol

import (
	"encoding/json"
	"time"
)

// AgentSession represents an active or historical agent supervision session.
type AgentSession struct {
	SessionID        string            `json:"session_id" redact:"none"`
	AgentID          string            `json:"agent_id" redact:"none"`
	AdapterType      string            `json:"adapter_type" redact:"none"`
	IntegrationLevel int               `json:"integration_level" redact:"none"`
	WorkspacePath    string            `json:"workspace_path" redact:"path"`
	Status           string            `json:"status" redact:"none"`
	StartedAt        time.Time         `json:"started_at" redact:"none"`
	EndedAt          *time.Time        `json:"ended_at,omitempty" redact:"none"`
	Metadata         map[string]string `json:"metadata,omitempty" redact:"sanitize"`
}

// TaskEnvelope is the immutable user request surface (original prompt + hard scope/timeout).
// Do not revise in place when the user appends constraints — open a new envelope or
// revise TaskContract instead. See docs/specs/adaptive_task_supervisor.md.
type TaskEnvelope struct {
	TaskID         string    `json:"task_id" redact:"none"`
	SessionID      string    `json:"session_id" redact:"none"`
	Prompt         string    `json:"prompt" redact:"sensitive"`
	ScopeWhitelist []string  `json:"scope_whitelist,omitempty" redact:"path"`
	MaxDepth       int       `json:"max_depth" redact:"none"`
	TimeoutSeconds int       `json:"timeout_seconds" redact:"none"`
	CreatedAt      time.Time `json:"created_at" redact:"none"`
}

// TaskSubmitted is the harness-agnostic task intake event (core model).
// Adapters map host-specific surfaces onto this type, e.g.:
//
//	Claude Code UserPromptSubmit → TaskSubmitted
//	Codex user input / API task payload / CLI initial prompt → TaskSubmitted
//
// Core packages must not depend on host hook names.
type TaskSubmitted struct {
	TaskID         string    `json:"task_id" redact:"none"`
	SessionID      string    `json:"session_id" redact:"none"`
	Prompt         string    `json:"prompt" redact:"sensitive"`
	ParentRevision int       `json:"parent_revision" redact:"none"`
	SubmittedAt    time.Time `json:"submitted_at" redact:"none"`
	SourceHint     string    `json:"source_hint,omitempty" redact:"none"` // adapter label only, not a core enum of host hooks
}

// Criterion is one success criterion on a TaskContract.
type Criterion struct {
	ID          string `json:"id" redact:"none"`
	Description string `json:"description" redact:"sensitive"`
}

// EvidenceRequirement names required proof for a criterion or risk class.
type EvidenceRequirement struct {
	ID       string `json:"id" redact:"none"`
	Kind     string `json:"kind" redact:"none"` // test, diff, lint, manual, checkpoint, ...
	Required bool   `json:"required" redact:"none"`
}

// ValidationBudget bounds redundant validation work.
type ValidationBudget struct {
	MaxFullSuiteRuns int `json:"max_full_suite_runs" redact:"none"`
	MaxTargetedRuns  int `json:"max_targeted_runs" redact:"none"`
}

// ToolBudget bounds tool invocations for a contract revision.
type ToolBudget struct {
	MaxToolCalls int `json:"max_tool_calls" redact:"none"`
}

// TaskContract is a revisioned workload/evidence budget derived at intake (and revisable).
// First M2 repeated-failure slice may pass nil/default contracts into policy APIs.
type TaskContract struct {
	TaskID   string `json:"task_id" redact:"none"`
	Revision int    `json:"revision" redact:"none"`

	Complexity string  `json:"complexity" redact:"none"` // trivial, simple, normal, complex
	Risk       string  `json:"risk" redact:"none"`       // low, medium, high, irreversible
	Confidence float64 `json:"confidence" redact:"none"`

	SuccessCriteria  []Criterion           `json:"success_criteria,omitempty" redact:"sanitize"`
	RequiredEvidence []EvidenceRequirement `json:"required_evidence,omitempty" redact:"sanitize"`
	AllowedScope     []string              `json:"allowed_scope,omitempty" redact:"path"`
	ValidationBudget ValidationBudget      `json:"validation_budget" redact:"none"`
	ToolBudget       ToolBudget            `json:"tool_budget" redact:"none"`
	SubagentBudget   int                   `json:"subagent_budget" redact:"none"`
	ReviewerBudget   int                   `json:"reviewer_budget" redact:"none"`

	CreatedFrom string    `json:"created_from" redact:"none"` // user_explicit, repository_policy, heuristic, model
	CreatedAt   time.Time `json:"created_at" redact:"none"`
}

// CriterionStatus tracks whether a success criterion has been proven.
type CriterionStatus struct {
	CriterionID string `json:"criterion_id" redact:"none"`
	Status      string `json:"status" redact:"none"` // unmet, met, waived
}

// ValidationRecord is one validation attempt with a multi-part fingerprint.
// Fingerprint inputs (normative): command + target scope + workspace revision
// + task contract revision + validation purpose — not command alone.
type ValidationRecord struct {
	RecordID         string    `json:"record_id" redact:"none"`
	Command          string    `json:"command" redact:"sensitive"`
	TargetScope      []string  `json:"target_scope,omitempty" redact:"path"`
	WorkspaceRev     string    `json:"workspace_revision" redact:"none"`
	ContractRevision int       `json:"contract_revision" redact:"none"`
	Purpose          string    `json:"purpose" redact:"none"`
	Succeeded        bool      `json:"succeeded" redact:"none"`
	Fingerprint      string    `json:"fingerprint" redact:"none"`
	RecordedAt       time.Time `json:"recorded_at" redact:"none"`
}

// EvidenceLedger records what was actually proven for a task/contract revision.
// First M2 slice may pass nil/default ledgers into policy APIs.
type EvidenceLedger struct {
	TaskID            string                     `json:"task_id" redact:"none"`
	ContractRevision  int                        `json:"contract_revision" redact:"none"`
	WorkspaceRevision string                     `json:"workspace_revision,omitempty" redact:"none"`
	CriteriaStatus    map[string]CriterionStatus `json:"criteria_status,omitempty" redact:"sanitize"`
	ValidationRecords []ValidationRecord         `json:"validation_records,omitempty" redact:"sanitize"`
	ToolCallCounts    map[string]int             `json:"tool_call_counts,omitempty" redact:"none"`
	LastProgressAt    *time.Time                 `json:"last_progress_at,omitempty" redact:"none"`
	LastWorkspaceHash string                     `json:"last_workspace_hash,omitempty" redact:"none"`
}

// SafeBoundary is an adapter-declared delivery boundary (core names, not host hooks).
type SafeBoundary string

const (
	BoundaryBeforeTool SafeBoundary = "before_tool"
	BoundaryAfterTool  SafeBoundary = "after_tool"
	BoundaryTurnEnd    SafeBoundary = "turn_end"
	BoundaryNextInput  SafeBoundary = "next_input"
)

// AckPolicy selects how strongly an intervention requires acknowledgement.
type AckPolicy string

const (
	AckExplicit    AckPolicy = "explicit"          // agent_ack required (#71 fake slice)
	AckTransport   AckPolicy = "transport_receipt" // harness accepted message
	AckBehavioral  AckPolicy = "behavioral"        // next actions match intent
	AckNone        AckPolicy = "none"              // no ack; human escalate if critical
)

// AgentEvent is the canonical NDJSON event wrapper containing sequence numbers and dynamic payload object.
type AgentEvent struct {
	EventID     string          `json:"event_id" redact:"none"`
	SessionID   string          `json:"session_id" redact:"none"`
	SequenceNum int64           `json:"sequence_num" redact:"none"`
	EventType   string          `json:"event_type" redact:"none"`
	Timestamp   time.Time       `json:"timestamp" redact:"none"`
	Payload     json.RawMessage `json:"payload" redact:"sanitize"`
}

// ToolCallEvent contains details of a tool invocation requested or completed by target agent.
type ToolCallEvent struct {
	ToolCallID string         `json:"tool_call_id" redact:"none"`
	ToolName   string         `json:"tool_name" redact:"none"`
	Arguments  map[string]any `json:"arguments" redact:"sensitive"`
	Output     string         `json:"output,omitempty" redact:"sensitive"`
	ExitCode   *int           `json:"exit_code,omitempty" redact:"none"`
	DurationMs int64          `json:"duration_ms" redact:"none"`
	Error      string         `json:"error,omitempty" redact:"sensitive"`
}

// FileChangeEvent records workspace file mutation, line additions/deletions, and scope check.
type FileChangeEvent struct {
	FilePath         string `json:"file_path" redact:"path"`
	ChangeType       string `json:"change_type" redact:"none"`
	LinesAdded       int    `json:"lines_added" redact:"none"`
	LinesRemoved     int    `json:"lines_removed" redact:"none"`
	NetDiff          string `json:"net_diff,omitempty" redact:"sensitive"`
	IsScopeViolation bool   `json:"is_scope_violation" redact:"none"`
}

// TestResultEvent captures unit/integration test execution results and pass/fail counters.
type TestResultEvent struct {
	TestRunID     string `json:"test_run_id" redact:"none"`
	Command       string `json:"command" redact:"sensitive"`
	PassedCount   int    `json:"passed_count" redact:"none"`
	FailedCount   int    `json:"failed_count" redact:"none"`
	SkippedCount  int    `json:"skipped_count" redact:"none"`
	PassDelta     int    `json:"pass_delta" redact:"none"`
	FailureOutput string `json:"failure_output,omitempty" redact:"sensitive"`
	DurationMs    int64  `json:"duration_ms" redact:"none"`
}

// ErrorFingerprint is an invariant error signature for repeated error loop detection.
type ErrorFingerprint struct {
	FingerprintID   string    `json:"fingerprint_id" redact:"none"`
	RawError        string    `json:"raw_error" redact:"sensitive"`
	NormalizedText  string    `json:"normalized_text" redact:"sensitive"`
	OccurrenceCount int       `json:"occurrence_count" redact:"none"`
	FirstObservedAt time.Time `json:"first_observed_at" redact:"none"`
	LastObservedAt  time.Time `json:"last_observed_at" redact:"none"`
}

// EvidenceItem is an objective evidence snippet (log, diff, trace) captured for review.
type EvidenceItem struct {
	ItemID   string            `json:"item_id" redact:"none"`
	Source   string            `json:"source" redact:"none"`
	Category string            `json:"category" redact:"none"`
	Content  string            `json:"content" redact:"sensitive"`
	Metadata map[string]string `json:"metadata,omitempty" redact:"sanitize"`
}

// EvidencePack is a consolidated evidence bundle prepared by EvidencePackBuilder.
type EvidencePack struct {
	PackID             string         `json:"pack_id" redact:"none"`
	SessionID          string         `json:"session_id" redact:"none"`
	ForkTurns          string         `json:"fork_turns" redact:"none"`
	Items              []EvidenceItem `json:"items" redact:"sanitize"`
	ChurnRatio         float64        `json:"churn_ratio" redact:"none"`
	RepeatedErrorCount int            `json:"repeated_error_count" redact:"none"`
	CreatedAt          time.Time      `json:"created_at" redact:"none"`
}

// Hypothesis is a diagnostic hypothesis tracking proposed root cause and supporting evidence.
type Hypothesis struct {
	HypothesisID          string   `json:"hypothesis_id" redact:"none"`
	Statement             string   `json:"statement" redact:"sensitive"`
	Status                string   `json:"status" redact:"none"`
	SupportingEvidenceIDs []string `json:"supporting_evidence_ids,omitempty" redact:"none"`
	RefutingEvidenceIDs   []string `json:"refuting_evidence_ids,omitempty" redact:"none"`
	ConfidenceScore       float64  `json:"confidence_score" redact:"none"`
}

// Assumption is an explicit agent assumption regarding code or environment.
type Assumption struct {
	AssumptionID       string `json:"assumption_id" redact:"none"`
	Description        string `json:"description" redact:"sensitive"`
	IsVerified         bool   `json:"is_verified" redact:"none"`
	VerificationMethod string `json:"verification_method,omitempty" redact:"sensitive"`
	AuditVerdict       string `json:"audit_verdict,omitempty" redact:"none"`
}

// TunnelSignal is a raw score signal emitted by an individual anomaly detector.
type TunnelSignal struct {
	SignalID     string            `json:"signal_id" redact:"none"`
	SessionID    string            `json:"session_id" redact:"none"`
	DetectorName string            `json:"detector_name" redact:"none"`
	FailureMode  string            `json:"failure_mode" redact:"none"`
	Weight       float64           `json:"weight" redact:"none"`
	Score        float64           `json:"score" redact:"none"`
	Details      map[string]string `json:"details,omitempty" redact:"sensitive"`
	TriggeredAt  time.Time         `json:"triggered_at" redact:"none"`
}

// TunnelAssessment is an evaluated aggregate score and adjudication result.
type TunnelAssessment struct {
	AssessmentID       string         `json:"assessment_id" redact:"none"`
	SessionID          string         `json:"session_id" redact:"none"`
	AggregateScore     float64        `json:"aggregate_score" redact:"none"`
	PrimaryFailureMode string         `json:"primary_failure_mode" redact:"none"`
	IsTunnelDetected   bool           `json:"is_tunnel_detected" redact:"none"`
	RecommendedAction  string         `json:"recommended_action" redact:"none"`
	Signals            []TunnelSignal `json:"signals" redact:"sanitize"`
	EvaluatedAt        time.Time      `json:"evaluated_at" redact:"none"`
}

// ReviewRequest is a model review request payload submitted to a Reviewer Provider.
type ReviewRequest struct {
	RequestID      string    `json:"request_id" redact:"none"`
	ReviewerRole   string    `json:"reviewer_role" redact:"none"`
	EvidencePackID string    `json:"evidence_pack_id" redact:"none"`
	Model          string    `json:"model" redact:"none"`
	Prompt         string    `json:"prompt" redact:"sensitive"`
	RequestedAt    time.Time `json:"requested_at" redact:"none"`
}

// ReviewDecision is a structured classification result returned by a Reviewer Provider model.
type ReviewDecision struct {
	DecisionID       string    `json:"decision_id" redact:"none"`
	RequestID        string    `json:"request_id" redact:"none"`
	ReviewerRole     string    `json:"reviewer_role" redact:"none"`
	TunnelConfidence float64   `json:"tunnel_confidence" redact:"none"`
	Classification   string    `json:"classification" redact:"none"`
	Rationale        string    `json:"rationale" redact:"sensitive"`
	SuggestedAdvice  string    `json:"suggested_advice,omitempty" redact:"sensitive"`
	TokensUsed       int       `json:"tokens_used" redact:"none"`
	DecidedAt        time.Time `json:"decided_at" redact:"none"`
}

// Intervention represents a supervisor action executed against a target session.
// Delivery/ACK/expiry fields (#57) are optional; empty/zero values are backward compatible.
type Intervention struct {
	InterventionID     string     `json:"intervention_id" redact:"none"`
	SessionID          string     `json:"session_id" redact:"none"`
	Level              int        `json:"level" redact:"none"`
	ActionType         string     `json:"action_type" redact:"none"`
	AdvicePrompt       string     `json:"advice_prompt,omitempty" redact:"sensitive"`
	TargetCheckpointID string     `json:"target_checkpoint_id,omitempty" redact:"none"`
	Status             string     `json:"status" redact:"none"`
	ExecutedAt         time.Time  `json:"executed_at" redact:"none"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty" redact:"none"`
	Priority           int        `json:"priority,omitempty" redact:"none"`
	DeliveryModeHint   string     `json:"delivery_mode_hint,omitempty" redact:"none"`
	RequiresAck        bool       `json:"requires_ack,omitempty" redact:"none"`
	AckStatus          string     `json:"ack_status,omitempty" redact:"none"`
	AckedAt            *time.Time `json:"acked_at,omitempty" redact:"none"`
	SafeBoundary       string     `json:"safe_boundary,omitempty" redact:"none"`
	Fingerprint        string     `json:"fingerprint,omitempty" redact:"none"`
	ParentAssessmentID string     `json:"parent_assessment_id,omitempty" redact:"none"`
}

// BudgetState tracks cumulative token, cost USD, time, and intervention metrics.
type BudgetState struct {
	SessionID         string  `json:"session_id" redact:"none"`
	MaxTokens         int64   `json:"max_tokens" redact:"none"`
	UsedTokens        int64   `json:"used_tokens" redact:"none"`
	MaxCostUSD        float64 `json:"max_cost_usd" redact:"none"`
	CurrentCostUSD    float64 `json:"current_cost_usd" redact:"none"`
	MaxInterventions  int     `json:"max_interventions" redact:"none"`
	InterventionCount int     `json:"intervention_count" redact:"none"`
	IsExhausted       bool    `json:"is_exhausted" redact:"none"`
}

// CapabilityManifest declares capabilities by target agent adapter during session handshake.
// SupportsPause means harness-native pause only; OS SIGSTOP is not CapPause (#72).
type CapabilityManifest struct {
	AgentID                  string `json:"agent_id" redact:"none"`
	Version                  string `json:"version" redact:"none"`
	IntegrationLevel         int    `json:"integration_level" redact:"none"`
	SupportsEventStream      bool   `json:"supports_event_stream" redact:"none"`
	SupportsToolInspection   bool   `json:"supports_tool_inspection" redact:"none"`
	SupportsDiffInspection   bool   `json:"supports_diff_inspection" redact:"none"`
	SupportsCostTracking     bool   `json:"supports_cost_tracking" redact:"none"`
	SupportsHooks            bool   `json:"supports_hooks" redact:"none"`
	SupportsHeadless         bool   `json:"supports_headless" redact:"none"`
	SupportsCLIControl       bool   `json:"supports_cli_control" redact:"none"`
	SupportsPause            bool   `json:"supports_pause" redact:"none"`
	SupportsCancel           bool   `json:"supports_cancel" redact:"none"`
	SupportsResume           bool   `json:"supports_resume" redact:"none"`
	SupportsCheckpoint       bool   `json:"supports_checkpoint" redact:"none"`
	SupportsRollback         bool   `json:"supports_rollback" redact:"none"`
	SupportsMCP              bool   `json:"supports_mcp" redact:"none"`
	SupportsSubagents        bool   `json:"supports_subagents" redact:"none"`
	SupportsExtensions       bool   `json:"supports_extensions" redact:"none"`
	SupportsSwitchModel      bool   `json:"supports_switch_model" redact:"none"`
	SupportsCustomProvider   bool   `json:"supports_custom_provider" redact:"none"`
	SupportsOpenAICompat     bool   `json:"supports_openai_compat" redact:"none"`
	SupportsLocalModels      bool   `json:"supports_local_models" redact:"none"`
	SupportsSDK              bool   `json:"supports_sdk" redact:"none"`
	SupportsAdviceDelivery   bool   `json:"supports_advice_delivery" redact:"none"`
	SupportsContextInjection bool   `json:"supports_context_injection" redact:"none"`
	SupportsToolGate         bool   `json:"supports_tool_gate" redact:"none"`
	SupportsTurnBoundary     bool   `json:"supports_turn_boundary" redact:"none"`
	SupportsInterventionAck  bool   `json:"supports_intervention_ack" redact:"none"`
}

// Checkpoint represents a workspace Git commit hash and test status snapshot.
type Checkpoint struct {
	CheckpointID     string    `json:"checkpoint_id" redact:"none"`
	SessionID        string    `json:"session_id" redact:"none"`
	GitCommitHash    string    `json:"git_commit_hash" redact:"none"`
	BranchName       string    `json:"branch_name" redact:"none"`
	Description      string    `json:"description" redact:"sensitive"`
	PassingTestCount int       `json:"passing_test_count" redact:"none"`
	CreatedAt        time.Time `json:"created_at" redact:"none"`
}

// RollbackResult is an outcome record of Git workspace or state checkpoint restoration.
type RollbackResult struct {
	RollbackID         string    `json:"rollback_id" redact:"none"`
	SessionID          string    `json:"session_id" redact:"none"`
	TargetCheckpointID string    `json:"target_checkpoint_id" redact:"none"`
	PreviousCommitHash string    `json:"previous_commit_hash" redact:"none"`
	RestoredCommitHash string    `json:"restored_commit_hash" redact:"none"`
	Success            bool      `json:"success" redact:"none"`
	ErrorMessage       string    `json:"error_message,omitempty" redact:"sensitive"`
	CompletedAt        time.Time `json:"completed_at" redact:"none"`
}

// ProviderUsage tracks individual LLM API request usage metrics.
type ProviderUsage struct {
	UsageID          string    `json:"usage_id" redact:"none"`
	SessionID        string    `json:"session_id" redact:"none"`
	ProviderName     string    `json:"provider_name" redact:"none"`
	Model            string    `json:"model" redact:"none"`
	PromptTokens     int       `json:"prompt_tokens" redact:"none"`
	CompletionTokens int       `json:"completion_tokens" redact:"none"`
	TotalTokens      int       `json:"total_tokens" redact:"none"`
	EstimatedCostUSD float64   `json:"estimated_cost_usd" redact:"none"`
	LatencyMs        int64     `json:"latency_ms" redact:"none"`
	Timestamp        time.Time `json:"timestamp" redact:"none"`
}

// AuditRecord is an immutable event entry persisted to SQLite WAL audit database.
type AuditRecord struct {
	AuditID    string          `json:"audit_id" redact:"none"`
	SessionID  string          `json:"session_id" redact:"none"`
	Actor      string          `json:"actor" redact:"none"`
	Category   string          `json:"category" redact:"none"`
	Summary    string          `json:"summary" redact:"sensitive"`
	DetailJSON json.RawMessage `json:"detail_json,omitempty" redact:"sanitize"`
	RecordedAt time.Time       `json:"recorded_at" redact:"none"`
}
