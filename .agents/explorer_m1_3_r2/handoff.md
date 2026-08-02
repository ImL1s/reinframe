# Handoff Report — Explorer 3 (Milestone 1 / Issue #7: Capability Manifest & Handshake Protocol — Iteration 2)

## 1. Observation

- **Target Files Inspected**:
  - `pkg/protocol/capability.go`
  - `pkg/protocol/schema.go`
  - `pkg/protocol/schemas/capability_manifest.json`
  - `pkg/protocol/capability_test.go`
  - `pkg/protocol/challenger_stress_test.go`
  - `pkg/protocol/adversarial_stress_test.go`
  - `pkg/protocol/validator.go`
  - `.agents/reviewer_m1_2/handoff.md`

- **Verbatim Data Model Definitions**:
  - `CapabilityManifest` (`pkg/protocol/schema.go:196-206`):
    ```go
    type CapabilityManifest struct {
        AgentID            string `json:"agent_id" redact:"none"`
        Version            string `json:"version" redact:"none"`
        IntegrationLevel   int    `json:"integration_level" redact:"none"`
        SupportsPause      bool   `json:"supports_pause" redact:"none"`
        SupportsCancel     bool   `json:"supports_cancel" redact:"none"`
        SupportsResume     bool   `json:"supports_resume" redact:"none"`
        SupportsCheckpoint bool   `json:"supports_checkpoint" redact:"none"`
        SupportsRollback   bool   `json:"supports_rollback" redact:"none"`
        SupportsMCP        bool   `json:"supports_mcp" redact:"none"`
    }
    ```

  - `capability_manifest.json` schema (`pkg/protocol/schemas/capability_manifest.json:1-51`):
    ```json
    {
      "$schema": "http://json-schema.org/draft-07/schema#",
      "$id": "https://reinframe.dev/schemas/capability_manifest.json",
      "title": "CapabilityManifest",
      "type": "object",
      "required": [
        "agent_id", "version", "integration_level",
        "supports_pause", "supports_cancel", "supports_resume",
        "supports_checkpoint", "supports_rollback", "supports_mcp"
      ],
      "properties": {
        "agent_id": { "type": "string", "minLength": 1 },
        "version": { "type": "string", "minLength": 1 },
        "integration_level": { "type": "integer", "minimum": 0, "maximum": 3 },
        "supports_pause": { "type": "boolean" },
        "supports_cancel": { "type": "boolean" },
        "supports_resume": { "type": "boolean" },
        "supports_checkpoint": { "type": "boolean" },
        "supports_rollback": { "type": "boolean" },
        "supports_mcp": { "type": "boolean" }
      },
      "additionalProperties": false
    }
    ```

  - `CapabilityFlag` constants (`pkg/protocol/capability.go:8-39`):
    - 20 canonical flags across 4 categories: bits 0..19 (`CapEventStream`=bit 0 through `CapSDK`=bit 19).

  - Conversion logic in `capability.go:101-160`:
    - `ToBitmask()` reconstructs bitmask from `IntegrationLevel` (ORing required level masks: Level 0 `0x1`, Level 1 `0x183`, Level 2 `0xd87`, Level 3 `0xafc7`) plus the 6 explicit boolean flags (Pause=bit 7, Cancel=bit 8, Resume=bit 9, Checkpoint=bit 10, Rollback=bit 11, MCP=bit 12).
    - `FromBitmask(mask uint64)` populates the 6 boolean flags from mask and sets `IntegrationLevel = EvaluateAchievableLevelFromMask(mask)`.

- **Reviewer 2 Iteration 1 Failure Observation**:
  - `TestChallenger_BoundaryBitmasks` passed raw uint64 masks (`0x0`, `1<<63`, `1<<20`, `1<<19`) into `FromBitmask(mask)` and then called `manifest.HasCapability(flag)` expecting lossless bitwise equality.
  - Subtests failed because:
    1. `FromBitmask(0)` evaluated `IntegrationLevel = 0`. Calling `ToBitmask()` on `IntegrationLevel 0` set `Level0RequiredMask` (`CapEventStream` bit 0 = 0x1), causing `HasCapability(CapEventStream)` to return `true` instead of `false`.
    2. Bit 19 (`CapSDK`), undefined bit 20 (`1<<20`), and highest bit 63 (`1<<63`) are not stored as individual boolean fields on `CapabilityManifest` nor part of `Level0RequiredMask`. They were discarded during `FromBitmask(mask)` conversion, returning `false` for `HasCapability`.
    3. Off-by-one masks (e.g. clearing `CapPause` from Level 1 mask) caused `IntegrationLevel` to degrade to Level 0, discarding bit 1 (`CapToolInspection`) which was only implicitly represented by Level 1.

- **Current Repository Test Output**:
  - Command: `go test -v -count=1 -race ./pkg/protocol/...`
  - Result: Exit code 0 (PASS, 0 data races, 0 failures).

---

## 2. Logic Chain

1. **Option B Evaluation (Adding uint64 bitmask storage to `CapabilityManifest`)**:
   - **Hypothesis**: Add a `RawBitmask uint64` field (e.g., `json:"raw_bitmask,omitempty"`) or internal bitmask field to `CapabilityManifest`.
   - **Flaws & Invalidation**:
     a. **JSON Schema Violation**: `pkg/protocol/schemas/capability_manifest.json` specifies `"additionalProperties": false`. If `raw_bitmask` is added to the Go struct and serialized to JSON, `ValidateEvent(payload, "capability_manifest")` will fail schema validation.
     b. **Schema Immutability Constraint**: Issue #6 established the 22 canonical schema contracts. Requirement 3 explicitly states: "Ensure the fix maintains JSON schema compatibility (`pkg/protocol/schemas/capability_manifest.json` uses `additionalProperties: false`)."
     c. **Transport Split-Brain**: If `rawBitmask` is made an unexported Go field, it will not survive JSON marshaling/unmarshaling over JSON-RPC / NDJSON network boundaries. A `CapabilityManifest` received from a remote agent client would lose `rawBitmask`, breaking consistency between local and RPC-initialized manifests.
     d. **Domain Boundary Mismatch**: `CapabilityManifest` represents the structured JSON handshake declaration. Storing arbitrary 64-bit integers (including undefined bits 20..63) inside `CapabilityManifest` breaks the domain abstraction.

2. **Option A Evaluation (`TestChallenger_BoundaryBitmasks` domain mismatch)**:
   - **Domain Definition**: `CapabilityManifest`'s domain is strictly defined by its 9 fields: `agent_id`, `version`, `integration_level` (0..3), and 6 boolean capability flags (`supports_pause`, `supports_cancel`, `supports_resume`, `supports_checkpoint`, `supports_rollback`, `supports_mcp`).
   - **Lossy Projection**: `FromBitmask(mask uint64)` is a projection from the 64-bit uint64 space into `CapabilityManifest`'s domain model.
   - **Representation Limits**:
     - Undefined bits (bits 20..63) are outside Reinframe's capability model.
     - Flags not defined as struct boolean fields (e.g., `CapSDK` bit 19 or `CapToolInspection` bit 1) are represented through supervision level thresholds (`IntegrationLevel`), not as individual struct fields.
   - **Root Cause of Test Failure**: `TestChallenger_BoundaryBitmasks` assumed `FromBitmask` -> `CapabilityManifest` -> `ToBitmask` was an identity bijection for all 2^64 raw bitmask values. In reality, `CapabilityManifest` is a structured domain object, not a raw uint64 container.
   - **Correct Testing Partition**:
     - Raw uint64 bitmask evaluation MUST be tested directly using `EvaluateAchievableLevelFromMask(mask uint64)` and bitwise operations `(mask & uint64(flag)) != 0` (as implemented in `TestAdversarialCapability_BitFlips` in `adversarial_stress_test.go`).
     - `CapabilityManifest` domain helpers MUST be tested against valid struct instances and achievable level round-trips within its defined domain (as implemented in `TestCapabilityManifest_BitmaskHelpers` and `TestChallenger_BooleanCombinationsRoundTrip`).

---

## 3. Caveats

- **Read-Only Investigation**: Explorer 3 performed read-only analysis and did NOT modify any Go source files or test files in `pkg/protocol/`.
- **Scope Limit**: Investigation focused on `pkg/protocol/capability.go`, `schema.go`, `schemas/capability_manifest.json`, and associated test suites. `pkg/state` (Issue #9) is outside this investigation scope.

---

## 4. Conclusion

- **Determination**: `TestChallenger_BoundaryBitmasks` was testing raw uint64 bitmasks (such as bit 63 or undefined bit 20) that exceed the domain of `CapabilityManifest` (which consists of 20 capability flags across bits 0..19 mapped to 9 struct fields).
- **Recommendation**:
  1. `capability.go` and `CapabilityManifest` struct MUST NOT be modified to add a raw uint64 bitmask field, preserving strict JSON schema compatibility (`additionalProperties: false`).
  2. Tests must evaluate raw uint64 bitmasks via `EvaluateAchievableLevelFromMask(mask)` and test `CapabilityManifest` within its 9-field domain model.
  3. Current test suite in `pkg/protocol/` (`capability_test.go`, `adversarial_stress_test.go`, `challenger_stress_test.go`) already implements this separation cleanly and passes `go test -v -count=1 -race ./pkg/protocol/...` with zero failures and zero race conditions.

---

## 5. Verification Method

To independently verify this analysis:

1. **Verify JSON Schema Validation**:
   ```bash
   go test -v -count=1 -race -run TestValidateEvent ./pkg/protocol/...
   ```
   Confirm that `capability_manifest` payloads strictly match `pkg/protocol/schemas/capability_manifest.json`.

2. **Verify Full Protocol Test Suite & Race Detector**:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
   Confirm exit code 0 with 0 failing subtests and 0 race warnings.

3. **Verify Bitmask & Negotiation Test Suite**:
   ```bash
   go test -v -count=1 -race -run "TestCapability|TestEvaluate|TestNegotiate|TestAdversarialCapability" ./pkg/protocol/...
   ```
