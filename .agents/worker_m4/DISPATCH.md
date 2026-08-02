## 2026-08-02T06:52:29Z
You are teamwork_preview_worker for Milestone M4 (Capability Test Suite Rewrite).
Your working directory is: /Users/iml1s/Documents/mine/reinframe.
Your workspace folder is: /Users/iml1s/Documents/mine/reinframe/.agents/worker_m4.
Path to user request: /Users/iml1s/Documents/mine/reinframe/docs/dev/ORIGINAL_REQUEST.md. Read this file FIRST.
Path to Explorer 2 survey analysis: /Users/iml1s/Documents/mine/reinframe/.agents/explorer_survey_2/analysis.md. Read this file for exact test rewrite specifications.

Write Ownership Boundary: You exclusively own `pkg/protocol/capability_test.go` and `pkg/protocol/challenger2_stress_test.go`. Do NOT modify files outside these.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Tasks for M4:
1. Rewrite `pkg/protocol/capability_test.go`: Remove all test assertions that expect `ToBitmask()` to auto-grant capabilities based on `IntegrationLevel`.
2. Update tests to verify that `ToBitmask()` constructs bitmask strictly from explicit boolean fields.
3. Test that missing required boolean fields result in correct degradation or failure according to negotiation policy.
4. Add `TestCapability_JSONRoundTrip_Lossless` to verify that `json.Marshal` -> `json.Unmarshal` round-trip across `CapabilityManifest` with all 20 boolean fields set preserves 100% of capabilities with zero data loss.
5. Verify Level contracts: Test Level 1 (Advisory) without `CapPause`/`CapCancel`/`CapResume` and Level 2 (Guarded) with `CapPause`/`CapCancel`/`CapResume`.
6. Update `pkg/protocol/challenger2_stress_test.go` if necessary to match the updated capability manifest struct and bitmask logic.
7. Run `go test -v -race ./pkg/protocol/...` and ensure 100% of protocol tests pass cleanly.

Write a complete report of your changes and test outputs in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m4/changes.md` and handoff report in `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m4/handoff.md`. Update progress.md in your agent folder regularly. Report back via send_message when done.
