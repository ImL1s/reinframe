## 2026-08-02T05:27:08Z
You are Worker M1 (Go Structs & Package Setup Worker).
Your working directory is /Users/iml1s/Documents/mine/reinframe/.agents/worker_m1.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Context & Requirements:
- Read /Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md
- Read /Users/iml1s/Documents/mine/reinframe/PROJECT.md
- Read /Users/iml1s/Documents/mine/reinframe/.agents/spec_miner_1/analysis.md for exact specifications of the 22 canonical struct models.

Task:
1. Create Git branch: Run `git checkout -b issue-6-canonical-agent-event-schema` (if not already created).
2. Initialize `go.mod` in project root with module `github.com/reinframe/reinframe` (e.g. `go mod init github.com/reinframe/reinframe`).
3. Add dependency `github.com/santhosh-tekuri/jsonschema/v5` (run `go get github.com/santhosh-tekuri/jsonschema/v5`).
4. Create directory `pkg/protocol/` and `pkg/protocol/schemas/`.
5. Implement `pkg/protocol/schema.go` defining all 22 canonical Go structs:
   - AgentSession
   - TaskEnvelope
   - AgentEvent
   - ToolCallEvent
   - FileChangeEvent
   - TestResultEvent
   - ErrorFingerprint
   - EvidenceItem
   - EvidencePack
   - Hypothesis
   - Assumption
   - TunnelSignal
   - TunnelAssessment
   - ReviewRequest
   - ReviewDecision
   - Intervention
   - BudgetState
   - CapabilityManifest
   - Checkpoint
   - RollbackResult
   - ProviderUsage
   - AuditRecord
   Ensure every struct has standard `json:"..."` tags and `redact:"none|path|sensitive|sanitize"` metadata tags as detailed in Spec Miner 1 analysis report.
6. Verify syntax by running `go build ./pkg/protocol/...`.
7. Write your handoff report to /Users/iml1s/Documents/mine/reinframe/.agents/worker_m1/handoff.md detailing commands executed, files created/modified, and build results.
8. Send a message to parent with a brief summary and the path to handoff.md.
