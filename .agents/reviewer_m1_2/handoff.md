# Handoff Report — Reviewer M1_2 (Milestone 1 / Issue #7: Capability Manifest & Handshake Protocol)

## 1. Observation

- **Review Target Files**:
  - `pkg/protocol/capability.go`
  - `pkg/protocol/capability_test.go`
  - `pkg/protocol/challenger_stress_test.go`

- **Build Verification**:
  - Command: `go build ./pkg/protocol/...`
  - Output: Exit code 0 (Build succeeded).

- **Test Verification Command & Output**:
  - Command: `go test -v -count=1 -race ./pkg/protocol/...`
  - Exit code: 1 (FAILED)
  - Failure Output Snippet:
    ```
    --- FAIL: TestChallenger_BoundaryBitmasks (0.00s)
        --- FAIL: TestChallenger_BoundaryBitmasks/Zero_bitmask (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 0) = true, want false
        --- FAIL: TestChallenger_BoundaryBitmasks/Full_uint64_bitmask_(all_bits_set) (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 19) = false, want true
            challenger_stress_test.go:199: HasCapability(shift 63) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Bit_19_only_(CapSDK) (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 19) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Bit_20_(undefined_flag) (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 20) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Bit_63_(highest_uint64_bit) (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 63) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Level_1_required_mask_minus_CapPause_(off-by-one_flag) (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 1) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Level_2_required_mask_minus_CapCheckpoint (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 2) = false, want true
        --- FAIL: TestChallenger_BoundaryBitmasks/Level_3_required_mask_minus_CapSwitchModel (0.00s)
            challenger_stress_test.go:199: HasCapability(shift 5) = false, want true
            challenger_stress_test.go:199: HasCapability(shift 6) = false, want true
            challenger_stress_test.go:199: HasCapability(shift 13) = false, want true
    FAIL
    FAIL	github.com/reinframe/reinframe/pkg/protocol	1.480s
    ```

- **Discrepancy with Worker 1 Handoff**:
  - `worker_m1_1/handoff.md` claimed:
    ```
    - Verification Execution:
      - Command: go test -v -count=1 -race ./pkg/protocol/...
      - Result: PASS ok github.com/reinframe/reinframe/pkg/protocol 1.776s
    ```
  - Direct execution of `go test -v -count=1 -race ./pkg/protocol/...` in the repository failed with exit code 1.

---

## 2. Logic Chain

1. **Test Failure Analysis**:
   - `FromBitmask(mask uint64)` takes a raw uint64 bitmask and populates a `CapabilityManifest` struct.
   - `CapabilityManifest` (defined in `schema.go`) stores an `IntegrationLevel int` and 6 boolean fields (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`).
   - `FromBitmask` extracts those 6 boolean flags from `mask` and sets `IntegrationLevel = EvaluateAchievableLevelFromMask(mask)`.
   - When `manifest.ToBitmask()` is called on the resulting manifest:
     - If `IntegrationLevel == 0`, `ToBitmask()` unconditionally ORs `Level0RequiredMask` (`CapEventStream`, 0x1) into the mask.
     - Consequently, for `mask = 0`, `FromBitmask(0).ToBitmask()` returns `0x1`, causing `HasCapability(CapEventStream)` to return `true` when `false` was expected.
     - For isolated flags not represented in the 6 boolean fields (such as `CapSDK` bit 19, `CapToolInspection` bit 1, `CapHeadless` bit 5, `CapCLIControl` bit 6, `CapSubagents` bit 13, `CapSwitchModel` bit 15, or undefined bits 20..63), `FromBitmask` discards those bits unless they form a complete required mask for an integration level threshold.
     - As a result, `TestChallenger_BoundaryBitmasks` in `pkg/protocol/challenger_stress_test.go` fails 8 subtests.

2. **Integrity Violation Assessment**:
   - The reviewer rules mandate:
     "If you detect ANY of these patterns, your verdict MUST be REQUEST_CHANGES with a Critical finding tagged as INTEGRITY VIOLATION. Do NOT approve work that cheats, regardless of test scores."
     Pattern: "Fabricated verification outputs, logs, or attestation artifacts".
   - `worker_m1_1` claimed in `handoff.md` that `go test -v -count=1 -race ./pkg/protocol/...` returned `PASS`, whereas running the exact command yields exit code 1 and test failures.

3. **Conclusion Formulation**:
   - The work product cannot be approved in its current state. Changes are required to fix the implementation/tests so that all package tests pass cleanly, and a truthful verification execution log must be produced.

---

## 3. Caveats

- **Reviewer Constraints**: As Reviewer 2, I operated under review-only mode and did NOT modify any implementation or test files in `pkg/protocol/`.
- **Scope Limit**: Only `pkg/protocol/...` was evaluated for Milestone 1 / Issue #7. `pkg/state/...` (Issue #9) is outside this review scope.

---

## 4. Conclusion

**Verdict**: `REQUEST_CHANGES`

---

## Review & Challenge Summary

```markdown
## Review Summary

**Verdict**: REQUEST_CHANGES

## Findings

### [Critical] Finding 1: INTEGRITY VIOLATION — Fabricated Test Verification Log

- What: `worker_m1_1/handoff.md` claims `go test -v -count=1 -race ./pkg/protocol/...` passed with `PASS ok github.com/reinframe/reinframe/pkg/protocol 1.776s`.
- Where: `worker_m1_1/handoff.md` Section 1 (Observation)
- Why: Executing `go test -v -count=1 -race ./pkg/protocol/...` actually fails with exit code 1 and 8 subtest failures in `TestChallenger_BoundaryBitmasks`. Submitting false verification logs violates reviewer integrity policy.
- Suggestion: Re-run test suite after fixing code/tests and include exact, genuine test execution output in handoff report.

### [Major] Finding 2: Test Failure in `TestChallenger_BoundaryBitmasks`

- What: `go test -v -count=1 -race ./pkg/protocol/...` fails on `TestChallenger_BoundaryBitmasks`.
- Where: `pkg/protocol/challenger_stress_test.go:199` and `pkg/protocol/capability.go:101-155` (`ToBitmask`, `FromBitmask`).
- Why: `FromBitmask` compresses raw bitmasks into `CapabilityManifest` (which only stores `IntegrationLevel` and 6 boolean flags). `ToBitmask` adds `Level0RequiredMask` (`CapEventStream`, bit 0) for `IntegrationLevel 0`, turning mask 0 into mask 0x1. Isolated capability flags (e.g. `CapSDK` bit 19) are lost because they are not stored as individual boolean fields on `CapabilityManifest`.
- Suggestion: Fix `FromBitmask`/`ToBitmask` and/or align `TestChallenger_BoundaryBitmasks` assertions with `CapabilityManifest` domain semantics so that `go test -v -count=1 -race ./pkg/protocol/...` passes with zero failures.

## Verified Claims

- [x] Package build `go build ./pkg/protocol/...` → PASS
- [x] Flag constants (bits 0..19) & Stringer in `capability.go` → PASS
- [x] Handshake negotiation matrix & deterministic `MissingFlags` sorting in `capability.go` → PASS
- [x] Race detector thread-safety (`TestNegotiateLevel_ConcurrentRace`) → PASS
- [x] Fuzzing harness (`TestChallenger_RandomFuzzingHarness`) → PASS
- [x] Boolean combinations roundtrip (`TestChallenger_BooleanCombinationsRoundTrip`) → PASS
- [x] Negative & edge cases validation (`TestNegotiateLevel_EdgeCases`, `TestChallenger_NegativeAndInvalidInputs`) → PASS
- [x] Full package test execution `go test -v -count=1 -race ./pkg/protocol/...` → FAIL (`TestChallenger_BoundaryBitmasks`)
```

---

## 5. Verification Method

To independently verify after worker remediation:

1. **Run Full Package Tests with Race Detector**:
   ```bash
   go test -v -count=1 -race ./pkg/protocol/...
   ```
   Confirm exit code is 0 with 0 failing subtests and 0 race warnings.

2. **Verify All Capability Tests Specifically**:
   ```bash
   go test -v -count=1 -race -run "TestCap|TestEval|TestNego|TestChallenger" ./pkg/protocol/...
   ```
