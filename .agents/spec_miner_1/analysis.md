# Reinframe Protocol Specification & Canonical Data Structures Analysis

## 1. Executive Summary & Specification Context

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

This document serves as the authoritative protocol specification mined from repository documentation (`ORIGINAL_REQUEST.md`, `mvp_scope_and_non_goals.md`, `dag_and_execution_plan.md`, `anti_tunnel_threat_model.md`, `harness_capability_matrix.md`, `001_external_supervisor_vs_extension.md`, `002_core_language_db_ipc.md`).

It enumerates the 22 canonical data structures, their Go struct definitions, JSON tags, redaction metadata tags, field types, JSON Schema specifications, and edge case behaviors.

---

## 2. Features Discovered

## Features Discovered
| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Session & Harness | `AgentSession` | Manages target agent process lifetime, adapter mode, integration level, and state transitions | SessionID, AgentID, AdapterType, IntegrationLevel, WorkspacePath | Active Session State, Session Status | Returns error on invalid transition or unknown adapter type | `ORIGINAL_REQUEST.md`, `mvp_scope_and_non_goals.md` |
| 2 | Session & Harness | `TaskEnvelope` | Encloses task prompt, scope whitelist globs, timeout, and max subagent depth | TaskID, Prompt, ScopeWhitelist, MaxDepth, TimeoutSeconds | Task execution context envelope | Rejects task if prompt empty or timeout non-positive | `ORIGINAL_REQUEST.md`, `dag_and_execution_plan.md` |
| 3 | Event Stream | `AgentEvent` | Universal NDJSON event wrapper containing sequence numbers and typed payloads | EventID, SessionID, SequenceNum, EventType, Payload | Standardized audit/stream event | Fails schema validation if payload type mismatch | `ORIGINAL_REQUEST.md`, `002_core_language_db_ipc.md` |
| 4 | Telemetry & Events | `ToolCallEvent` | Tracks target agent tool invocations, arguments, output, exit code, and execution time | ToolCallID, ToolName, Arguments, Output, ExitCode, DurationMs | Serialized tool telemetry record | Flags execution errors when ExitCode != 0 or Error set | `harness_capability_matrix.md` |
| 5 | Telemetry & Events | `FileChangeEvent` | Records workspace file creation, modification, deletion, line diffs, and scope bounds | FilePath, ChangeType, LinesAdded, LinesRemoved, NetDiff | Workspace diff record | Flags `IsScopeViolation=true` if path outside whitelist | `anti_tunnel_threat_model.md` (FM-4) |
| 6 | Telemetry & Events | `TestResultEvent` | Captures test runner execution, pass/fail counts, pass delta, and failure traces | TestRunID, Command, PassedCount, FailedCount, PassDelta, FailureOutput | Evaluated test metrics record | Triggers FM-1/FM-3 signals if pass delta <= 0 | `mvp_scope_and_non_goals.md` |
| 7 | Detection & Intelligence | `ErrorFingerprint` | Normalizes and hashes compiler/runtime errors to detect fixated error loops | RawError, NormalizedText, OccurrenceCount | Fingerprint hash ID & loop count | Triggers SUSPECT state when OccurrenceCount >= 3 | `anti_tunnel_threat_model.md` (FM-1) |
| 8 | Detection & Intelligence | `EvidenceItem` | Standardized objective snippet (diff, trace, log) extracted for model evaluation | Source, Category, Content, Metadata | Evidence item struct | Sanitizes sensitive paths/keys before review | `mvp_scope_and_non_goals.md` |
| 9 | Detection & Intelligence | `EvidencePack` | Bundled evidence items, churn ratio, and error counts constructed for reviewers | SessionID, Items, ChurnRatio, RepeatedErrorCount | Reviewer input context pack | Fails build if items array is empty | `mvp_scope_and_non_goals.md` |
| 10 | Detection & Intelligence | `Hypothesis` | Diagnostic hypothesis statement tracking supporting/refuting evidence and confidence | Statement, Status, SupportingEvidenceIDs, RefutingEvidenceIDs | Evaluated hypothesis record | Refuted status set if contradictory evidence found | `anti_tunnel_threat_model.md` (FM-2, FM-5) |
| 11 | Detection & Intelligence | `Assumption` | Tracks explicit agent environment/code assumptions and audit verdicts | Description, IsVerified, VerificationMethod, AuditVerdict | Verified assumption record | Verdict set to CONTRADICTED if check fails | `anti_tunnel_threat_model.md` (FM-2) |
| 12 | Detection & Intelligence | `TunnelSignal` | Raw anomaly score output from individual detectors or model classifiers | DetectorName, FailureMode, Weight, Score, Details | Anomaly signal record | Signal ignored if weight or score out of [0,1] range | `anti_tunnel_threat_model.md` §3 |
| 13 | Detection & Intelligence | `TunnelAssessment` | Aggregated weighted tunnel confidence score and adjudication decision | SessionID, AggregateScore, PrimaryFailureMode, IsTunnelDetected | Adjudication decision state | Triggers ZOOM_OUT / PAUSE / ROLLBACK if score >= 0.85 | `anti_tunnel_threat_model.md` §3 |
| 14 | Reviewer & AI | `ReviewRequest` | Request sent to reviewer provider with prompt and evidence pack reference | ReviewerRole, EvidencePackID, Model, Prompt | Provider request envelope | Returns error on provider connection/auth failure | `mvp_scope_and_non_goals.md` |
| 15 | Reviewer & AI | `ReviewDecision` | Structured JSON response from model classifier with confidence and rationale | TunnelConfidence, Classification, Rationale, SuggestedAdvice | Structured model classification | Falls back to default decision on JSON parse failure | `mvp_scope_and_non_goals.md` |
| 16 | Policy & Governance | `Intervention` | Actions taken against target session (Advisory prompt, Pause process, Rollback) | Level, ActionType, AdvicePrompt, TargetCheckpointID | Executed intervention record | Halts on unhandled execution error or invalid level | `anti_tunnel_threat_model.md` §4 |
| 17 | Policy & Governance | `BudgetState` | Monitors cumulative token usage, cost USD, time, and max intervention limits | MaxTokens, UsedTokens, MaxCostUSD, CurrentCostUSD, MaxInterventions | Session budget status | Flags `IsExhausted=true` and pauses on threshold breach | `dag_and_execution_plan.md` (Issue #36) |
| 18 | Session & Harness | `CapabilityManifest` | Handshake capabilities declared by target agent adapter during session init | AgentID, IntegrationLevel, SupportsPause, SupportsCancel, SupportsRollback | Negotiated session capabilities | Downgrades intervention level to supported max | `dag_and_execution_plan.md` (Issue #7) |
| 19 | Recovery & State | `Checkpoint` | Workspace Git commit hash and test count snapshot for session recovery | SessionID, GitCommitHash, BranchName, PassingTestCount | Stored checkpoint snapshot | Fails if Git repository status is unclean or corrupt | `mvp_scope_and_non_goals.md` |
| 20 | Recovery & State | `RollbackResult` | Outcome record of a Git workspace or state checkpoint restoration | TargetCheckpointID, PreviousCommitHash, RestoredCommitHash, Success | Rollback verification status | Sets `Success=false` and logs error message on Git error | `anti_tunnel_threat_model.md` §4 |
| 21 | Telemetry & Events | `ProviderUsage` | Individual LLM API request usage tracking tokens, cost USD, and latency ms | ProviderName, Model, PromptTokens, CompletionTokens, LatencyMs | Usage telemetry record | Logs warning if cost calculation unavailable | `harness_capability_matrix.md` |
| 22 | Observability & Audit | `AuditRecord` | Immutably logged event stored in SQLite WAL event store for post-mortem audit | Actor, Category, Summary, DetailJSON, RecordedAt | SQLite WAL audit entry | Fails transaction if SQLite write lock fails | `002_core_language_db_ipc.md` |

---

## 3. Edge Cases & Boundary Conditions

## Edge Cases
| # | Feature | Input | Observed Behavior |
|---|---------|-------|-------------------|
| 1 | `ErrorFingerprint` | Raw error containing dynamic timestamps (e.g. `2026-08-02 13:25:00 [ERROR] line 42`) | Regex normalization strips timestamps, line numbers, and file paths to match invariant fingerprint across turns. |
| 2 | `FileChangeEvent` | File edit targeting path outside `ScopeWhitelist` (e.g. `../../etc/passwd` or `scripts/deploy.sh`) | `IsScopeViolation` is set to `true`, raising a Scope Drift (`FM-4`) signal in `TunnelSignal`. |
| 3 | `TunnelAssessment` | Aggregate score calculation resulting in exact threshold boundary value `0.8500` | `IsTunnelDetected` evaluates to `true`, triggering `RecommendedAction = "ADVISORY_ZOOM_OUT"` or higher. |
| 4 | `Intervention` | Level 3 `GIT_ROLLBACK` requested when target agent adapter supports only Level 1 (`SupportsRollback = false`) | Adjudicator policy engine degrades action to highest supported level (Level 1 Advisory `/zoom-out` prompt). |
| 5 | `AgentEvent` | NDJSON stream parser receives malformed or truncated JSON payload byte string | `ValidateEvent` returns `ErrMalformedJSON`, event is logged as corrupted in `AuditRecord`, and stream resumes. |
| 6 | `EvidencePack` | Agent workspace has no file changes (`LinesAdded = 0`, `LinesRemoved = 0`) but test fails | `EvidencePack` builds zero-diff pack with `ChurnRatio = 0.0`, isolating error output in `EvidenceItem`. |
| 7 | `BudgetState` | Session reaches `MaxTokens` limit mid-tool execution | `IsExhausted` becomes `true`, trigger supervisor sends `SIGINT` (or `PAUSE_PROCESS`) to target agent. |
| 8 | `ReviewDecision` | Reviewer LLM returns markdown wrapped block instead of pure JSON object | JSON schema parser strips markdown backticks (` ```json ... ``` `) before schema validation. |
| 9 | `Checkpoint` | Workspace contains uncommitted untracked files during checkpoint creation | Checkpoint manager executes `git add -A && git commit -m "reinframe: checkpoint <ID>"` to guarantee reproducible state. |
| 10 | `RollbackResult` | Target checkpoint Git commit hash no longer exists in repository history | `RollbackResult` sets `Success = false`, records `ErrorMessage`, and flags session state as `SUSPECT`. |
| 11 | `CapabilityManifest` | Target agent provides unknown integration level (e.g., `Level = 99`) | Handshake validator rejects manifest with `ErrInvalidIntegrationLevel` and defaults session to Level 0 (Observe-only). |
| 12 | `AuditRecord` | DetailJSON contains sensitive API keys or auth tokens (`sk-proj-...`) | Redaction engine recursively applies `redact:"sensitive"` regex before committing to SQLite WAL. |

---

## 4. Enumeration of 22 Canonical Data Structures

Below is the exhaustive specification of all 22 canonical Go structs, field names, Go types, JSON tags, redaction tags, and constraints.

### 1. `AgentSession`
Go Struct Name: `AgentSession`
Description: Represents an active or historical agent supervision session.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Non-empty, UUID format recommended |
| `AgentID` | `string` | `json:"agent_id"` | `redact:"none"` | Non-empty string (e.g. "claude-code", "codex-cli") |
| `AdapterType` | `string` | `json:"adapter_type"` | `redact:"none"` | Enum: `log_observer`, `cli_process`, `native_subagent`, `mcp_bridge` |
| `IntegrationLevel` | `int` | `json:"integration_level"` | `redact:"none"` | Value in range `[0, 3]` |
| `WorkspacePath` | `string` | `json:"workspace_path"` | `redact:"path"` | Valid absolute filesystem path string |
| `Status` | `string` | `json:"status"` | `redact:"none"` | Enum: `OBSERVE`, `EXECUTE`, `AUDIT`, `SUSPECT`, `ZOOM_OUT`, `PAUSED`, `TERMINATED`, `COMPLETED` |
| `StartedAt` | `time.Time` | `json:"started_at"` | `redact:"none"` | RFC 3339 formatted timestamp |
| `EndedAt` | `*time.Time` | `json:"ended_at,omitempty"` | `redact:"none"` | Optional/nullable RFC 3339 timestamp |
| `Metadata` | `map[string]string` | `json:"metadata,omitempty"` | `redact:"sanitize"` | Key-value map of session environment metadata |

---

### 2. `TaskEnvelope`
Go Struct Name: `TaskEnvelope`
Description: Envelope specifying task prompt, scope rules, and timeout constraints.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `TaskID` | `string` | `json:"task_id"` | `redact:"none"` | Non-empty unique task ID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Matching parent AgentSession SessionID |
| `Prompt` | `string` | `json:"prompt"` | `redact:"sensitive"` | Non-empty instruction prompt string |
| `ScopeWhitelist` | `[]string` | `json:"scope_whitelist,omitempty"` | `redact:"path"` | Slice of glob path strings (e.g. `["pkg/**", "cmd/**"]`) |
| `MaxDepth` | `int` | `json:"max_depth"` | `redact:"none"` | Min 1, default 1 for M1 |
| `TimeoutSeconds` | `int` | `json:"timeout_seconds"` | `redact:"none"` | Must be > 0 |
| `CreatedAt` | `time.Time` | `json:"created_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 3. `AgentEvent`
Go Struct Name: `AgentEvent`
Description: Canonical NDJSON event wrapper containing sequence numbers and dynamic payload object.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `EventID` | `string` | `json:"event_id"` | `redact:"none"` | Non-empty unique event UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Non-empty session ID |
| `SequenceNum` | `int64` | `json:"sequence_num"` | `redact:"none"` | Monotonically increasing integer >= 1 |
| `EventType` | `string` | `json:"event_type"` | `redact:"none"` | Enum: `tool_call`, `file_change`, `test_result`, `error_fingerprint`, `signal`, `assessment`, `intervention`, `checkpoint`, `budget`, `audit` |
| `Timestamp` | `time.Time` | `json:"timestamp"` | `redact:"none"` | RFC 3339 timestamp |
| `Payload` | `json.RawMessage` | `json:"payload"` | `redact:"sanitize"` | Raw JSON payload object matching `EventType` schema |

---

### 4. `ToolCallEvent`
Go Struct Name: `ToolCallEvent`
Description: Details of tool invocation requested or completed by target agent.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `ToolCallID` | `string` | `json:"tool_call_id"` | `redact:"none"` | Non-empty tool call UUID |
| `ToolName` | `string` | `json:"tool_name"` | `redact:"none"` | Non-empty tool identifier (e.g., "Bash", "ViewFile") |
| `Arguments` | `map[string]any` | `json:"arguments"` | `redact:"sensitive"` | Structured tool parameters map |
| `Output` | `string` | `json:"output,omitempty"` | `redact:"sensitive"` | Tool stdio output string |
| `ExitCode` | `*int` | `json:"exit_code,omitempty"` | `redact:"none"` | Optional exit code (integer) |
| `DurationMs` | `int64` | `json:"duration_ms"` | `redact:"none"` | Non-negative duration integer |
| `Error` | `string` | `json:"error,omitempty"` | `redact:"sensitive"` | Optional error message string |

---

### 5. `FileChangeEvent`
Go Struct Name: `FileChangeEvent`
Description: Records workspace file mutation, line additions/deletions, and scope check.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `FilePath` | `string` | `json:"file_path"` | `redact:"path"` | Relative path to workspace file |
| `ChangeType` | `string` | `json:"change_type"` | `redact:"none"` | Enum: `CREATED`, `MODIFIED`, `DELETED` |
| `LinesAdded` | `int` | `json:"lines_added"` | `redact:"none"` | Non-negative integer |
| `LinesRemoved` | `int` | `json:"lines_removed"` | `redact:"none"` | Non-negative integer |
| `NetDiff` | `string` | `json:"net_diff,omitempty"` | `redact:"sensitive"` | Git unified diff string |
| `IsScopeViolation` | `bool` | `json:"is_scope_violation"` | `redact:"none"` | True if file path outside `ScopeWhitelist` |

---

### 6. `TestResultEvent`
Go Struct Name: `TestResultEvent`
Description: Unit/integration test execution results and pass/fail counters.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `TestRunID` | `string` | `json:"test_run_id"` | `redact:"none"` | Unique test run identifier |
| `Command` | `string` | `json:"command"` | `redact:"sensitive"` | Test execution command string |
| `PassedCount` | `int` | `json:"passed_count"` | `redact:"none"` | Non-negative count |
| `FailedCount` | `int` | `json:"failed_count"` | `redact:"none"` | Non-negative count |
| `SkippedCount` | `int` | `json:"skipped_count"` | `redact:"none"` | Non-negative count |
| `PassDelta` | `int` | `json:"pass_delta"` | `redact:"none"` | Difference from baseline (positive/negative/zero) |
| `FailureOutput` | `string` | `json:"failure_output,omitempty"` | `redact:"sensitive"` | Raw stack trace or error log |
| `DurationMs` | `int64` | `json:"duration_ms"` | `redact:"none"` | Non-negative duration integer |

---

### 7. `ErrorFingerprint`
Go Struct Name: `ErrorFingerprint`
Description: Invariant error signature for repeated error loop detection.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `FingerprintID` | `string` | `json:"fingerprint_id"` | `redact:"none"` | SHA256 hex string of normalized text |
| `RawError` | `string` | `json:"raw_error"` | `redact:"sensitive"` | Original raw error string |
| `NormalizedText` | `string` | `json:"normalized_text"` | `redact:"sensitive"` | Stripped/normalized invariant error string |
| `OccurrenceCount` | `int` | `json:"occurrence_count"` | `redact:"none"` | Must be >= 1 |
| `FirstObservedAt` | `time.Time` | `json:"first_observed_at"` | `redact:"none"` | RFC 3339 timestamp |
| `LastObservedAt` | `time.Time` | `json:"last_observed_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 8. `EvidenceItem`
Go Struct Name: `EvidenceItem`
Description: Objective evidence snippet (log, diff, trace) captured for review.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `ItemID` | `string` | `json:"item_id"` | `redact:"none"` | Non-empty item UUID |
| `Source` | `string` | `json:"source"` | `redact:"none"` | Enum: `git_diff`, `stderr`, `stdout`, `test_runner`, `file_system` |
| `Category` | `string` | `json:"category"` | `redact:"none"` | Enum: `ERROR_TRACE`, `DIFF_CHURN`, `SCOPE_DRIFT`, `TEST_FAILURE` |
| `Content` | `string` | `json:"content"` | `redact:"sensitive"` | Verbatim text content snippet |
| `Metadata` | `map[string]string` | `json:"metadata,omitempty"` | `redact:"sanitize"` | Context metadata key-values |

---

### 9. `EvidencePack`
Go Struct Name: `EvidencePack`
Description: Consolidated evidence bundle prepared by `EvidencePackBuilder`.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `PackID` | `string` | `json:"pack_id"` | `redact:"none"` | Unique pack UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Session ID |
| `ForkTurns` | `string` | `json:"fork_turns"` | `redact:"none"` | Fork strategy string ("none" for M1) |
| `Items` | `[]EvidenceItem` | `json:"items"` | `redact:"sanitize"` | Array of EvidenceItem objects |
| `ChurnRatio` | `float64` | `json:"churn_ratio"` | `redact:"none"` | Non-negative ratio (`edited_lines / net_changed_lines`) |
| `RepeatedErrorCount` | `int` | `json:"repeated_error_count"` | `redact:"none"` | Non-negative integer count |
| `CreatedAt` | `time.Time` | `json:"created_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 10. `Hypothesis`
Go Struct Name: `Hypothesis`
Description: Diagnostic hypothesis tracking proposed root cause and supporting evidence.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `HypothesisID` | `string` | `json:"hypothesis_id"` | `redact:"none"` | Non-empty unique UUID |
| `Statement` | `string` | `json:"statement"` | `redact:"sensitive"` | Hypothesis description |
| `Status` | `string` | `json:"status"` | `redact:"none"` | Enum: `PROPOSED`, `CONFIRMED`, `REFUTED`, `DISCARDED` |
| `SupportingEvidenceIDs` | `[]string` | `json:"supporting_evidence_ids,omitempty"` | `redact:"none"` | Array of EvidenceItem ItemIDs |
| `RefutingEvidenceIDs` | `[]string` | `json:"refuting_evidence_ids,omitempty"` | `redact:"none"` | Array of EvidenceItem ItemIDs |
| `ConfidenceScore` | `float64` | `json:"confidence_score"` | `redact:"none"` | Range `[0.0, 1.0]` |

---

### 11. `Assumption`
Go Struct Name: `Assumption`
Description: Explicit agent assumption regarding code or environment.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `AssumptionID` | `string` | `json:"assumption_id"` | `redact:"none"` | Unique assumption UUID |
| `Description` | `string` | `json:"description"` | `redact:"sensitive"` | Assumption text statement |
| `IsVerified` | `bool` | `json:"is_verified"` | `redact:"none"` | Boolean flag |
| `VerificationMethod` | `string` | `json:"verification_method,omitempty"` | `redact:"sensitive"` | Verification command/procedure description |
| `AuditVerdict` | `string` | `json:"audit_verdict,omitempty"` | `redact:"none"` | Enum: `VALIDATED`, `UNSUBSTANTIATED`, `CONTRADICTED` |

---

### 12. `TunnelSignal`
Go Struct Name: `TunnelSignal`
Description: Raw score signal emitted by individual anomaly detector.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `SignalID` | `string` | `json:"signal_id"` | `redact:"none"` | Unique signal UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Target session ID |
| `DetectorName` | `string` | `json:"detector_name"` | `redact:"none"` | Detector name string |
| `FailureMode` | `string` | `json:"failure_mode"` | `redact:"none"` | Enum: `FM-1`, `FM-2`, `FM-3`, `FM-4`, `FM-5`, `FM-6` |
| `Weight` | `float64` | `json:"weight"` | `redact:"none"` | Signal weight in `[0.0, 1.0]` |
| `Score` | `float64` | `json:"score"` | `redact:"none"` | Normalized score in `[0.0, 1.0]` |
| `Details` | `map[string]string` | `json:"details,omitempty"` | `redact:"sensitive"` | Context key-value pairs |
| `TriggeredAt` | `time.Time` | `json:"triggered_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 13. `TunnelAssessment`
Go Struct Name: `TunnelAssessment`
Description: Evaluated aggregate score and adjudication result.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `AssessmentID` | `string` | `json:"assessment_id"` | `redact:"none"` | Assessment UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Session ID |
| `AggregateScore` | `float64` | `json:"aggregate_score"` | `redact:"none"` | Range `[0.0, 1.0]` |
| `PrimaryFailureMode` | `string` | `json:"primary_failure_mode"` | `redact:"none"` | Enum: `FM-1`, `FM-2`, `FM-3`, `FM-4`, `FM-5`, `FM-6`, `NONE` |
| `IsTunnelDetected` | `bool` | `json:"is_tunnel_detected"` | `redact:"none"` | True if `AggregateScore >= 0.85` |
| `RecommendedAction` | `string` | `json:"recommended_action"` | `redact:"none"` | Enum: `NONE`, `ADVISORY_ZOOM_OUT`, `PAUSE_EXECUTION`, `GIT_ROLLBACK` |
| `Signals` | `[]TunnelSignal` | `json:"signals"` | `redact:"sanitize"` | Array of contributing TunnelSignal items |
| `EvaluatedAt` | `time.Time` | `json:"evaluated_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 14. `ReviewRequest`
Go Struct Name: `ReviewRequest`
Description: Model review request payload submitted to Reviewer Provider.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `RequestID` | `string` | `json:"request_id"` | `redact:"none"` | Request UUID |
| `ReviewerRole` | `string` | `json:"reviewer_role"` | `redact:"none"` | Enum: `TunnelClassifier`, `AssumptionAuditor`, `ContrarianReviewer`, `EvidenceVerifier` |
| `EvidencePackID` | `string` | `json:"evidence_pack_id"` | `redact:"none"` | Reference PackID |
| `Model` | `string` | `json:"model"` | `redact:"none"` | Requested LLM model name |
| `Prompt` | `string` | `json:"prompt"` | `redact:"sensitive"` | Combined system & user prompt |
| `RequestedAt` | `time.Time` | `json:"requested_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 15. `ReviewDecision`
Go Struct Name: `ReviewDecision`
Description: Structured classification result returned by Reviewer Provider model.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `DecisionID` | `string` | `json:"decision_id"` | `redact:"none"` | Decision UUID |
| `RequestID` | `string` | `json:"request_id"` | `redact:"none"` | Matching ReviewRequest RequestID |
| `ReviewerRole` | `string` | `json:"reviewer_role"` | `redact:"none"` | Reviewer role string |
| `TunnelConfidence` | `float64` | `json:"tunnel_confidence"` | `redact:"none"` | Range `[0.0, 1.0]` |
| `Classification` | `string` | `json:"classification"` | `redact:"none"` | Enum: `NORMAL_PROGRESS`, `TUNNEL_VISION`, `SCOPE_DRIFT`, `REPEATED_ERROR` |
| `Rationale` | `string` | `json:"rationale"` | `redact:"sensitive"` | Detailed justification text from model |
| `SuggestedAdvice` | `string` | `json:"suggested_advice,omitempty"` | `redact:"sensitive"` | Injected `/zoom-out` re-planning advice prompt |
| `TokensUsed` | `int` | `json:"tokens_used"` | `redact:"none"` | Total request token count |
| `DecidedAt` | `time.Time` | `json:"decided_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 16. `Intervention`
Go Struct Name: `Intervention`
Description: Supervisor action executed against target session.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `InterventionID` | `string` | `json:"intervention_id"` | `redact:"none"` | Intervention UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Target SessionID |
| `Level` | `int` | `json:"level"` | `redact:"none"` | Escalation level `[0, 3]` |
| `ActionType` | `string` | `json:"action_type"` | `redact:"none"` | Enum: `ZOOM_OUT_PROMPT`, `PAUSE_PROCESS`, `CANCEL_ACTION`, `GIT_ROLLBACK`, `TERMINATE_SESSION` |
| `AdvicePrompt` | `string` | `json:"advice_prompt,omitempty"` | `redact:"sensitive"` | Advisory prompt injected for Level 1 |
| `TargetCheckpointID` | `string` | `json:"target_checkpoint_id,omitempty"` | `redact:"none"` | CheckpointID target for Level 3 rollback |
| `Status` | `string` | `json:"status"` | `redact:"none"` | Enum: `PENDING`, `EXECUTED`, `FAILED`, `SUPPRESSED` |
| `ExecutedAt` | `time.Time` | `json:"executed_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 17. `BudgetState`
Go Struct Name: `BudgetState`
Description: Tracks cumulative token, cost USD, time, and intervention metrics.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Target SessionID |
| `MaxTokens` | `int64` | `json:"max_tokens"` | `redact:"none"` | Non-negative limit |
| `UsedTokens` | `int64` | `json:"used_tokens"` | `redact:"none"` | Consumed token counter |
| `MaxCostUSD` | `float64` | `json:"max_cost_usd"` | `redact:"none"` | Dollar budget cap |
| `CurrentCostUSD` | `float64` | `json:"current_cost_usd"` | `redact:"none"` | Current accumulated cost |
| `MaxInterventions` | `int` | `json:"max_interventions"` | `redact:"none"` | Max intervention count threshold |
| `InterventionCount` | `int` | `json:"intervention_count"` | `redact:"none"` | Interventions executed counter |
| `IsExhausted` | `bool` | `json:"is_exhausted"` | `redact:"none"` | True if any limit reached |

---

### 18. `CapabilityManifest`
Go Struct Name: `CapabilityManifest`
Description: Capabilities declared by target agent adapter during session handshake.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `AgentID` | `string` | `json:"agent_id"` | `redact:"none"` | Agent framework string |
| `Version` | `string` | `json:"version"` | `redact:"none"` | Agent version string |
| `IntegrationLevel` | `int` | `json:"integration_level"` | `redact:"none"` | Level in range `[0, 3]` |
| `SupportsPause` | `bool` | `json:"supports_pause"` | `redact:"none"` | SIGSTOP / process pause capability |
| `SupportsCancel` | `bool` | `json:"supports_cancel"` | `redact:"none"` | SIGINT / action cancel capability |
| `SupportsResume` | `bool` | `json:"supports_resume"` | `redact:"none"` | Process/session resume capability |
| `SupportsCheckpoint` | `bool` | `json:"supports_checkpoint"` | `redact:"none"` | Checkpoint creation capability |
| `SupportsRollback` | `bool` | `json:"supports_rollback"` | `redact:"none"` | Git workspace rollback capability |
| `SupportsMCP` | `bool` | `json:"supports_mcp"` | `redact:"none"` | Native MCP server support |

---

### 19. `Checkpoint`
Go Struct Name: `Checkpoint`
Description: Workspace Git commit hash and test status snapshot.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `CheckpointID` | `string` | `json:"checkpoint_id"` | `redact:"none"` | Checkpoint UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Target SessionID |
| `GitCommitHash` | `string` | `json:"git_commit_hash"` | `redact:"none"` | 40-char SHA1 git commit hash |
| `BranchName` | `string` | `json:"branch_name"` | `redact:"none"` | Git branch name string |
| `Description` | `string` | `json:"description"` | `redact:"sensitive"` | Checkpoint notes / summary |
| `PassingTestCount` | `int` | `json:"passing_test_count"` | `redact:"none"` | Tests passing at checkpoint |
| `CreatedAt` | `time.Time` | `json:"created_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 20. `RollbackResult`
Go Struct Name: `RollbackResult`
Description: Outcome record of Git workspace or state checkpoint restoration.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `RollbackID` | `string` | `json:"rollback_id"` | `redact:"none"` | Rollback operation UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Target SessionID |
| `TargetCheckpointID` | `string` | `json:"target_checkpoint_id"` | `redact:"none"` | CheckpointID requested |
| `PreviousCommitHash` | `string` | `json:"previous_commit_hash"` | `redact:"none"` | Git commit hash prior to rollback |
| `RestoredCommitHash` | `string` | `json:"restored_commit_hash"` | `redact:"none"` | Git commit hash post rollback |
| `Success` | `bool` | `json:"success"` | `redact:"none"` | True if git checkout succeeded |
| `ErrorMessage` | `string` | `json:"error_message,omitempty"` | `redact:"sensitive"` | Optional error details if failed |
| `CompletedAt` | `time.Time` | `json:"completed_at"` | `redact:"none"` | RFC 3339 timestamp |

---

### 21. `ProviderUsage`
Go Struct Name: `ProviderUsage`
Description: LLM API request usage metrics (tokens, cost USD, latency ms).

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `UsageID` | `string` | `json:"usage_id"` | `redact:"none"` | Usage UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | SessionID |
| `ProviderName` | `string` | `json:"provider_name"` | `redact:"none"` | Enum: `openai`, `ollama`, `anthropic`, `local` |
| `Model` | `string` | `json:"model"` | `redact:"none"` | LLM model identifier string |
| `PromptTokens` | `int` | `json:"prompt_tokens"` | `redact:"none"` | Non-negative integer |
| `CompletionTokens` | `int` | `json:"completion_tokens"` | `redact:"none"` | Non-negative integer |
| `TotalTokens` | `int` | `json:"total_tokens"` | `redact:"none"` | Equal to `PromptTokens + CompletionTokens` |
| `EstimatedCostUSD` | `float64` | `json:"estimated_cost_usd"` | `redact:"none"` | Non-negative dollar amount |
| `LatencyMs` | `int64` | `json:"latency_ms"` | `redact:"none"` | Request duration integer |
| `Timestamp` | `time.Time` | `json:"timestamp"` | `redact:"none"` | RFC 3339 timestamp |

---

### 22. `AuditRecord`
Go Struct Name: `AuditRecord`
Description: Immutable event entry persisted to SQLite WAL audit database.

| Field Name | Go Type | JSON Tag | Redaction Tag | Constraints / Validation |
|------------|---------|----------|---------------|--------------------------|
| `AuditID` | `string` | `json:"audit_id"` | `redact:"none"` | Audit record UUID |
| `SessionID` | `string` | `json:"session_id"` | `redact:"none"` | Session ID |
| `Actor` | `string` | `json:"actor"` | `redact:"none"` | Enum: `SUPERVISOR`, `TARGET_AGENT`, `REVIEWER`, `USER` |
| `Category` | `string` | `json:"category"` | `redact:"none"` | Enum: `SESSION_STATE`, `DETECTION`, `INTERVENTION`, `CHECKPOINT`, `REDACTION` |
| `Summary` | `string` | `json:"summary"` | `redact:"sensitive"` | Human-readable log message summary |
| `DetailJSON` | `json.RawMessage` | `json:"detail_json,omitempty"` | `redact:"sanitize"` | Raw JSON payload details |
| `RecordedAt` | `time.Time` | `json:"recorded_at"` | `redact:"none"` | RFC 3339 immutably logged timestamp |

---

## 5. Redaction Metadata Tagging Specification & Policy

Reinframe enforces strict data privacy and secrets protection via `redact:"..."` struct tags processed by the Sensitive Data Redaction Engine (Issue #37).

### Redaction Categories:
1. `redact:"none"`
   - Used for structural IDs, numeric metrics, timestamps, booleans, and non-sensitive status enums.
   - Preserved as-is in telemetry and export logs.

2. `redact:"path"`
   - Used for file paths, working directory paths, and scope glob patterns.
   - Home directory references (e.g. `/Users/johndoe/` or `C:\Users\johndoe\`) are stripped or scrubbed to relative repository paths before exporting external telemetry.

3. `redact:"sensitive"`
   - Used for prompt text, code diffs, stdout/stderr logs, tool arguments, error messages, and reviewer rationales.
   - Processed through regex scrubbers to detect and mask API keys (OpenAI `sk-`, Anthropic `sk-ant-`), OAuth tokens, SSH private keys, password strings, and PII.

4. `redact:"sanitize"`
   - Used for nested structs, maps, raw JSON messages (`json.RawMessage`), and struct slices.
   - Indicates that the redaction engine must recursively walk child elements to apply field-level redaction rules.

---

## 6. Complete JSON Schemas Specification

All 22 canonical data structures require corresponding JSON Schema Draft-07 files stored under `pkg/protocol/schemas/<snake_case_name>.json`.

### Schema Naming & URI Convention:
- File naming: `<snake_case_name>.json` (e.g., `agent_session.json`, `tool_call_event.json`, `tunnel_assessment.json`).
- Schema `$schema`: `http://json-schema.org/draft-07/schema#`
- Schema `$id`: `https://reinframe.dev/schemas/<snake_case_name>.json`

### JSON Schema Property Mapping Table:
| Data Structure | Schema File | Root Required Properties |
|----------------|-------------|--------------------------|
| `AgentSession` | `agent_session.json` | `session_id`, `agent_id`, `adapter_type`, `integration_level`, `workspace_path`, `status`, `started_at` |
| `TaskEnvelope` | `task_envelope.json` | `task_id`, `session_id`, `prompt`, `max_depth`, `timeout_seconds`, `created_at` |
| `AgentEvent` | `agent_event.json` | `event_id`, `session_id`, `sequence_num`, `event_type`, `timestamp`, `payload` |
| `ToolCallEvent` | `tool_call_event.json` | `tool_call_id`, `tool_name`, `arguments`, `duration_ms` |
| `FileChangeEvent` | `file_change_event.json` | `file_path`, `change_type`, `lines_added`, `lines_removed`, `is_scope_violation` |
| `TestResultEvent` | `test_result_event.json` | `test_run_id`, `command`, `passed_count`, `failed_count`, `skipped_count`, `pass_delta`, `duration_ms` |
| `ErrorFingerprint` | `error_fingerprint.json` | `fingerprint_id`, `raw_error`, `normalized_text`, `occurrence_count`, `first_observed_at`, `last_observed_at` |
| `EvidenceItem` | `evidence_item.json` | `item_id`, `source`, `category`, `content` |
| `EvidencePack` | `evidence_pack.json` | `pack_id`, `session_id`, `fork_turns`, `items`, `churn_ratio`, `repeated_error_count`, `created_at` |
| `Hypothesis` | `hypothesis.json` | `hypothesis_id`, `statement`, `status`, `confidence_score` |
| `Assumption` | `assumption.json` | `assumption_id`, `description`, `is_verified` |
| `TunnelSignal` | `tunnel_signal.json` | `signal_id`, `session_id`, `detector_name`, `failure_mode`, `weight`, `score`, `triggered_at` |
| `TunnelAssessment` | `tunnel_assessment.json` | `assessment_id`, `session_id`, `aggregate_score`, `primary_failure_mode`, `is_tunnel_detected`, `recommended_action`, `signals`, `evaluated_at` |
| `ReviewRequest` | `review_request.json` | `request_id`, `reviewer_role`, `evidence_pack_id`, `model`, `prompt`, `requested_at` |
| `ReviewDecision` | `review_decision.json` | `decision_id`, `request_id`, `reviewer_role`, `tunnel_confidence`, `classification`, `rationale`, `tokens_used`, `decided_at` |
| `Intervention` | `intervention.json` | `intervention_id`, `session_id`, `level`, `action_type`, `status`, `executed_at` |
| `BudgetState` | `budget_state.json` | `session_id`, `max_tokens`, `used_tokens`, `max_cost_usd`, `current_cost_usd`, `max_interventions`, `intervention_count`, `is_exhausted` |
| `CapabilityManifest` | `capability_manifest.json` | `agent_id`, `version`, `integration_level`, `supports_pause`, `supports_cancel`, `supports_resume`, `supports_checkpoint`, `supports_rollback`, `supports_mcp` |
| `Checkpoint` | `checkpoint.json` | `checkpoint_id`, `session_id`, `git_commit_hash`, `branch_name`, `description`, `passing_test_count`, `created_at` |
| `RollbackResult` | `rollback_result.json` | `rollback_id`, `session_id`, `target_checkpoint_id`, `previous_commit_hash`, `restored_commit_hash`, `success`, `completed_at` |
| `ProviderUsage` | `provider_usage.json` | `usage_id`, `session_id`, `provider_name`, `model`, `prompt_tokens`, `completion_tokens`, `total_tokens`, `estimated_cost_usd`, `latency_ms`, `timestamp` |
| `AuditRecord` | `audit_record.json` | `audit_id`, `session_id`, `actor`, `category`, `summary`, `recorded_at` |

---

## 7. Schema Validation Engine Architecture

The schema validation engine in `pkg/protocol` must implement the following key entry points:

```go
// ValidateEvent checks an arbitrary JSON payload byte slice against the compiled JSON schema for schemaType.
func ValidateEvent(payload []byte, schemaType string) error

// LoadSchemas parses and compiles all schema JSON files embedded via go:embed pkg/protocol/schemas/*.json.
func LoadSchemas() error
```

### Key Validation Rules:
1. `schemaType` lookup matches the `EventType` string or canonical structure name (case-insensitive conversion to snake_case).
2. Uses `github.com/santhosh-tekuri/jsonschema/v5` (or pure Go JSON Schema validator) compiled at init time for sub-millisecond validation per event.
3. Errors returned provide clear, actionable path and violation details (e.g., `invalid property "status": value "UNKNOWN" not in enum ["OBSERVE", ...]`).
