# Handoff Report: Explorer 2 (Capability Manifest & Handshake Protocol - Issue #7)

## 1. Observation

Direct observations from codebase inspection, specification documents, and workspace state:

1. **Mandatory Specification & Scope Requirements**:
   - `PROJECT.md` (lines 33-74) specifies `pkg/protocol/capability.go` containing `CapabilityFlag uint64` constants, `HandshakeRequest`, `HandshakeResponse`, and `NegotiateLevel`.
   - `SCOPE.md` (lines 8-16) requires:
     - 20 `CapabilityFlag uint64` constants across 4 categories.
     - `CapabilityManifest` helper methods: `ToBitmask()`, `FromBitmask()`, `HasCapability()`.
     - `EvaluateAchievableLevel()` calculating maximum achievable supervision level (Level 0 to Level 3).
     - `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)` with automatic degradation (`IsDegraded: true`, `DegradedFrom`, `MissingFlags`).
     - Unit tests in `pkg/protocol/capability_test.go` verifying bitmask conversions, level calculation, degradation, and race safety.

2. **Existing Schema Inspection**:
   - File `pkg/protocol/schema.go` (lines 196-206):
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
   - Schema JSON file `pkg/protocol/schemas/capability_manifest.json` requires: `agent_id`, `version`, `integration_level`, `supports_pause`, `supports_cancel`, `supports_resume`, `supports_checkpoint`, `supports_rollback`, `supports_mcp`.

3. **Test Suite Baseline**:
   - Executed `go test -v ./pkg/protocol/...` in workspace root.
   - Result: All tests passed (100% pass rate) for schema validation, JSON roundtrip, and redaction metadata tags.

---

## 2. Logic Chain

Step-by-step reasoning from observations to implementation strategy:

### Step 2.1: 20 CapabilityFlag Definitions across 4 Categories
From requirement analysis, 20 `CapabilityFlag` constants must be declared as `uint64` bitmask shifts (`1 << iota`), categorized into 4 domain categories:

#### Category 1: Observation & Telemetry Capabilities (Bits 0–4)
1. `CapEventStream` (`1 << 0` = `0x1`): Real-time NDJSON event streaming to supervisor harness.
2. `CapToolInspection` (`1 << 1` = `0x2`): Tool argument and output payload telemetry inspection.
3. `CapDiffInspection` (`1 << 2` = `0x4`): Line-by-line file diff and scope violation inspection.
4. `CapCostTracking` (`1 << 3` = `0x8`): Real-time LLM token usage and USD cost tracking.
5. `CapHooks` (`1 << 4` = `0x10`): Lifecycle hook execution interception.

#### Category 2: Control & Intervention Capabilities (Bits 5–9)
6. `CapHeadless` (`1 << 5` = `0x20`): Headless unattended process execution.
7. `CapCLIControl` (`1 << 6` = `0x40`): CLI standard I/O control and signal injection.
8. `CapPause` (`1 << 7` = `0x80`): Session pause execution capability.
9. `CapCancel` (`1 << 8` = `0x100`): Session cancellation capability.
10. `CapResume` (`1 << 9` = `0x200`): Session resume capability.

#### Category 3: Workspace & State Management Capabilities (Bits 10–14)
11. `CapCheckpoint` (`1 << 10` = `0x400`): Worktree Git commit and state snapshot creation.
12. `CapRollback` (`1 << 11` = `0x800`): Worktree Git commit restoration and rollback execution.
13. `CapMCP` (`1 << 12` = `0x1000`): Model Context Protocol tool container isolation.
14. `CapSubagents` (`1 << 13` = `0x2000`): Child subagent process spawning and hierarchy management.
15. `CapExtensions` (`1 << 14` = `0x4000`): Harness extension plugin loading.

#### Category 4: Model & Integration Ecosystem Capabilities (Bits 15–19)
16. `CapSwitchModel` (`1 << 15` = `0x8000`): Dynamic LLM model switching at runtime.
17. `CapCustomProvider` (`1 << 16` = `0x10000`): Custom API endpoint / provider integration.
18. `CapOpenAICompat` (`1 << 17` = `0x20000`): OpenAI-compatible API protocol compliance.
19. `CapLocalModels` (`1 << 18` = `0x40000`): Local LLM runner (Ollama/llama.cpp) integration.
20. `CapSDK` (`1 << 19` = `0x80000`): Native language SDK binding support.

---

### Step 2.2: CapabilityManifest Helper Methods & Bitmask Serialization

To support flexible capability representation:
1. `CapabilityManifest` will be extended with boolean fields for all 20 flags (maintaining json tags compatible with `capability_manifest.json` schema) or a `CustomBitmask uint64` field.
2. `ToBitmask() uint64`:
   - Scans all boolean flags in `CapabilityManifest` and combines bit shifts into a single `uint64`.
   - Formula:
     ```go
     func (m CapabilityManifest) ToBitmask() uint64 {
         var bm uint64
         if m.SupportsEventStream { bm |= uint64(CapEventStream) }
         if m.SupportsToolInspection { bm |= uint64(CapToolInspection) }
         if m.SupportsDiffInspection { bm |= uint64(CapDiffInspection) }
         if m.SupportsCostTracking { bm |= uint64(CapCostTracking) }
         if m.SupportsHooks { bm |= uint64(CapHooks) }
         if m.SupportsHeadless { bm |= uint64(CapHeadless) }
         if m.SupportsCLIControl { bm |= uint64(CapCLIControl) }
         if m.SupportsPause { bm |= uint64(CapPause) }
         if m.SupportsCancel { bm |= uint64(CapCancel) }
         if m.SupportsResume { bm |= uint64(CapResume) }
         if m.SupportsCheckpoint { bm |= uint64(CapCheckpoint) }
         if m.SupportsRollback { bm |= uint64(CapRollback) }
         if m.SupportsMCP { bm |= uint64(CapMCP) }
         if m.SupportsSubagents { bm |= uint64(CapSubagents) }
         if m.SupportsExtensions { bm |= uint64(CapExtensions) }
         if m.SupportsSwitchModel { bm |= uint64(CapSwitchModel) }
         if m.SupportsCustomProvider { bm |= uint64(CapCustomProvider) }
         if m.SupportsOpenAICompat { bm |= uint64(CapOpenAICompat) }
         if m.SupportsLocalModels { bm |= uint64(CapLocalModels) }
         if m.SupportsSDK { bm |= uint64(CapSDK) }
         return bm
     }
     ```
3. `FromBitmask(bitmask uint64) CapabilityManifest`:
   - Restores a `CapabilityManifest` struct with boolean fields set corresponding to bits set in `bitmask`.
4. `HasCapability(flag CapabilityFlag) bool`:
   - Evaluates whether `m.ToBitmask() & uint64(flag) == uint64(flag)`.
5. Flag Stringer / Name mapping (`FlagToString(flag CapabilityFlag) string`):
   - Returns string representation (e.g. `"CapPause"` or `"CapEventStream"`) for reporting in `MissingFlags`.

---

### Step 2.3: Supervision Level Threshold Evaluation (`EvaluateAchievableLevel`)

Supervision levels require cumulative combinations of flags:
- **Level 0 (Observe)**:
  - Required Mask: `CapEventStream`
  - Bitmask: `0x1`
- **Level 1 (Advisory)**:
  - Required Mask: `CapEventStream | CapToolInspection | CapPause | CapCancel | CapResume`
  - Bitmask: `0x1 | 0x2 | 0x80 | 0x100 | 0x200` = `0x383`
- **Level 2 (Guarded)**:
  - Required Mask: `RequiredLevel1Mask | CapDiffInspection | CapCheckpoint | CapRollback`
  - Bitmask: `0x383 | 0x4 | 0x400 | 0x800` = `0xF87`
- **Level 3 (Full-control)**:
  - Required Mask: `RequiredLevel2Mask | CapHeadless | CapCLIControl | CapMCP | CapSubagents | CapSwitchModel`
  - Bitmask: `0xF87 | 0x20 | 0x40 | 0x1000 | 0x2000 | 0x8000` = `0xBFE7`

Logic in `EvaluateAchievableLevel(bitmask uint64) int`:
1. If `(bitmask & MaskLevel3) == MaskLevel3` -> Return `3`
2. Else if `(bitmask & MaskLevel2) == MaskLevel2` -> Return `2`
3. Else if `(bitmask & MaskLevel1) == MaskLevel1` -> Return `1`
4. Else if `(bitmask & MaskLevel0) == MaskLevel0` -> Return `0`
5. Else -> Return `0` (Level 0 observe baseline with degraded capabilities if streaming absent).

---

## 3. Caveats

1. **Schema JSON Validation vs Struct Extension**:
   - `CapabilityManifest` in `pkg/protocol/schema.go` currently defines 6 boolean fields (`supports_pause`, `supports_cancel`, `supports_resume`, `supports_checkpoint`, `supports_rollback`, `supports_mcp`).
   - If adding boolean fields for the remaining 14 capability flags to `CapabilityManifest`, `pkg/protocol/schemas/capability_manifest.json` schema MUST either allow additional properties or be updated to list those properties so `ValidateEvent` passes without errors.
2. **Missing Flags Identification**:
   - When degrading level from target `L_req` to achievable `L_achieved`, `MissingFlags` should list flags required for `L_req` that are bitwise missing from the agent's bitmask.

---

## 4. Conclusion

The capability flag bitmask model, manifest conversion helpers, and supervision level evaluator have a clean, mathematical design:
- 20 uint64 bitmask flags organized in 4 distinct operational categories.
- Level thresholds (Level 0, 1, 2, 3) mapped to explicit bitmasks `MaskLevel0..3`.
- Robust `NegotiateLevel` automatic degradation logic returning structured `HandshakeResponse`.

---

## 5. Verification Method

To verify the implementation once written by Worker:

1. **Run Unit & Race Tests**:
   ```bash
   go test -v -race ./pkg/protocol/...
   ```
2. **Key Test Cases to Validate**:
   - Bitmask round-trip: `CapabilityManifest.FromBitmask(m.ToBitmask()) == m`.
   - `HasCapability()` correctly evaluates single and combined flags.
   - `EvaluateAchievableLevel()` correctly returns 0, 1, 2, or 3 based on exact flag sets.
   - `NegotiateLevel()` correctly identifies degradation, populates `DegradedFrom`, `IsDegraded`, and `MissingFlags`.
