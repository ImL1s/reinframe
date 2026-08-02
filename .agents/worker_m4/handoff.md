# Handoff Report — M4 (Capability Test Suite Rewrite)

## 1. Observation

Direct code inspection of `pkg/protocol/` test suite showed:
- `pkg/protocol/capability_test.go` and `pkg/protocol/challenger2_stress_test.go`: Contained legacy test assertions assuming `ToBitmask()` auto-granted capability bitmasks based on `IntegrationLevel` without explicit boolean fields set.
- Level contract expectations in `capability_test.go`, `challenger2_stress_test.go`, and `capability_stress_test.go` still expected Level 1 to include `CapPause`, `CapCancel`, and `CapResume`, which contradicted the M2 realigned contracts (where `CapPause`, `CapCancel`, `CapResume` belong to Level 2 Guarded).
- `challenger_stress_test.go`: Contained hardcoded mock payloads (`max_depth: 2` and missing 14 capability boolean properties) that failed `ValidateEvent` following M2 JSON schema constraints.

Verification commands executed:
- `go test -v -race ./pkg/protocol/...` -> **PASS** (100% of protocol tests pass cleanly, exit code 0).
- `go test -race -count=5 ./pkg/protocol/...` -> **PASS** (5 consecutive runs pass cleanly under race detector, exit code 0).

## 2. Logic Chain

1. **Auto-grant Removal & Explicit Boolean Verification**: Rewrote `capability_test.go` to test that `CapabilityManifest` with `IntegrationLevel: 3` and no boolean fields returns bitmask `0x0` and achievable level `-1`, confirming zero auto-granting behavior.
2. **Level Contract Verification**:
   - Level 1 (Advisory) contract verified: `SupportsEventStream` + `SupportsToolInspection` achieves Level 1 without `SupportsPause`, `SupportsCancel`, `SupportsResume`.
   - Level 2 (Guarded) contract verified: requires process control hooks (`SupportsPause`, `SupportsCancel`, `SupportsResume`) plus `SupportsDiffInspection`, `SupportsCheckpoint`, `SupportsRollback`. Missing `SupportsPause` degrades candidate to Level 1.
3. **Lossless JSON Round-trip**: Added `TestCapability_JSONRoundTrip_Lossless_Explicit` testing 100% preservation across `json.Marshal` and `json.Unmarshal` for all 20 boolean capability flags (`SupportsEventStream` through `SupportsSDK`) and alternating boolean patterns.
4. **Stress & Degradation Alignment**: Updated `challenger2_stress_test.go`, `capability_stress_test.go`, and `challenger_stress_test.go` mock definitions to use `FromBitmask(...)` and schema-compliant payloads, ensuring missing flag sorting, bit flip detection, and zero struct handling are tested accurately.

## 3. Caveats

- None. All protocol tests pass 100% under `-race` and 5-run concurrency testing.

## 4. Conclusion

Milestone M4 tasks (1-7) are fully implemented and verified:
- Legacy auto-granting assertions removed.
- Bitmask construction verified strictly from 20 explicit boolean flags.
- Lossless JSON round-trip verified for all 20 capability flags.
- Level 1 (Advisory) vs Level 2 (Guarded) contracts verified according to ADR specification.
- 100% clean test execution for `go test -v -race ./pkg/protocol/...`.

## 5. Verification Method

To independently verify M4 work:
1. Run protocol package test suite with race detector:
   `go test -v -race ./pkg/protocol/...`
2. Run multi-iteration race stress test:
   `go test -race -count=5 ./pkg/protocol/...`
