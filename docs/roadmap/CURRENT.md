# Reinframe current executable roadmap

**Status:** current (2026-08-04)  
**Wins on conflict:** README (public status) > this file (executable queue) > `docs/specs/*` (normative model) > Epic #80 (tracker) > historical docs.

## Implemented (narrow DoD — do not reopen for same scope)

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| M2.0 control loop | #69 #82 #70 #71 / #88–#89 | Fake-agent detect→defer→deliver→ACK |
| Task model + intake | #83–#84 / #90–#91 | Fixtures; not live host install |
| Verification churn + effort slice | #85–#86 / #92–#93 | Library only |
| Codex observation scaffold | #95 / #101–#102 | Offline + near-live JSONL tail; not process attach |
| Claude bridge API | #96 / #102 | Experimental API/CLI; no installer in that DoD |
| FileActuator | #97 / #102 | Write ≠ agent receipt |
| Review-session detectors | #98 / #101–#102 | Library + thin policy; uncalibrated |
| Optional LLM reviewer | PR #103 | Uncertain path only; high-confidence no LLM |
| Governance / source of truth | **#109 / PR #110** | CURRENT roadmap + archive markers only |
| Action Alignment design | **#104 / PR #111** | Normative Stage 0/1/2 design ([`docs/specs/action_alignment_classifier.md`](../specs/action_alignment_classifier.md)); **not** shadow runtime |
| Claude project-local install | **#106 / PR #112** | Installer + unit tests; **no pinned live Claude smoke** |
| Codex product observe surface | **#107 / PR #113** | Discovery/cursor/caps/codexctl; **observe-only** Level 0 |

## Active backlog (open only)

| Issue | Pri | Depends | Notes |
|-------|-----|---------|-------|
| #105 | P1 | #104 design **done** | Shadow-mode classifier **implementation** (ready) |
| #108 | P0 | **#106 live injection evidence** | Real advice consume / SafeBoundary / ACK — still blocked without pinned live smoke |
| #99 | P1 | — | Managed-worktree checkpoint/rollback runtime |
| #100 | P2 | #105 preferred | M3 synthetic/FP before hard-gate promotion |
| #80 | epic | — | Keep OPEN residual tracker |

## Execution order (remaining)

```text
#105 classifier shadow mode
  → #100 benchmarks / threshold recommendation
       → future explicit hard-gate promotion issue

#108 advice consumer / SafeBoundary (after live Claude inject evidence or alternate surface)

#99 managed-worktree rollback (parallel after ownership review)
```

## Explicit non-claims

- No calibrated hard-gate before #100 + separate promotion decision  
- No silent global Claude/Codex install  
- No dual-host production supervision claim  
- FileActuator write ≠ agent receipt  
- Codex JSONL tail / codexctl ≠ bidirectional control  
- #106 installer ≠ proven live control-loop without pinned smoke  
- OS SIGSTOP ≠ CapPause  
- Git rollback runtime not shipped until #99  

## Historical sources (not executable backlog)

- `docs/research/2026-08-03-m21-open-issues-order.md` — completed M2.1 pass record  
- Closed Epic #1 — foundation archive  
- Old DAG snapshots — historical phase map; prefer this file for queue  
