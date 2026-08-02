## 2026-08-02T06:48:30Z
You are teamwork_preview_worker for Milestone M2 (Capability & Schema Fixes).
Your working directory is: /Users/iml1s/Documents/mine/reinframe.
Your workspace folder is: /Users/iml1s/Documents/mine/reinframe/.agents/worker_m2.
Path to user request: /Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md. Read this file FIRST.
Path to project specification: /Users/iml1s/Documents/mine/reinframe/PROJECT.md.
Path to Explorer 2 survey analysis: /Users/iml1s/Documents/mine/reinframe/.agents/explorer_survey_2/analysis.md. Read this file for exact fix specifications.

Write Ownership Boundary: You exclusively own files in `pkg/protocol/` (`capability.go`, `schema.go`, `schemas/*.json`). Do NOT modify `pkg/protocol/capability_test.go` (that is owned by M4) or files outside `pkg/protocol/`.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Tasks for M2:
1. Fix `ToBitmask()` in `pkg/protocol/capability.go`: Remove auto-granting logic based on `IntegrationLevel`. Build bitmask exclusively from explicit boolean properties.
2. Re-align Level contracts: Move process control flags (`CapPause`, `CapCancel`, `CapResume`) from `Level1RequiredMask` to `Level2RequiredMask`.
3. Complete all 20 capability fields in `CapabilityManifest` struct in `pkg/protocol/capability.go` and in `pkg/protocol/schemas/capability_manifest.json`.
4. Harden `ValidateEvent` in `pkg/protocol/schema.go`: (a) Add payload size limit check (e.g. 1MB) before unmarshaling to prevent DoS; (b) Use `json.Decoder.UseNumber()` to prevent float64 precision loss.
5. Add `"RESUME"` to status enum in `pkg/protocol/schemas/agent_session.json`.
6. Add `"maximum": 1` to `max_depth` in `pkg/protocol/schemas/task_envelope.json`.
7. Refactor schema compilation from `sync.Once` lazy loading to Go `init()` fail-fast startup compilation with panic on error.
8. Build and run unit tests in `pkg/protocol/` (note: some legacy capability tests may fail until M4 rewrites them, but schema tests must pass).

Write a complete report of your changes and test outputs in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m2/changes.md` and handoff report in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m2/handoff.md`. Update progress.md in your agent folder regularly. Report back via send_message when done.
