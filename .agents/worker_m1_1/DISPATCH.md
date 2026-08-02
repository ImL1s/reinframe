## 2026-08-02T13:41:11Z

<USER_REQUEST>
You are Worker 1 for Milestone 1 (Issue #7: Capability Manifest & Handshake Protocol).
Working Directory: /Users/iml1s/Documents/mine/reinframe/.agents/worker_m1_1

Mandatory Input Files (READ THESE FIRST):
- /Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md
- /Users/iml1s/Documents/mine/reinframe/PROJECT.md
- /Users/iml1s/Documents/mine/reinframe/.agents/sub_orch_m1_issue_7/SCOPE.md
- /Users/iml1s/Documents/mine/reinframe/.agents/explorer_m1_1/handoff.md
- /Users/iml1s/Documents/mine/reinframe/.agents/explorer_m1_2/handoff.md
- /Users/iml1s/Documents/mine/reinframe/.agents/explorer_m1_3/handoff.md

Your exclusive file write ownership:
- /Users/iml1s/Documents/mine/reinframe/pkg/protocol/capability.go
- /Users/iml1s/Documents/mine/reinframe/pkg/protocol/capability_test.go

DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Your mission:
1. Ensure git branch is `issue-7-capability-manifest-negotiation`.
2. Create `pkg/protocol/capability.go`:
   - Define `CapabilityFlag uint64` and 20 capability flag constants across 4 categories:
     - Category 1: CapEventStream (1<<0), CapToolInspection (1<<1), CapDiffInspection (1<<2), CapCostTracking (1<<3), CapHooks (1<<4)
     - Category 2: CapHeadless (1<<5), CapCLIControl (1<<6), CapPause (1<<7), CapCancel (1<<8), CapResume (1<<9)
     - Category 3: CapCheckpoint (1<<10), CapRollback (1<<11), CapMCP (1<<12), CapSubagents (1<<13), CapExtensions (1<<14)
     - Category 4: CapSwitchModel (1<<15), CapCustomProvider (1<<16), CapOpenAICompat (1<<17), CapLocalModels (1<<18), CapSDK (1<<19)
   - Define `HandshakeRequest` and `HandshakeResponse` structs matching specs in PROJECT.md.
   - Helper methods on `CapabilityManifest`:
     - `ToBitmask() uint64`: combines boolean fields (`SupportsPause`, `SupportsCancel`, `SupportsResume`, `SupportsCheckpoint`, `SupportsRollback`, `SupportsMCP`, `IntegrationLevel`, etc.) into a bitmask.
     - `FromBitmask(mask uint64) CapabilityManifest`: populates boolean fields from bitmask.
     - `HasCapability(flag CapabilityFlag) bool`: checks if specific flag is set.
   - String representation / FlagToString map for flag names in `MissingFlags`.
   - `EvaluateAchievableLevel(manifest *CapabilityManifest) int`:
     - Level 0 (Observe): CapEventStream
     - Level 1 (Advisory): Level 0 + CapToolInspection + CapPause + CapCancel + CapResume
     - Level 2 (Guarded): Level 1 + CapDiffInspection + CapCheckpoint + CapRollback
     - Level 3 (Full-control): Level 2 + CapHeadless + CapCLIControl + CapMCP + CapSubagents + CapSwitchModel
   - `NegotiateLevel(req *HandshakeRequest) (*HandshakeResponse, error)`:
     - Input validation (nil request error, empty SessionID error, invalid level 0-3 error).
     - Calculates achievable level.
     - If requested_level <= achievable: returns NegotiatedLevel = requested_level, IsDegraded = false.
     - If requested_level > achievable: degrades to achievable level, sets IsDegraded = true, DegradedFrom = requested_level, and lists missing flag names in deterministic order in MissingFlags.
3. Create `pkg/protocol/capability_test.go`:
   - Unit tests covering bitmask conversions, level calculation for Levels 0-3, negotiation matrix, automatic degradation details, nil/invalid request edge cases.
   - Multi-goroutine concurrent race test (`TestNegotiateLevel_ConcurrentRace`) running 100 parallel goroutines calling NegotiateLevel.
4. Execute verification:
   - Run `go test -v -race ./pkg/protocol/...`
   - Confirm all tests pass with 0 race warnings.
5. Write your handoff report to `/Users/iml1s/Documents/mine/reinframe/.agents/worker_m1_1/handoff.md`.
6. Send a message to caller with path to handoff.md and test results when complete.
</USER_REQUEST>
