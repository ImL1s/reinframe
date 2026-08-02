# Plan — Issue #6: Canonical Agent Event Schema & JSON Validation

## Objective
Implement canonical agent event schema and JSON validation for Reinframe according to Issue #6 specifications.

## High-Level Execution Plan
1. **Phase 0: Repository & Requirements Survey**
   - Dispatch 3 Explorers / Spec Miners to survey existing repository layout (`/Users/iml1s/Documents/mine/reinframe`), check `ORIGINAL_REQUEST.md`, inspect existing `pkg/protocol/` (if any), check Go dependencies, and map requirements.
   - Formulate `PROJECT.md` and `TEST_INFRA.md`.

2. **Phase 1: Decomposition & Track Setup**
   - Setup Dual Track: Implementation Track & E2E/Unit Testing Track.
   - Branch creation: `issue-6-canonical-agent-event-schema`.

3. **Phase 2: Milestone Execution**
   - **Milestone 1**: Go Struct definitions in `pkg/protocol/schema.go` for all 22 data models (AgentSession, TaskEnvelope, AgentEvent, ToolCallEvent, FileChangeEvent, TestResultEvent, ErrorFingerprint, EvidenceItem, EvidencePack, Hypothesis, Assumption, TunnelSignal, TunnelAssessment, ReviewRequest, ReviewDecision, Intervention, BudgetState, CapabilityManifest, Checkpoint, RollbackResult, ProviderUsage, AuditRecord) with JSON tags and redaction metadata tags.
   - **Milestone 2**: JSON Schema definitions in `pkg/protocol/schemas/*.json` and `ValidateEvent(payload []byte, schemaType string) error` implementation in `pkg/protocol/`.
   - **Milestone 3**: Comprehensive unit tests in `pkg/protocol/schema_test.go` covering all valid/invalid schemas and edge cases.

4. **Phase 3: Verification & Auditing**
   - Independent Reviewers, Challengers, and Forensic Auditor (`teamwork_preview_auditor`).
   - Strict Gate check: build/tests pass, all Reviewers approve, Challengers approve, Forensic Auditor CLEAN.

5. **Phase 4: Git & Delivery**
   - Commit changes cleanly, update Issue #6 comment on GitHub, create Pull Request, deliver final completion report.
