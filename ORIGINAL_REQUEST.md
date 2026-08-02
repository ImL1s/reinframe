# Original User Request

## Initial Request — 2026-08-02T13:25:04Z

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

Working directory: /Users/iml1s/Documents/mine/reinframe
Integrity mode: development

## Requirements

### R1. Implement Issue #6: Canonical Agent Event Schema & JSON Validation
Define all 22 canonical event and data structures (AgentSession, TaskEnvelope, AgentEvent, ToolCallEvent, FileChangeEvent, TestResultEvent, ErrorFingerprint, EvidenceItem, EvidencePack, Hypothesis, Assumption, TunnelSignal, TunnelAssessment, ReviewRequest, ReviewDecision, Intervention, BudgetState, CapabilityManifest, Checkpoint, RollbackResult, ProviderUsage, AuditRecord) in pkg/protocol/schema.go, JSON schemas in pkg/protocol/schemas/, and JSON schema validation engine.

### R2. Strict Issue-Driven Development Workflow
- Create Git branch: issue-6-canonical-agent-event-schema
- Implement scope of Issue #6 only.
- Write unit tests in pkg/protocol/schema_test.go.
- Validate with go test ./pkg/protocol/...
- Create clean commit, update Issue #6 comment on GitHub, and open Pull Request.

## Acceptance Criteria

### Schema Completeness & Validation
- [ ] All 22 canonical types defined in Go struct models with JSON tags and redaction metadata tags.
- [ ] JSON Schema files created under pkg/protocol/schemas/*.json for canonical payload validation.
- [ ] ValidateEvent(payload []byte, schemaType string) error function passes unit tests.
- [ ] Unit tests pass with go test ./pkg/protocol/...
- [ ] Git commit and Pull Request created for Issue #6.

## Follow-up — 2026-08-02T13:38:58Z

Reinframe is a cross-platform (Windows, macOS, Linux) Anti-Tunnel Supervision Harness for AI coding agents written in Go, powered by SQLite WAL state persistence and JSON-RPC 2.0 / NDJSON protocol interfaces.

Working directory: /Users/iml1s/Documents/mine/reinframe
Integrity mode: development

## Requirements

### R1. Implement Issue #7: Capability Manifest & Handshake Protocol
Build CapabilityManifest struct, 20 capability flags, and negotiation engine (pkg/protocol/capability.go) supporting Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), Level 3 (Full-control) with automatic degradation. Write unit tests in pkg/protocol/capability_test.go.

### R2. Implement Issue #9: Append-Only Event Store & SQLite WAL Engine
Build SQLite WAL-backed event store (pkg/state/store.go), schema migration engine (pkg/state/migrations/001_initial_events.sql), AppendEvent and QueryEvents methods with multi-goroutine safety. Write unit tests in pkg/state/store_test.go.

### R3. Strict Issue-Driven Development Workflow
- Create Git branches: issue-7-capability-manifest-negotiation and issue-9-sqlite-wal-event-store.
- Implement unit tests and run go test -race ./pkg/...
- Run Victory Audit and create GitHub comments & Pull Requests for Issue #7 and Issue #9.

## Acceptance Criteria

### Issue #7 Criteria
- [ ] CapabilityManifest and NegotiateLevel correctly map capability flags to Levels 0-3.
- [ ] Degradation policy handles partial capabilities safely.
- [ ] Unit tests pass with go test -race ./pkg/protocol/...
- [ ] Pull Request opened for Issue #7.

### Issue #9 Criteria
- [ ] SQLite connection configured with journal_mode=WAL and busy timeout.
- [ ] 001_initial_events.sql migration runs cleanly on fresh DB.
- [ ] AppendEvent and QueryEvents pass concurrent race tests (go test -race ./pkg/state/...).
- [ ] Pull Request opened for Issue #9.
