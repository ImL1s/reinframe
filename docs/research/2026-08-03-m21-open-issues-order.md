# M2.1 open issues — deep-research order & PR plan

> **Historical execution record (not current backlog).**  
> Executable queue: [`docs/roadmap/CURRENT.md`](../roadmap/CURRENT.md).  
> Active tracker: GitHub Epic #80.

**Date:** 2026-08-03  
**Scope:** Remaining open product issues after M2.0 (#82/#69/#70/#71) and #83 on `main` (pass completed).  
**Status:** Historical — M2.1 #84–#86 and later M2.2 #95–#98 closed with narrow DoD.

## Baseline on main

| Landed | Role |
|--------|------|
| Protocol + TaskSubmitted/Contract/Ledger + builder (#83) | Intake types + `BuildContractFromSubmitted` |
| Detector repeated-failure (#82) | direction_fixation |
| Policy fast/slow (#69) | ZOOM_OUT; optional Contract/Ledger enrich |
| Supervisor + vertical slice (#70/#71) | detect→defer→deliver→ACK (fakes) |

## Open set

| Issue | Priority | Product question |
|-------|----------|------------------|
| **#84** | P0 | Host event → TaskSubmitted (+ contract draft); Claude mapping is **adapter-only** |
| **#85** | P1 | `verification_churn` multi-part fingerprint detector |
| **#86** | P1 | Effort-calibration vertical slice (over-SOP deny at before_tool) |
| **#80** | Epic | M2 governance — **not** one implementation PR |

## Recommended order (strict)

```text
#84  TaskSubmitted intake + Claude mapping (fixtures)
  →  #85  VerificationChurnDetector
  →  #86  Effort-calibration vertical-slice test (uses #84+#85+policy/hook)
  →  #80  Comment update only (leave open unless all epic DoD honestly met)
```

**Why this order**

1. **#84 first** — #86 scenario starts with TaskSubmitted/contract; core must stay host-agnostic; Claude names only in adapter tests/docs.  
2. **#85 before #86** — over-SOP deny needs a real churn signal, not hand-inserted policy rows alone.  
3. **#86 last** — composes intake + detector + before_tool deny; must not re-implement #71 repeated-error path.  
4. **#80** — epic already satisfied M2.0 children; M2.1 children tracked separately; do **not** close epic solely when research or one PR lands.

## Must-not-do

- Claim production Claude Code / Codex **actuators** (hooks as live product) beyond fixture mapping for #84.  
- Claim full multi-deviation E2E or calibrated hard-gate science.  
- Close #80 without honest residual list.  
- Put Claude hook type names inside `pkg/protocol`.  
- Satisfy #86 with Store-only hand-built interventions without detector+policy.

## One atomic PR per issue

| Issue | PR title pattern | Primary package(s) |
|-------|------------------|--------------------|
| #84 | `feat(adapter): TaskSubmitted intake + Claude mapping fixtures (#84)` | `pkg/adapter` + short doc |
| #85 | `feat(detector): verification_churn multi-part fingerprint (#85)` | `pkg/detector` |
| #86 | `test(supervisor): effort-calibration over-SOP vertical slice (#86)` | `pkg/supervisor` (+ thin policy/hook glue if needed) |

Process per PR (project standard): branch → tests `-race` → open PR → Lint+Ubuntu+macOS+Windows green → AI review comment → merge → close issue **implemented**.

## Per-issue DoD (summary)

### #84
- Mapper: host payload (generic map / Claude-shaped fixture) → `TaskSubmitted` → optional `BuildContractFromSubmitted`.  
- Unit tests for Claude UserPromptSubmit-shaped JSON **without** protocol importing Claude names.  
- Doc table Host→Core (Claude, Codex, API, CLI).

### #85
- Fingerprint = normalize(command)+scope+workspaceRev+contractRev+purpose.  
- Fire only when prior equivalent **succeeded**, workspace+contract unchanged, not flaky-investigation, not policy-required re-run, not high-risk independent re-validation.  
- Unit tests for fire + all counterexamples.

### #86
- Scenario: typo/simple contract → edit/diff done → full suite attempt → **before_tool DENY** (`redundant_validation` / disproportionate).  
- Counterexamples allow re-test.  
- Uses real intake helper + churn detector + hook gate (not Store-only theater).

## Epic #80 disposition after this pass

Leave **OPEN** with comment: M2.0 implemented; M2.1 #84–#86 status as merged; concrete harness adapters still non-goal for epic DoD.  
Close #80 only if maintainers redefine epic scope to M2.0-only (out of this pass).

---

## M2.2 / residual atomic issues (opened 2026-08-03)

After library slices closed, product backlog (do not reuse #84–#86 DoD):

| Issue | Priority | Topic | Disposition (2026-08-03) |
|-------|----------|--------|--------------------------|
| **#95** | P0 | Codex EventSource | **CLOSED** — offline `CodexRolloutSource` + near-live `CodexTailSource` (PR #101/#102); not process attach |
| **#96** | P0 | Claude PreTool/event bridge | **CLOSED** — experimental API + `cmd/claudebridge` (PR #102); no global settings install claim |
| **#97** | P0 | Real InterventionActuator | **CLOSED** — `FileActuator` JSONL channel + pending ACK (PR #102) |
| **#98** | P1 | Tool-budget + hypothesis-loop | **CLOSED** library DoD — detectors + thin `EvaluateSlow` (PR #101/#102) |
| **#99** | P1 | Git checkpoint/rollback runtime | **OPEN** |
| **#100** | P2 | M3 synthetic + FP benchmarks | **OPEN** |

**Historical order (execution):** `#95 scaffold → #98 library → #96 ∥ #97 + Codex tail` via PR #101 then #102.

**Still open under epic #80:** #99, #100; dual-host production install and calibrated hard-gates remain non-claims. See README Project Status and `docs/dev/STREET_WIRE.md`.

**Experience note:** long Codex *review* sessions often lack identical compile-error fingerprints (#82); #95+#98 library paths address observation/detection — not automatic live intervention without host consumers.
