# Reinframe current executable roadmap

**Status:** current (2026-08-04) — post #99/#100 merge hygiene  
**Wins on conflict:** README (public status) > this file (executable queue) > `docs/specs/*` (normative model) > Epic #80 (tracker) > historical docs.

## Implemented (narrow DoD — do not reopen for same scope)

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| M2.0 control loop | #69 #82 #70 #71 / #88–#89 | Fake-agent detect→defer→deliver→ACK |
| Task model + intake | #83–#84 / #90–#91 | Fixtures; not live host install |
| Verification churn + effort slice | #85–#86 / #92–#93 | Library only |
| Codex observation scaffold | #95 / #101–#102 | Offline + near-live JSONL tail; not process attach |
| Claude bridge API | #96 / #102 | Experimental API/CLI |
| FileActuator | #97 / #102 | Write ≠ agent receipt |
| Review-session detectors | #98 / #101–#102 | Library + thin policy; uncalibrated |
| Optional LLM reviewer | PR #103 | Uncertain path only; high-confidence no LLM |
| Governance / source of truth | **#109 / PR #110** + **#121 / PR #122** | CURRENT + README honesty |
| Action Alignment design | **#104 / PR #111** | Concept Stage 0/1/2 only |
| Classifier wire contract | **#119 / PR #126** | Schemas, ADR 005, FakeProvider |
| Claude project-local install | **#106 / PR #112** | Installer unit; **no live smoke** |
| Claude settings harden | **#117 / PR #124** | Exact ownership; atomic write |
| ProposedAction projection | **#115 / PR #123** | ToolName ≠ Command |
| PreTool response semantics | **#116 / PR #127** | No `continue:false` for tool deny |
| Codex product observe + identity | **#107/#118 / PR #113–#125** | Observe-only L0; collision-safe IDs |
| Shadow classifier | **#105 / PR #128** | `Enforced=false` always |
| M3 synthetic/FP benchmarks | **#100 / PR #129** | MORE-DATA; **no hard-gate** |
| Managed worktree rollback | **#99 / PR #130** | Clean-only; not primary checkout |

## Active backlog (open only)

### Blocked

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#120** | P0 | environment (interactive Claude) | Pinned live project-local ALLOW/BLOCK smoke — `BLOCKED_BY_ENVIRONMENT` |
| **#108** | P0 | **#120** | Real advice consume / SafeBoundary / ACK |
| **#80** | epic | — | Residual tracker; keep OPEN |

### Ready product work

*None.* All previously ready residual library issues (#115–#119, #121, #99, #100, #105) are closed with narrow DoD.

## Execution order (remaining)

```text
#120 live Claude smoke (operator / interactive environment)
  → #108 advice consumer / SafeBoundary

Epic #80 stays open until product-complete or explicit scope transfer.
Future hard-gate promotion only after #100 LIMITED-GO + separate issue
(#100 disposition is MORE-DATA — do not promote).
```

## Explicit non-claims

- No calibrated hard-gate (MORE-DATA on synthetic #100)  
- No silent global Claude/Codex install  
- No dual-host production supervision claim  
- FileActuator write ≠ agent receipt  
- Codex JSONL tail / codexctl ≠ bidirectional control  
- #106 installer ≠ proven live control-loop without #120  
- OS SIGSTOP ≠ CapPause  
- Managed worktree rollback ≠ primary checkout mutation  

## Evaluation

Offline synthetic benchmarks: [`docs/evaluation/m3_benchmarks.md`](../evaluation/m3_benchmarks.md). Hard-gates not enabled. Report disposition **MORE-DATA**.

## Historical sources (not executable backlog)

- `docs/plans/2026-08-03-issue-queue.md` — superseded  
- Closed Epic #1 — foundation archive  
