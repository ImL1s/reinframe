# Survey Report 1: Codebase & Repo Structure

## 1. Observation

### 1.1 Root Directory & File Structure
Direct directory listing (`list_dir`) and file search (`find_by_name`) of `/Users/iml1s/Documents/mine/reinframe` identified 46 total visible items:
- Root files:
  - `ORIGINAL_REQUEST.md`: Contains project overview, Issue #6, Issue #7, and Issue #9 requirements and acceptance criteria.
  - `PROJECT.md`: Technical documentation detailing the architecture, 6 feature inventory items, 3 milestone specifications (M1-M3), public API contract for `pkg/protocol`, and code layout.
  - `TEST_INFRA.md`: Specifies test philosophy, 6 test mapping criteria, and test command (`go test -v -race -bench=. ./pkg/protocol/...`).
  - `go.mod`: Module name `github.com/reinframe/reinframe`, Go version `1.25.0`, dependency `github.com/santhosh-tekuri/jsonschema/v5 v5.3.1 // indirect`.
  - `go.sum`: Verification checksums for `jsonschema/v5 v5.3.1`.
- Directories:
  - `docs/`: Subdirectories `docs/adr/`, `docs/architecture/`, `docs/research/`, `docs/specs/`.
  - `pkg/`: Contains `pkg/protocol/` Go source files, unit tests, and 22 embedded JSON schema files under `pkg/protocol/schemas/`.
  - `.agents/`: Holds agent metadata (`teamwork_preview_explorer_survey_1`).
  - `.git/`: Repository git database.

### 1.2 Documentation Architecture (`docs/`)
Inspection via `view_file` revealed six foundational specification documents:
1. `docs/adr/001_external_supervisor_vs_extension.md`: ADR 001 deciding Option C — External OS Process Supervisor with embedded MCP Bridge.
2. `docs/adr/002_core_language_db_ipc.md`: ADR 002 selecting Go 1.25+, SQLite with WAL mode (`modernc.org/sqlite` or `mattn/go-sqlite3`), and JSON-RPC 2.0 / NDJSON IPC protocol.
3. `docs/architecture/dag_and_execution_plan.md`: 47 atomized issue backlog across Phases 1-4 (M0 to M3).
4. `docs/research/anti_tunnel_threat_model.md`: Failure Mode Taxonomy (FM-1 to FM-6), signal scoring formula ($w_1 S_{\text{error}} + w_2 S_{\text{churn}} + w_3 S_{\text{scope}} + w_4 S_{\text{reviewer}}$), and Level 0-3 intervention escalation ladder.
5. `docs/research/harness_capability_matrix.md`: 24 dimensions x 12 agent frameworks matrix, mapping Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), and Level 3 (Full-control).
6. `docs/specs/mvp_scope_and_non_goals.md`: Milestone M1 minimum vertical slice scope, non-goals, and OS compatibility matrix (macOS Sonoma/Sequoia, Ubuntu 22.04+, Windows 11).

### 1.3 Go Package Architecture & Code Patterns (`pkg/protocol`)
Inspection of `pkg/protocol/schema.go`, `pkg/protocol/validator.go`, and test files showed:
- **Canonical Struct Models (`pkg/protocol/schema.go`, lines 1-255)**:
  - Defines 22 Go struct models: `AgentSession`, `TaskEnvelope`, `AgentEvent`, `ToolCallEvent`, `FileChangeEvent`, `TestResultEvent`, `ErrorFingerprint`, `EvidenceItem`, `EvidencePack`, `Hypothesis`, `Assumption`, `TunnelSignal`, `TunnelAssessment`, `ReviewRequest`, `ReviewDecision`, `Intervention`, `BudgetState`, `CapabilityManifest`, `Checkpoint`, `RollbackResult`, `ProviderUsage`, `AuditRecord`.
  - Every exported field includes dual tags: `json:"<snake_case>"` and custom reflection tag `redact:"none|path|sensitive|sanitize"`.
- **Validation Engine (`pkg/protocol/validator.go`, lines 1-124)**:
  - Employs `//go:embed schemas/*.json` for embedding schema files.
  - Implements lazy schema compilation via `sync.Once` (`LoadSchemas()`).
  - Registers schemas under URI `https://reinframe.dev/schemas/<name>.json` with `jsonschema.NewCompiler()` for Draft-07 validation.
  - `ValidateEvent(payload []byte, schemaType string) error` normalizes `schemaType` using `toSnakeCase()` before checking cached schemas.
- **Test Suite (`pkg/protocol/*_test.go`)**:
  - `schema_test.go`: Tests schema loading (22 items), valid payloads, invalid payloads (unknown type, malformed JSON, missing fields, type mismatch, enum violations, out-of-bounds numbers), struct JSON roundtrip for all 22 structs, reflection `redact` tag completeness, and `BenchmarkValidateEvent`.
  - `adversarial_stress_test.go`: Adversarial edge case coverage (empty payloads, corrupt bytes, extra unexpected properties rejection, explicit null fields, out-of-range numbers, malicious `schemaType` strings, 500-level deep nesting, and goroutine concurrency).
  - `challenger_stress_test.go`: High-concurrency stress test launching 200 goroutines executing 500 iterations (100,000 total validations), `schemaType` case normalization tests, boundary edge cases, and compliant payload validation for all 22 schemas.

### 1.4 Test Suite Execution Results
Executing `go test -v -race -bench=. ./pkg/protocol/...` (`run_command`) produced:
```text
--- PASS: TestLoadSchemas (0.00s)
--- PASS: TestValidateEvent_ValidPayloads (0.00s)
--- PASS: TestValidateEvent_InvalidPayloads (0.00s)
--- PASS: TestStructJSONRoundtrip (0.00s)
--- PASS: TestRedactionTags (0.00s)
--- PASS: TestAdversarial_EmptyPayloads (0.00s)
--- PASS: TestAdversarial_CorruptBytes (0.00s)
--- PASS: TestAdversarial_UnexpectedProperties (0.00s)
--- PASS: TestAdversarial_NullFields (0.00s)
--- PASS: TestAdversarial_OutOfRangeNumbers (0.00s)
--- PASS: TestAdversarial_SchemaTypeSecurity (0.00s)
--- PASS: TestAdversarial_DeepRecursionPayload (0.00s)
--- PASS: TestAdversarial_ConcurrentStress (0.00s)
--- PASS: TestChallenger_ConcurrentStress (0.00s)
--- PASS: TestChallenger_SchemaTypeNormalization (0.00s)
--- PASS: TestChallenger_BoundaryAndEdgeCases (0.00s)
--- PASS: TestChallenger_All22SchemasValidation (0.00s)
goos: darwin
goarch: arm64
pkg: github.com/reinframe/reinframe/pkg/protocol
cpu: Apple M4 Max
BenchmarkValidateEvent-16    	   45009	     27023 ns/op
PASS
ok  	github.com/reinframe/reinframe/pkg/protocol	3.635s
```

### 1.5 Git Repository Configuration
Execution of `git status`, `git branch -a`, `git log`, `git remote -v`, and `ls -la .gitignore` revealed:
- **Active Branch**: `issue-6-canonical-agent-event-schema` (up to date with `origin/issue-6-canonical-agent-event-schema`).
- **Remote Origin**: `https://github.com/ImL1s/reinframe.git`.
- **Commit History**:
  - `72428270e14bd0f70706be7c947c3341703721c0`: `feat(protocol): implement canonical agent event schema & JSON validation (#6)`
  - `c8095efe24dcba7b7be3a53f89b96145f1493653`: `docs: add initial architecture research, threat model, ADRs, and DAG plan`
- **Untracked Workspace Artifacts**: `.agents/`, `ORIGINAL_REQUEST.md`, `PROJECT.md`, `TEST_INFRA.md`.
- **Missing File**: `.gitignore` is currently absent from the root directory.

---

## 2. Logic Chain

1. **Architecture & Scope Alignment**:
   - Observations 1.1, 1.2 show that Reinframe has been designed via ADR 001 and ADR 002 as a Go 1.25+ cross-platform process supervisor using SQLite WAL persistence and JSON-RPC 2.0 / NDJSON IPC interfaces.
   - The project plan in `docs/architecture/dag_and_execution_plan.md` outlines 47 atomic issues. Issue #6 is fully implemented in `pkg/protocol`.
2. **Data Protocol Rigor**:
   - Observation 1.3 shows complete 1:1 mapping between 22 Go struct models in `pkg/protocol/schema.go` and 22 JSON schema definitions in `pkg/protocol/schemas/`.
   - Every exported Go field enforces sensitivity redaction rules via custom `redact` tags (`none`, `path`, `sensitive`, `sanitize`), verified at runtime via reflection in `TestRedactionTags`.
3. **Execution Safety & Verification**:
   - Observation 1.4 confirms that `pkg/protocol` compiles cleanly with `-race` enabled and passes 100% of unit, adversarial, stress, and benchmark tests on Darwin arm64. Validation latency is ~27 µs/op, comfortably meeting the sub-20ms requirement.
4. **Repository Hygiene Gap**:
   - Observation 1.5 indicates that while Git branches and commits follow issue-driven development guidelines (`issue-6-canonical-agent-event-schema`), `.gitignore` is missing, leaving `.agents/` and metadata files untracked.

---

## 3. Caveats

- **Scope Limits**: Only `pkg/protocol` has Go code implemented so far. Future packages outlined in the execution plan (`pkg/state`, `pkg/agent`, `pkg/reviewer`, `pkg/detector`, `pkg/adjudicator`) are documented in `docs/` and `ORIGINAL_REQUEST.md` but not yet created.
- **Missing File**: `.gitignore` is absent in the repository root. Future feature branches should create `.gitignore` to prevent tracking `.agents/` or local test DB artifacts.
- **Environment**: Investigation and tests were executed on macOS Darwin arm64 (Apple M4 Max). Cross-platform behavior (Linux, Windows Job Objects) is defined in `docs/adr/001_external_supervisor_vs_extension.md` and `docs/specs/mvp_scope_and_non_goals.md`, but cross-compilation CI matrix (Issue #40) remains to be implemented in Phase 4.

---

## 4. Conclusion

The Reinframe codebase is well-structured, adhering strictly to Go best practices, single-binary architecture, and issue-driven development.
- `pkg/protocol` provides a complete, robust, thread-safe, and sub-millisecond JSON schema validation engine for all 22 canonical event types.
- The repository is currently on branch `issue-6-canonical-agent-event-schema` with Issue #6 implemented and passing all tests under `go test -v -race -bench=. ./pkg/protocol/...`.
- The architecture documentation in `docs/` provides clear blueprints for upcoming implementations: Issue #7 (Capability Manifest & Negotiation Engine in `pkg/protocol/capability.go`) and Issue #9 (SQLite WAL Event Store in `pkg/state/store.go`).

---

## 5. Verification Method

To independently verify all findings in this survey report:

1. **Verify Package Architecture & Layout**:
   ```bash
   ls -la /Users/iml1s/Documents/mine/reinframe/pkg/protocol
   ls -la /Users/iml1s/Documents/mine/reinframe/pkg/protocol/schemas
   ```
2. **Verify Test Suite & Benchmarks**:
   ```bash
   cd /Users/iml1s/Documents/mine/reinframe
   go test -v -race -bench=. ./pkg/protocol/...
   ```
   *Expected Output*: 17 test suites PASS, race detector finds 0 data races, `BenchmarkValidateEvent` completes with 0 failures.

3. **Verify Git Repository & History**:
   ```bash
   git status
   git branch -a
   git log -n 5
   ```
   *Expected Output*: Current branch `issue-6-canonical-agent-event-schema`, head commit `72428270e14bd0f70706be7c947c3341703721c0`.

4. **Invalidation Conditions**:
   - Any failure or panic during `go test -race ./pkg/protocol/...`.
   - Discrepancy between the 22 Go struct models in `schema.go` and the 22 JSON schema files in `pkg/protocol/schemas/`.
