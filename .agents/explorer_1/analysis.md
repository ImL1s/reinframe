# Codebase & Go Environment Survey Report (Issue #6 Preparation)

**Date**: 2026-08-02  
**Explorer**: Explorer 1 (Codebase & Go Environment Explorer)  
**Target Repository**: `/Users/iml1s/Documents/mine/reinframe`  

---

## 1. Executive Summary

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

Currently, the repository is in an early architecture & documentation state. The `docs/` folder contains comprehensive ADRs, specifications, threat models, capability matrices, and execution DAGs. However, **no Go codebase (`go.mod`, `pkg/`, `main.go`, or test files) exists yet in the repository**.

The task for Issue #6 is to establish the core protocol package (`pkg/protocol`), implement all **22 Canonical Event and Data Structures** in `pkg/protocol/schema.go`, define JSON schemas under `pkg/protocol/schemas/`, implement a JSON schema validation engine (`ValidateEvent`), write unit tests in `pkg/protocol/schema_test.go`, and verify everything with `go test ./pkg/protocol/...`.

---

## 2. Current Repository Inventory & File Structure

### File Tree Analysis
```
/Users/iml1s/Documents/mine/reinframe
├── .agents/                               # Agent working directories (metadata)
│   └── explorer_1/                        # Working folder for Explorer 1
│       ├── BRIEFING.md
│       ├── DISPATCH.md
│       ├── analysis.md                    # This report
│       └── handoff.md                     # Handoff report
├── .git/                                  # Git version control directory
├── ORIGINAL_REQUEST.md                    # User prompt & acceptance criteria
└── docs/                                  # Architectural and research documentation
    ├── adr/
    │   ├── 001_external_supervisor_vs_extension.md
    │   └── 002_core_language_db_ipc.md
    ├── architecture/
    │   └── dag_and_execution_plan.md
    ├── research/
    │   ├── anti_tunnel_threat_model.md
    │   └── harness_capability_matrix.md
    └── specs/
        └── mvp_scope_and_non_goals.md
```

### Absence of Go Source Files
- **`go.mod`**: Missing. Must be initialized (`go mod init github.com/reinframe/reinframe` or module name defined by team).
- **`pkg/`**: Missing. Must be created for Go packages.
- **`pkg/protocol/`**: Missing. Must be created for Issue #6 schema and validation code.

---

## 3. Go Environment & Build/Test Status

### Local Toolchain
- **Go Version**: `go1.26.0 darwin/arm64` (Installed and functional on host).
- **Git Branch Status**: `main` branch, clean working tree except untracked `.agents/` and `ORIGINAL_REQUEST.md`.
- **Git Commit History**: Latest commit `c8095efe24dcba7b7be3a53f89b96145f1493653` (`docs: add initial architecture research, threat model, ADRs, and DAG plan`).

### Verification Command Execution Result
Command: `go test ./...`  
Result: Exited with code 1  
Output:
```
# ./...
pattern ./...: directory prefix . does not contain main module or its selected dependencies
FAIL	./... [setup failed]
```
*Rationale*: `go test ./...` fails expectedly because `go.mod` is not yet created.

---

## 4. Requirements Analysis for Issue #6 (Canonical Agent Event Schema & JSON Validation)

### 4.1 Target Branch Requirement
- Create git branch `issue-6-canonical-agent-event-schema` from `main`.

### 4.2 Required Module & Package Setup
- Initialize `go.mod` in repository root.
- Create directory structure:
  - `pkg/protocol/`
  - `pkg/protocol/schemas/`

### 4.3 The 22 Canonical Event & Data Structures
Issue #6 requires defining all 22 canonical struct models in `pkg/protocol/schema.go`:

| # | Canonical Model | Description & Role |
|---|---|---|
| 1 | `AgentSession` | Session metadata, target agent ID, harness level, start/end timestamps, session status. |
| 2 | `TaskEnvelope` | Task description, goal, constraints, repository root, original prompt, acceptance criteria. |
| 3 | `AgentEvent` | Wrapper event envelope (ID, SessionID, Timestamp, EventType, Sequence, Payload). |
| 4 | `ToolCallEvent` | Tool name, parameters, execution outcome, duration, stdio outputs. |
| 5 | `FileChangeEvent` | Modified file path, diff patch, lines added/deleted, change type (created/modified/deleted). |
| 6 | `TestResultEvent` | Test suite name, total/passed/failed counts, failed test names, failure logs. |
| 7 | `ErrorFingerprint` | Normalized error string hash/fingerprint, raw error text, repetition count, file location. |
| 8 | `EvidenceItem` | Single item of objective evidence (source type, file path, line range, snippet, relevance). |
| 9 | `EvidencePack` | Collection of evidence items compiled for reviewer LLM assessment (`fork_turns`). |
| 10 | `Hypothesis` | Working hypothesis formulated by agent or reviewer regarding root cause or plan. |
| 11 | `Assumption` | Presumed state/fact checked by Assumption Auditor against empirical evidence. |
| 12 | `TunnelSignal` | Signal detected by signal framework (detector name, signal type, weight, score, raw metrics). |
| 13 | `TunnelAssessment` | Combined classification result (failure mode category, confidence score, rationale). |
| 14 | `ReviewRequest` | Request envelope sent to Reviewer Provider (role, evidence pack, prompt, schema format). |
| 15 | `ReviewDecision` | Output decision from Reviewer Provider (verdict, action recommendation, confidence). |
| 16 | `Intervention` | Action taken by supervisor (level 0-3, action type, payload, zoom-out prompt, timestamp). |
| 17 | `BudgetState` | Token budget, USD cost accumulator, max turns, remaining turns, budget status. |
| 18 | `CapabilityManifest` | Harness capability level (0-3), supported tools, pause/resume capability, hook endpoints. |
| 19 | `Checkpoint` | Git commit hash, session state snapshot ID, workspace tree hash, timestamp. |
| 20 | `RollbackResult` | Outcome of rollback operation (target checkpoint ID, restored files count, success status, error). |
| 21 | `ProviderUsage` | Provider name, model name, prompt tokens, completion tokens, estimated cost USD. |
| 22 | `AuditRecord` | Immutable audit log entry combining event, state snapshot, and intervention outcome. |

### 4.4 Tagging & Metadata Requirements
- All structs must include standard `json:"..."` tags.
- Sensitive or PII fields must include redaction metadata tags (e.g. `redact:"true"` or `json:"..." redact:"true"` as per sensitive data redaction design in ADRs/Threat Model).

### 4.5 JSON Schemas & Validation Engine
- Place JSON Schema `.json` files in `pkg/protocol/schemas/` corresponding to the canonical types.
- Implement validation function in `pkg/protocol/schema.go` (or `pkg/protocol/validator.go`):
  `ValidateEvent(payload []byte, schemaType string) error`
- Validation logic can leverage standard JSON Schema packages such as `github.com/santhosh-tekuri/jsonschema/v5` or `github.com/xeipuuv/gojsonschema` (or embed schema definitions via Go 1.16+ `//go:embed pkg/protocol/schemas/*.json`).

### 4.6 Testing Requirements
- Implement unit tests in `pkg/protocol/schema_test.go`.
- Test cases must cover:
  1. Valid JSON payload validation for canonical types (should return `nil` error).
  2. Invalid JSON payload validation (missing required fields, bad types, should return validation error).
  3. JSON marshaling and unmarshaling of Go structs.
  4. Redaction metadata tag presence on expected sensitive fields.
- Command to verify: `go test ./pkg/protocol/...`

---

## 5. Next Steps & Recommendations for Implementation

1. **Git Branch Creation**: `git checkout -b issue-6-canonical-agent-event-schema`
2. **Go Module Initialization**: Run `go mod init github.com/reinframe/reinframe` (or selected module name).
3. **Directory Creation**: `mkdir -p pkg/protocol/schemas`
4. **Implement Models**: Create `pkg/protocol/schema.go` defining the 22 Go struct models.
5. **Create JSON Schemas**: Write corresponding `.json` schema files in `pkg/protocol/schemas/`.
6. **Implement Validator**: Implement `ValidateEvent(payload []byte, schemaType string) error` using `go:embed`.
7. **Write Unit Tests**: Create `pkg/protocol/schema_test.go` and run `go test ./pkg/protocol/...`.
8. **Commit & PR**: Commit changes cleanly and open Pull Request for Issue #6.
