# Reinframe current executable roadmap

**Status:** current (2026-08-04)  
**Wins on conflict:** README (public status) > this file (executable queue) > `docs/specs/*` (normative model) > Epic #80 (tracker) > historical docs.

## Implemented (narrow DoD — do not reopen for same scope)

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| M2.0 control loop | #69 #82 #70 #71 / #88–#89 | Fake-agent detect→defer→deliver→ACK |
| Task model + intake | #83–#84 / #90–#91 | Fixtures; not live host install |
| Verification churn + effort slice | #85–#86 / #92–#93 | Library only |
| Codex observation | #95 / #101–#102 | Offline + near-live JSONL tail; not process attach |
| Claude bridge | #96 / #102 | Experimental API/CLI; no installer |
| FileActuator | #97 / #102 | Write ≠ agent receipt |
| Review-session detectors | #98 / #101–#102 | Library + thin policy; uncalibrated |
| Optional LLM reviewer | PR #103 | Uncertain path only; high-confidence no LLM |

## Active backlog

| Issue | Pri | Depends | Notes |
|-------|-----|---------|-------|
| #109 | P1 | — | Governance / source-of-truth (this file) |
| #104 | P0 | — | Two-stage Action Alignment classifier design |
| #105 | P1 | **#104** | Shadow-mode implementation (blocked) |
| #106 | P0 | #96 | Claude project-local hook install + smoke |
| #107 | P0 | #95 | Codex discovery, durable tail, capability honesty |
| #108 | P0 | **#106** | Real advice consume / SafeBoundary / ACK (blocked) |
| #99 | P1 | — | Managed-worktree checkpoint/rollback runtime |
| #100 | P2 | #105 preferred | M3 synthetic/FP before hard-gate promotion |

## Execution order

```text
#109 → #104 → (#106 ∥ #107) → #108 (after #106)
         ↘ #105 → #100 → future promotion issue
#99 parallel after ownership review
```

## Explicit non-claims

- No calibrated hard-gate before #100 + separate promotion decision  
- No silent global Claude/Codex install  
- No dual-host production supervision claim  
- FileActuator write ≠ agent receipt  
- Codex JSONL tail ≠ bidirectional control  
- OS SIGSTOP ≠ CapPause  
- Git rollback runtime not shipped until #99  

## Historical sources (not executable backlog)

- `docs/research/2026-08-03-m21-open-issues-order.md` — completed M2.1 pass record  
- Closed Epic #1 — foundation archive  
- Old DAG snapshots — update under #109 or mark historical  
