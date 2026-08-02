# Handoff Report — Reviewer 1 (Protocol & Governance Focus)

## 1. Observation

Direct observations and evidence gathered from codebase inspection and terminal test execution:

- **`pkg/protocol/capability.go`**:
  - `ToBitmask()` (lines 103-172): Bitmask is constructed strictly by bitwise-ORing explicit boolean fields (`SupportsEventStream` .. `SupportsSDK`). `IntegrationLevel` is NOT referenced in `ToBitmask()`, eliminating auto-granting permissions.
  - Required Level Bitmasks (lines 43-48): `Level1RequiredMask` is `Level0RequiredMask | CapToolInspection`. `Level2RequiredMask` includes `Level1RequiredMask | CapDiffInspection | CapPause | CapCancel | CapResume | CapCheckpoint | CapRollback`. `CapPause`, `CapCancel`, and `CapResume` were successfully re-aligned from Level 1 to Level 2 (Guarded mode).
  - All 20 capability flags (`CapEventStream` through `CapSDK`) are defined with distinct bit shifts `1 << 0` to `1 << 19`.
  - `FromBitmask()` (lines 175-202): Populates all 20 boolean fields from the bitmask.

- **`pkg/protocol/schema.go` & `pkg/protocol/schemas/`**:
  - `CapabilityManifest` struct (lines 196-223): Contains all 20 `Supports*` boolean fields with `json:"..."` tags.
  - `capability_manifest.json`: Defines all 20 boolean properties under `properties` and listed in `required`.
  - `agent_session.json`: Includes `"RESUME"` in the `status` enum array (lines 44-54).
  - `task_envelope.json`: `max_depth` schema enforces `"minimum": 1, "maximum": 1` (lines 33-37).
  - Exactly 22 JSON schema files exist under `pkg/protocol/schemas/*.json`.

- **`pkg/protocol/validator.go`**:
  - `init()` fail-fast (lines 23-68): Compiles all 22 schemas into `schemaCache` on package initialization, panicking if any schema is invalid.
  - `ValidateEvent` (lines 79-102): Enforces payload size limit (`len(payload) > MaxPayloadSize` where `MaxPayloadSize = 1 * 1024 * 1024` / 1MB). Uses `json.Decoder.UseNumber()` to decode raw JSON into `any`, preventing float64 precision loss for large int64 fields.

- **Governance, CI & Directory Layout**:
  - `go.mod` specifies `go 1.25.0`.
  - `.github/workflows/ci.yml` uses `go-version-file: 'go.mod'`, runs `golangci-lint-action@v6`, tests across `ubuntu-latest`, `macos-latest`, `windows-latest`.
  - Root `README.md` exists with project goals, module responsibilities, and quickstart commands.
  - Root `.gitignore` excludes `*.db`, `*.db-wal`, `*.db-shm`, `.DS_Store`, and `.agents/*`.
  - Specification docs (`ORIGINAL_REQUEST.md`, `PROJECT.md`, `TEST_INFRA.md`, `TEST_READY.md`) are located in `docs/dev/`.
  - `tests/integration/` package is set to `integration_test`.

- **Test Execution Results**:
  - `go test -v -count=1 -race ./pkg/protocol/...` completed with 0 failures (PASS).
  - `go test -v -count=1 -race ./tests/integration/...` completed with 0 failures across all 4 tiers (PASS).
  - `go test -v -count=1 -race ./...` completed with 0 failures across the entire repository (PASS).

## 2. Logic Chain

1. **Auto-Granting Bypass Fix**:
   - *Observation*: `CapabilityManifest.ToBitmask()` in `pkg/protocol/capability.go` evaluates bits only from explicit boolean struct fields and does not consult `IntegrationLevel`.
   - *Deduction*: Setting `IntegrationLevel: 3` without explicit booleans produces bitmask `0x0`. A low-privilege client cannot elevate permissions simply by declaring a higher `IntegrationLevel`.

2. **Full 20-Capability Field Completeness**:
   - *Observation*: Struct `CapabilityManifest`, `ToBitmask()`, `FromBitmask()`, and `capability_manifest.json` schema all explicitly reference all 20 capabilities.
   - *Deduction*: JSON serialization/deserialization round-trips preserve all 20 capability booleans without data loss or default truncation.

3. **Level Contract Alignment**:
   - *Observation*: `Level1RequiredMask` requires only `CapEventStream` and `CapToolInspection`. `CapPause`, `CapCancel`, and `CapResume` are required under `Level2RequiredMask`.
   - *Deduction*: Level 1 is correctly restricted to Advisory supervision (monitoring/inspection without process intervention), whereas Level 2 correctly requires process control and workspace snapshot/rollback capabilities for Guarded supervision.

4. **Validator Safety & Fail-Fast**:
   - *Observation*: `ValidateEvent` rejects payloads exceeding 1MB before parsing, uses `json.Decoder.UseNumber()`, and relies on pre-compiled schemas initialized during `func init()`.
   - *Deduction*: DoS via large payload injection is prevented, float64 precision truncation on 64-bit sequence numbers is mitigated, and invalid schemas cause immediate startup panics rather than runtime error leaks.

5. **Governance & CI Consistency**:
   - *Observation*: CI workflow references `go.mod` directly via `go-version-file: 'go.mod'`, includes `golangci-lint`, and tests with `-race`. Root files (`README.md`, `.gitignore`) and `docs/dev/` structure conform to `PROJECT.md`.
   - *Deduction*: Build and governance setup is reproducible, consistent across local development and CI pipelines, and adheres to layout constraints.

## 3. Caveats

- No caveats. All required items under Review Scope have been inspected, tested, and verified directly.

## 4. Conclusion

**Verdict**: **APPROVE**

All code changes in `pkg/protocol/` and Governance/CI components meet all requirements:
1. `ToBitmask()` auto-granting permissions based on `IntegrationLevel` are completely eliminated.
2. All 20 capability boolean fields exist across Go structs, bitmask helpers, and JSON schemas.
3. Level contracts (Level 1 Advisory vs Level 2 Guarded) are properly re-aligned.
4. `ValidateEvent` limits payload size to 1MB, uses `json.Decoder.UseNumber()`, and schemas compile fail-fast in `init()`.
5. `AgentSession.status` enum contains `RESUME` and `task_envelope.json` enforces `max_depth: 1`.
6. Governance and CI configuration (`go.mod`, `.github/workflows/ci.yml`, `README.md`, `.gitignore`, `docs/dev/`, `tests/integration/`) are clean and consistent.
7. All unit and integration test suites pass with `-race` detection enabled.

## 5. Verification Method

To independently verify this assessment:

1. **Protocol Unit Tests (with Race Detector)**:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
2. **Integration Test Suite (with Race Detector)**:
   ```bash
   go test -v -count=1 -race ./tests/integration/...
   ```
3. **Full Project Test Suite**:
   ```bash
   go test -v -count=1 -race ./...
   ```
4. **Code Inspection**:
   - Inspect `pkg/protocol/capability.go` lines 103-172 to confirm `ToBitmask()` relies exclusively on boolean fields.
   - Inspect `pkg/protocol/validator.go` lines 80-92 to verify `MaxPayloadSize` check and `json.Decoder.UseNumber()`.
   - Inspect `pkg/protocol/schemas/agent_session.json` and `pkg/protocol/schemas/task_envelope.json` for `RESUME` and `maximum: 1` constraints.
