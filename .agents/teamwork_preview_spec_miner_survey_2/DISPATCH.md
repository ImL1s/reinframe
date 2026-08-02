# Survey Dispatch — Spec Miner 2 (Issue #7)

## Objective
Extract and document the complete specification and requirements for Issue #7: Capability Manifest & Handshake Protocol.

## Inputs
- /Users/iml1s/Documents/mine/reinframe/ORIGINAL_REQUEST.md
- Existing codebase in /Users/iml1s/Documents/mine/reinframe/

## Output Requirements
Write a detailed handoff report to `/Users/iml1s/Documents/mine/reinframe/.agents/teamwork_preview_spec_miner_survey_2/handoff.md` detailing:
1. Exact struct definitions required for `CapabilityManifest` in `pkg/protocol/capability.go`.
2. The 20 capability flags (names, values/bitmasks, categories, default states).
3. Negotiation engine requirements: Level 0 (Observe), Level 1 (Advisory), Level 2 (Guarded), Level 3 (Full-control).
4. Automatic degradation rules & handshake protocol flow.
5. Unit test requirements in `pkg/protocol/capability_test.go`.
6. Target git branch: `issue-7-capability-manifest-negotiation`.
