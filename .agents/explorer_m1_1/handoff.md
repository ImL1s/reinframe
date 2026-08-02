# Handoff Report — Explorer 1: Issue #7 Capability Manifest & Handshake Protocol

## 1. Observation

- **Existing Codebase Layout & Structures**:
  - `pkg/protocol/schema.go`: Line 196-206 defines `CapabilityManifest` struct with 9 fields:
    - `AgentID string` (`json:"agent_id" redact:"none"`)
    - `Version string` (`json:"version" redact:"none"`)
    - `IntegrationLevel int` (`json:"integration_level" redact:"none"`)
    - `SupportsPause bool` (`json:"supports_pause" redact:"none"`)
    - `SupportsCancel bool` (`json:"supports_cancel" redact:"none"`)
    - `SupportsResume bool` (`json:"supports_resume" redact:"none"`)
    - `SupportsCheckpoint bool` (`json:"supports_checkpoint" redact:"none"`)
    - `SupportsRollback bool` (`json:"supports_rollback" redact:"none"`)
    - `SupportsMCP bool` (`json:"supports_mcp" redact:"none"`)
  - `pkg/protocol/validator.go`: Embedded schema loader for `schemas/*.json`.
  - `pkg/protocol/schemas/capability_manifest.json`: JSON schema strictly requiring the above 9 fields with `additionalProperties: false`.
  - `pkg/protocol/schema_test.go`: Lines 285-298, 713-725, 839 test `CapabilityManifest` JSON validation and roundtripping.
  - `grep_search` for `Handshake`: Currently 0 occurrences in `pkg/protocol`.

- **Existing Test Execution**:
  - Command: `go test -v -race ./pkg/protocol/...`
  - Result: All 22 canonical struct unit tests passed successfully.

- **Git Branch Status**:
  - Command: `git status && git branch -a`
  - Observation: Repository was on branch `issue-6-canonical-agent-event-schema`.
  - Command executed: `git checkout -b issue-7-capability-manifest-negotiation`
  - Result: Successfully created and switched to new working branch `issue-7-capability-manifest-negotiation`.

---

## 2. Logic Chain

1. **Schema Non-Disruption**: `CapabilityManifest` is already defined in `pkg/protocol/schema.go` and verified against `pkg/protocol/schemas/capability_manifest.json` (which enforces `additionalProperties: false`). Modifying the `CapabilityManifest` struct in `schema.go` or adding JSON fields could break schema validation.
2. **Capability Engine Separation**: By creating `pkg/protocol/capability.go`, we can define:
   - 20 `CapabilityFlag uint64` constants across 4 categories (`Observation/Inspection`, `Control/Interventions`, `Integration/Architecture`, `Model/Provider`).
   - `HandshakeRequest` and `HandshakeResponse` structs.
   - `CapabilityManifest` helper methods (`ToBitmask()`, `FromBitmask()`, `HasCapability()`).
   - `EvaluateAchievableLevel()` and `NegotiateLevel()`.
   This keeps `schema.go` completely untouched while adding all required capability logic in the same package `protocol`.
3. **Bitmask & Level Mapping**:
   - `ToBitmask()` maps the boolean fields (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`) and base capability flags corresponding to `IntegrationLevel` into a `uint64` bitmask.
   - `FromBitmask(mask uint64)` populates `SupportsPause` (`mask & CapPause != 0`), `SupportsCancel`, etc.
   - `EvaluateAchievableLevel()` determines the maximum achievable supervision level (Level 0: Observe, Level 1: Advisory, Level 2: Guarded, Level 3: Full-control) based on required flags for each level.
   - `NegotiateLevel()` compares requested level against achievable level, setting `IsDegraded: true`, `DegradedFrom: req.RequestedLevel`, and listing missing flags when degradation occurs.

---

## 3. Caveats

No caveats. All file locations, JSON schemas, Go structs, and Git branch statuses were directly inspected and verified in the workspace.

---

## 4. Conclusion

- **Git Branch Status**: Branch `issue-7-capability-manifest-negotiation` created and active.
- **Implementation Strategy**:
  1. Create `pkg/protocol/capability.go`:
     - Define `CapabilityFlag uint64` and 20 flag constants:
       - `CapEventStream` (1<<0), `CapToolInspection` (1<<1), `CapDiffInspection` (1<<2), `CapCostTracking` (1<<3), `CapHooks` (1<<4)
       - `CapHeadless` (1<<5), `CapCLIControl` (1<<6), `CapPause` (1<<7), `CapCancel` (1<<8), `CapResume` (1<<9), `CapCheckpoint` (1<<10), `CapRollback` (1<<11)
       - `CapMCP` (1<<12), `CapSubagents` (1<<13), `CapExtensions` (1<<14), `CapSDK` (1<<19)
       - `CapSwitchModel` (1<<15), `CapCustomProvider` (1<<16), `CapOpenAICompat` (1<<17), `CapLocalModels` (1<<18)
     - Add `CapabilityManifest` helper methods (`ToBitmask()`, `FromBitmask()`, `HasCapability()`).
     - Define `HandshakeRequest` and `HandshakeResponse` structs.
     - Implement `EvaluateAchievableLevel(manifest *CapabilityManifest) int`.
     - Implement `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)`.
  2. Create `pkg/protocol/capability_test.go`:
     - Unit tests for bitmask operations, level calculation, negotiation degradation, nil/invalid inputs.
     - Multi-goroutine concurrent race test (`go test -v -race ./pkg/protocol/...`).

---

## 5. Verification Method

- **Test Command**:
  ```bash
  go test -v -race ./pkg/protocol/...
  ```
- **Files to Inspect**:
  - `/Users/iml1s/Documents/mine/reinframe/pkg/protocol/capability.go`
  - `/Users/iml1s/Documents/mine/reinframe/pkg/protocol/capability_test.go`
  - `/Users/iml1s/Documents/mine/reinframe/pkg/protocol/schema.go`
- **Invalidation Conditions**:
  - Failure of `go test -v -race ./pkg/protocol/...`.
  - Failure of `NegotiateLevel` to flag degradation or missing flags accurately.
  - Any compilation or schema validation errors.
