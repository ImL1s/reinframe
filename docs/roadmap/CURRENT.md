# Reinframe current executable roadmap

**Status:** current (2026-08-04) — #121 governance sync PR  
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
| Action Alignment design | **#104 / PR #111** | Concept Stage 0/1/2 design only; **wire contract incomplete → #119** |
| Claude project-local install | **#106 / PR #112** | Installer + unit tests; **no pinned live smoke → #120** |
| Codex product observe surface | **#107 / PR #113** + **#114** | Discovery/cursor/caps/codexctl; observe-only L0; **identity/tail harden → #118** |

## Active backlog (open only)

### Ready (no open dependency)

| Issue | Pri | Notes |
|-------|-----|-------|
| **#115** | P0 | Typed `ProposedAction` projection (ToolName ≠ Command) |
| **#117** | P0 | Claude hook ownership / doctor / atomic settings harden |
| **#118** | P0 | Codex EventID + collision-safe durable tail |
| **#119** | P0 | Classifier closed schemas / provider ADR / fixtures; **research OK, merge only after #115** |
| **#121** | P1 | README / CURRENT / Epic / label sync (docs) |
| **#99** | P1 | Managed-worktree checkpoint/rollback runtime |

### Blocked

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#116** | P0 | #115 | PreTool ALLOW/BLOCK/defer response semantics |
| **#120** | P0 | #115 #116 #117 | Pinned live project-local ALLOW/BLOCK smoke |
| **#105** | P1 | #119 (+ #115) | Shadow classifier runtime — **not ready** until contract complete |
| **#108** | P0 | **#120** (not closed #106 alone) | Real advice consume / SafeBoundary / ACK |
| **#100** | P2 | #105 (full scope) | M3 synthetic/FP; detector-only split optional later |
| **#80** | epic | — | Residual tracker; keep OPEN |

## Execution order (remaining)

```text
Parallel ready (product):
  #115 ProposedAction
  #117 Claude settings harden
  #118 Codex identity/tail harden
  #119 Classifier contract (docs/schemas/ADR/fixtures)
  #121 Governance sync (docs)

Then:
  #116 PreTool response semantics  (after #115)
  #120 Live Claude smoke           (after #115 #116 #117)
  #105 Shadow classifier           (after #119 + #115)
  #108 Advice consumer             (after #120)
  #100 Benchmarks / thresholds     (after #105 for classifier layer)

Parallel recovery:
  #99 managed-worktree rollback
```

## Explicit non-claims

- No calibrated hard-gate before #100 + separate promotion decision  
- No silent global Claude/Codex install  
- No dual-host production supervision claim  
- FileActuator write ≠ agent receipt  
- Codex JSONL tail / codexctl ≠ bidirectional control  
- #106 installer ≠ proven live control-loop without #120 pinned smoke  
- #104 design doc ≠ complete classifier wire contract (#119)  
- OS SIGSTOP ≠ CapPause  
- Git rollback runtime not shipped until #99  
- PR #114 CI green ≠ product complete  

## Historical sources (not executable backlog)

- `docs/research/2026-08-03-m21-open-issues-order.md` — completed M2.1 pass record  
- `docs/plans/2026-08-03-issue-queue.md` — superseded snapshot  
- Closed Epic #1 — foundation archive  
- Old DAG snapshots — historical phase map; prefer this file for queue  
