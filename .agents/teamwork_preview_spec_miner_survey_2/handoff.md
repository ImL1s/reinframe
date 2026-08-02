# Handoff Report: Issue #7 — Capability Manifest & Handshake Protocol Specification

## 1. Observation
From authoritative project documents (`ORIGINAL_REQUEST.md`, `PROJECT.md`, `docs/research/harness_capability_matrix.md`, `docs/architecture/dag_and_execution_plan.md`) and codebase files (`pkg/protocol/schema.go`, `pkg/protocol/schemas/capability_manifest.json`), the following facts were observed:

1. **Original Specification Baseline**:
   - `ORIGINAL_REQUEST.md` (lines 40-41): "Build CapabilityManifest struct, 20 capability flags, and negotiation engine (pkg/protocol/capability.go) supporting Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), Level 3 (Full-control) with automatic degradation. Write unit tests in pkg/protocol/capability_test.go."
   - Target Branch: `issue-7-capability-manifest-negotiation`
   - Verification Requirement: `go test -race ./pkg/protocol/...`

2. **Existing Schema Model**:
   - `pkg/protocol/schema.go` (lines 195-206): `CapabilityManifest` struct exists with basic fields (`AgentID`, `Version`, `IntegrationLevel`, `SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`).
   - `pkg/protocol/schemas/capability_manifest.json`: Validates Draft-07 JSON payloads for `CapabilityManifest`.

3. **Harness Capability Surface Dimensions**:
   - `docs/research/harness_capability_matrix.md` (lines 15-40): Evaluates 12+ framework harnesses across operational dimensions that form the basis of the 20 capability flags.
   - `docs/research/harness_capability_matrix.md` (lines 44-65): Explicitly defines Integration Levels 0-3.

---

## 2. Logic Chain

1. **Capability Flags Architecture**:
   - To support high-performance bitwise checks alongside JSON schema serialization, the 20 capability flags must be represented both as typed bitmask constants (`CapabilityFlag uint64`) and as fields within `CapabilityManifest`.
   - The 20 flags group naturally into 4 operational categories:
     - **Category 1: Observation & Telemetry** (EventStream, ToolInspection, DiffInspection, CostTracking, HookSupport)
     - **Category 2: Process & Control** (Headless, CLIControl, Pause, Cancel, Resume)
     - **Category 3: State & Worktree Control** (Checkpoint, Rollback, MCPSupport, Subagents, Extensions)
     - **Category 4: Model & Execution Flexibility** (SwitchModel, CustomProvider, OpenAICompat, LocalModels, SDKSupport)

2. **Negotiation & Level Evaluation Logic**:
   - Integration Level represents the maximum supervision level the harness can safely support:
     - **Level 0 (Observe-only)**: Passive log/diff observation. Fallback for minimal capabilities.
     - **Level 1 (Advisory)**: Requires observation + task cancellation / advisory prompt injection.
     - **Level 2 (Guarded)**: Requires Level 1 + process pause + lifecycle hook inspection / tool call blocking.
     - **Level 3 (Full-control)**: Requires Level 2 + state checkpointing + git workspace rollback + model/subagent switching.
   - The `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)` engine calculates the maximum achievable level based on manifest flags. If requested level > maximum achievable level, it triggers automatic degradation, setting `IsDegraded = true` and populating missing capability flag details.

3. **Handshake Protocol Engine Structure**:
   - Files to create in `pkg/protocol/`:
     - `pkg/protocol/capability.go`: Contains flag constants, bitmask converters, level thresholds, `CapabilityManifest` helpers, and `NegotiateLevel` function.
     - `pkg/protocol/capability_test.go`: Unit tests for capability flags, bitmask conversion, level calculation, automatic degradation, and race conditions (`go test -race`).

---

## 3. Caveats
- `CapabilityManifest` in `pkg/protocol/schema.go` contains 6 boolean fields (`SupportsPause`, `SupportsCancel`, etc.). `pkg/protocol/capability.go` should maintain backward compatibility with `schema.go` while extending capability flag coverage to all 20 flags (e.g. via helper methods `ToBitmask()`, `FromBitmask()`, `HasCapability(flag CapabilityFlag)`).
- Automatic degradation must be deterministic: if multiple flags are missing for a requested level, all missing flags must be returned in `MissingFlags` slice in predictable sorted order.

---

## 4. Conclusion

### Features Discovered

| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Core Protocol | `CapabilityFlag` Bitmask Definitions | 20 bitmask constants (`uint64`) defining agent capabilities across 4 categories | Capability constant | `uint64` bitmask value | Invalid flag check returns false | `ORIGINAL_REQUEST.md`, `harness_capability_matrix.md` |
| 2 | Protocol Model | `CapabilityManifest` Struct Extension | Struct model with 20 capability booleans, `AgentID`, `Version`, `IntegrationLevel`, and `Flags` uint64 | Manifest fields | Struct object | Validation via `ValidateEvent` | `pkg/protocol/schema.go`, `capability_manifest.json` |
| 3 | Negotiation Engine | `NegotiateLevel` Function | Evaluates requested vs achievable level (0-3) based on manifest capability flags | `HandshakeRequest` | `HandshakeResponse` | Returns `ErrNilManifest`, `ErrInvalidLevel`, or degrades safely | `ORIGINAL_REQUEST.md` (R1) |
| 4 | Protocol Flow | Automatic Level Degradation | Automatically degrades supervision level if agent lacks required flags for requested level | Manifest with missing required flags | Response with `IsDegraded: true`, `DegradedFrom`, `MissingFlags` | Non-fatal; logs degradation reason and degrades level | `ORIGINAL_REQUEST.md` (R1, Acceptance Criteria) |
| 5 | Helper Utility | `Manifest.ToBitmask()` & `FromBitmask()` | Converts between boolean fields in `CapabilityManifest` and `uint64` bitmask representation | `CapabilityManifest` / `uint64` | `uint64` / `CapabilityManifest` | Safe zero-value defaults on empty payload | `pkg/protocol/capability.go` design |
| 6 | Helper Utility | `Manifest.HasCapability(flag)` | Bitwise check confirming whether a specific capability flag is enabled | `CapabilityFlag` | `bool` | Returns `false` for unset flags | `pkg/protocol/capability.go` design |
| 7 | Protocol Engine | Level Threshold Evaluator | `EvaluateAchievableLevel(manifest)` determines max safe level (0, 1, 2, or 3) | `CapabilityManifest` | `int` (Level 0-3) | Defaults to Level 0 on insufficient flags | `harness_capability_matrix.md` |
| 8 | Handshake API | `ExecuteHandshake(payload []byte)` | NDJSON protocol handshake handler parsing JSON request and executing negotiation | Raw JSON byte slice | Raw JSON byte slice (`HandshakeResponse`) | Returns JSON-RPC / NDJSON error payload on malformed input | `ORIGINAL_REQUEST.md`, `PROJECT.md` |

### Detailed 20 Capability Flags Inventory

| Category | Flag Name | JSON Property | Bitmask Value | Description |
|---|---|---|---|---|
| **Observation & Telemetry** | `CapEventStream` | `supports_event_stream` | `1 << 0` | Real-time NDJSON event stream emission |
| **Observation & Telemetry** | `CapToolInspection` | `supports_tool_inspection` | `1 << 1` | Inspect tool call inputs and outputs |
| **Observation & Telemetry** | `CapDiffInspection` | `supports_diff_inspection` | `1 << 2` | File diff / workspace patch capture |
| **Observation & Telemetry** | `CapCostTracking` | `supports_cost_tracking` | `1 << 3` | Token usage and monetary cost tracking |
| **Observation & Telemetry** | `CapHooks` | `supports_hooks` | `1 << 4` | Pre/post tool call lifecycle event hooks |
| **Process & Control** | `CapHeadless` | `supports_headless` | `1 << 5` | Non-interactive background execution |
| **Process & Control** | `CapCLIControl` | `supports_cli_control` | `1 << 6` | Command-line process control / signals |
| **Process & Control** | `CapPause` | `supports_pause` | `1 << 7` | Dynamic process pause capability |
| **Process & Control** | `CapCancel` | `supports_cancel` | `1 << 8` | Active task cancellation capability |
| **Process & Control** | `CapResume` | `supports_resume` | `1 << 9` | Paused session resumption capability |
| **State & Worktree** | `CapCheckpoint` | `supports_checkpoint` | `1 << 10` | Workspace memory / state checkpointing |
| **State & Worktree** | `CapRollback` | `supports_rollback` | `1 << 11` | Git workspace / state tree rollback |
| **State & Worktree** | `CapMCP` | `supports_mcp` | `1 << 12` | Model Context Protocol support |
| **State & Worktree** | `CapSubagents` | `supports_subagents` | `1 << 13` | Native subagent thread orchestration |
| **State & Worktree** | `CapExtensions` | `supports_extensions` | `1 << 14` | IDE / plugin extension hooks |
| **Model & Execution** | `CapSwitchModel` | `supports_switch_model` | `1 << 15` | Runtime model switching support |
| **Model & Execution** | `CapCustomProvider` | `supports_custom_provider` | `1 << 16` | Custom LLM provider endpoint support |
| **Model & Execution** | `CapOpenAICompat` | `supports_openai_compat` | `1 << 17` | OpenAI REST API compatibility |
| **Model & Execution** | `CapLocalModels` | `supports_local_models` | `1 << 18` | Local model runtime support (Ollama/vLLM) |
| **Model & Execution** | `CapSDK` | `supports_sdk` | `1 << 19` | Direct programmatic SDK bindings |

### Integration Level Capability Mapping Matrix

| Integration Level | Required Capability Flags | Fallback / Degradation Action |
|---|---|---|
| **Level 0 (Observe)** | Basic observation (`CapEventStream` OR `CapDiffInspection`) | None (Base Level) |
| **Level 1 (Advisory)** | Level 0 + `CapCancel` (or `CapCLIControl` / `CapHooks`) | Degrades to Level 0 if `CapCancel` missing |
| **Level 2 (Guarded)** | Level 1 + `CapPause` + `CapHooks` (or `CapToolInspection`) | Degrades to Level 1 if `CapPause` or `CapHooks` missing |
| **Level 3 (Full-control)**| Level 2 + `CapCheckpoint` + `CapRollback` + `CapSwitchModel` | Degrades to Level 2 if `CapCheckpoint` or `CapRollback` missing |

### Edge Cases

| # | Feature | Input | Observed / Specified Behavior |
|---|---------|-------|-------------------|
| 1 | Handshake | `HandshakeRequest` with `nil` manifest | Returns `ErrNilManifest`; defaults session to Level 0 |
| 2 | Level Negotiation | `RequestedLevel = 3`, manifest lacks `CapRollback` | Negotiated level = 2; `IsDegraded = true`; `MissingFlags = ["supports_rollback"]` |
| 3 | Level Negotiation | `RequestedLevel = 2`, manifest lacks `CapPause` | Negotiated level = 1; `IsDegraded = true`; `MissingFlags = ["supports_pause"]` |
| 4 | Level Negotiation | `RequestedLevel = 99` (Out of bounds) | Clamps/degrades to max achievable level [0-3]; sets degradation reason |
| 5 | Level Negotiation | Manifest with 0 capability flags (`0` bitmask) | Negotiates to Level 0 (Observe-only) |
| 6 | JSON Handshake | Malformed JSON handshake byte slice | Returns syntax error / JSON schema validation error via `ValidateEvent` |
| 7 | Concurrent Handshakes | Multiple goroutines calling `NegotiateLevel` concurrently | Pure stateless calculation; 100% thread-safe (passes `go test -race`) |
| 8 | Bitmask Roundtrip | Manifest with all 20 flags set to true | `ToBitmask()` returns `0xFFFFF` (1048575); `FromBitmask` restores all 20 boolean fields |

---

## 5. Verification Method

To verify the implementation of Issue #7 when built:

1. **Branch Verification**:
   ```bash
   git checkout -b issue-7-capability-manifest-negotiation
   ```

2. **Unit Test Execution & Race Detector**:
   ```bash
   go test -v -race ./pkg/protocol/...
   ```
   *Expected Output*: `PASS` with zero data races detected across all tests in `pkg/protocol/capability_test.go` and `pkg/protocol/schema_test.go`.

3. **Specific Test Scenarios to Verify in `capability_test.go`**:
   - `TestCapabilityFlags_BitmaskConversion`: Verify 20 bitmasks map 1-to-1 with manifest boolean fields.
   - `TestNegotiateLevel_ExactMatch`: Verify requesting Level 0, 1, 2, 3 with full capabilities returns exact level without degradation.
   - `TestNegotiateLevel_AutomaticDegradation`: Table-driven test verifying degradation from L3 $\rightarrow$ L2, L2 $\rightarrow$ L1, L1 $\rightarrow$ L0 when missing required flags.
   - `TestNegotiateLevel_EdgeCases`: Verify nil manifests, negative levels, out-of-bound levels, zero bitmasks.
   - `TestNegotiateLevel_ConcurrentRace`: Test running 100 parallel goroutines calling `NegotiateLevel` with `-race`.
