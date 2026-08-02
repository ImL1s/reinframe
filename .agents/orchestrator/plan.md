# Orchestration Plan: Reinframe P0/P1 Issue Resolution

## Executive Summary
Resolving 13 P0 Blocker and 8 P1 issues across SQLite state engine, Protocol schema/capability, and Governance/CI structure.

## Milestone Plan

### Survey Phase (Step 0)
- **Explorer 1 (State Focus)**: Inspect `pkg/state/store.go`, migrations, DSN pragmas, mutex usages, BeginTx, `:memory:` pooling, and existing stress tests.
- **Explorer 2 (Protocol Focus)**: Inspect `pkg/protocol/` (schema.go, capability.go, json validation, bitmask logic, RESUME status, capability flags, schemas).
- **Explorer 3 (Governance/CI Focus)**: Inspect `go.mod`, `.github/workflows/`, README.md, `.gitignore`, directory structure (`docs/dev/`, `tests/e2e`), migration tests, Issue tracker status.

### Milestone Decomposition
1. **M1: SQLite Concurrency Architecture Fixes (R1)**
   - Fix DSN Pragma configuration.
   - Remove global `sync.RWMutex` / `sync.Mutex` in `store.go`, replace closed status with `atomic.Bool`.
   - Replace manual connection `BEGIN IMMEDIATE` with `db.BeginTx(ctx, nil)` and `_txlock=immediate` DSN parameter.
   - Fix default `:memory:` DB pooling with `cache=shared` or `maxOpen=1`.
2. **M2: Protocol Capability & Schema Fixes (R2)**
   - Remove auto-granting `IntegrationLevel` logic in `ToBitmask()`.
   - Re-align Level contracts with original ADR specs.
   - Expand `CapabilityManifest` struct and JSON schema to include all 20 boolean capability flags.
   - Secure `ValidateEvent()` with payload size limit check and `json.Decoder.UseNumber()`.
   - Add `RESUME` status to `AgentSession` status enum.
   - Add `maximum: 1` constraint to `max_depth` in JSON schema.
3. **M3: Governance, CI & Directory Refactoring (R3)**
   - Align `go.mod` Go version with CI workflow (`go-version-file: go.mod`).
   - Create root `README.md` and `.gitignore`.
   - Move `ORIGINAL_REQUEST.md`, `PROJECT.md`, `TEST_INFRA.md`, `TEST_READY.md` to `docs/dev/`.
   - Add `golangci-lint` step to CI workflow.
   - Move `SELECT EXISTS` inside transaction in migration logic.
   - Replace `sync.Once` with `init()` fail-fast for schema compilation.
   - Rename `tests/e2e/` to `tests/integration/` and update CI workflow.
   - Increase `BusyTimeout` in flaky tests to 500ms.
   - Refactor tests to use `t.TempDir()`.
4. **M4: Capability Test Suite Rewrite (R4)**
   - Rewrite capability tests to remove assertions expecting auto-granted permissions.
   - Add `TestCapability_JSONRoundTrip_Lossless` test.
   - Verify degradation and missing boolean handling.
5. **M5: Stress Testing & Verification (R5)**
   - Re-run 100 writers / 50 readers stress tests.
   - Re-run 500 goroutine high concurrency stress tests with race detector (`go test -race -count=5`).
   - Verify full CI pipeline pass.
