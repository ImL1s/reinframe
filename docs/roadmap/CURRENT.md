# Reinframe current executable roadmap

**Status:** current (2026-08-04) — challenge/provider/cache backlog sync (#142)  
**Wins on conflict:** README (public status) > this file (executable queue) > `docs/specs/*` (normative model) > Epic #80 (tracker) > historical docs.

Issue #142 tracks this docs synchronization and is not a product work item in the post-merge queue below.

## Implemented (narrow DoD — do not reopen for the same scope)

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| M2.0 control loop | #69 #82 #70 #71 / #88–#89 | Fake-agent detect→defer→deliver→ACK |
| Task model + intake | #83–#84 / #90–#91 | Fixtures; not live host install |
| Verification churn + effort slice | #85–#86 / #92–#93 | Library only |
| Codex observation scaffold | #95 / #101–#102 | Offline + near-live JSONL tail; not process attach |
| Claude bridge API | #96 / #102 | Experimental API/CLI |
| FileActuator | #97 / #102 | Write ≠ agent receipt |
| Review-session detectors | #98 / #101–#102 | Library + thin policy; provisional |
| Optional LLM reviewer | PR #103 | Uncertain path only; not the classifier provider |
| Governance / source of truth | #109/#121 / PR #110/#122 | CURRENT + README honesty before this pass |
| Action Alignment design | #104 / PR #111 | Concept Stage 0/1/2 only |
| Classifier wire contract | #119 / PR #126 | Schemas, ADR 005, FakeProvider |
| Claude project-local install | #106 / PR #112 | Installer unit; **no live smoke** |
| Claude settings harden | #117 / PR #124 | Exact ownership; atomic write |
| ProposedAction projection | #115 / PR #123 | ToolName ≠ Command |
| PreTool response semantics | #116 / PR #127 | No `continue:false` for ordinary tool deny |
| Codex product observe + identity | #107/#118 / PR #113–#125 | Observe-only L0; collision-safe IDs |
| Shadow classifier | #105 / PR #128 | `Enforced=false` always |
| M3 synthetic/FP benchmark foundation | #100 / PR #129 | **MORE-DATA**; no hard-gate |
| Managed worktree rollback | #99 / PR #130 | Clean-only; not primary checkout |
| Post-merge hygiene | PR #133 | Evaluation denominator, workspace fail-closed, docs |

## Active backlog (product/research issues after this governance change)

### Ready — no open code dependency

| Issue | Pri | Scope | Boundary |
|-------|-----|-------|----------|
| **#131** | P1 | Appealable productivity BLOCK, justification, one-shot semantic retry | Host-neutral core; no live Claude claim |
| **#132** | P1 | Real classifier provider runtime, strict parser, normalized usage, cache-neutral generic adapter | No native provider/cache claim |

### Blocked by environment

| Issue | Pri | Blocker | Notes |
|-------|-----|---------|-------|
| **#120** | P0 | Interactive/operator Claude Code session | Pinned project-local ALLOW/BLOCK/context smoke; `BLOCKED_BY_ENVIRONMENT` |

Closed #115/#116/#117 are implementation prerequisites, not current blockers. Do not mark #120 ready until the live behavioral evidence exists.

### Blocked by open code/evidence dependency

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#108** | P0 | **#120** | Real advice consumer / SafeBoundary / honest ACK; does not own challenge state |
| **#139** | P1 | **#131 + #120** | Claude challenge context, structured appeal, bound one-shot retry |
| **#134** | P1 | **#132** | Native OpenAI Responses + explicit prompt-cache controls |
| **#135** | P1 | **#132** | Native Anthropic Messages + `cache_control` profiles |
| **#136** | P1 | **#132** | Native Gemini `generateContent` + implicit-cache telemetry/eligibility |
| **#137** | P1 | **#132** | Native xAI Responses + sticky prefix-cache routing |
| **#138** | P1 | **#132** | Exact `RawAssessment` cache + singleflight + cache observability |
| **#140** | P2 | **#131** for Lane A | Challenge appeal, bypass, recovery, and cost evaluation; later lanes need provider/#139 |
| **#141** | P2 | **#132** minimum | Provider/cache correctness and economics; full scope needs #138 + native provider lane |
| **#80** | epic | — | Residual tracker; keep open |

## Architectural invariants

### Classifier vs challenge

```text
Classifier / deterministic resolver: ALLOW | BLOCK
Appeal workflow metadata: none | APPEALABLE_CHALLENGE | HUMAN_REVIEW
```

`CHALLENGE` is not a third classifier decision. A justification is new evidence, never automatic permission. Hard security boundaries remain non-appealable or require human review.

### Provider cache vs Reinframe exact cache

```text
Stage 0 deterministic skip       → no model call
Reinframe exact assessment hit   → provider call skipped
Provider prompt/prefix cache     → provider call occurs; provider may reuse prefix work
No cache                         → normal provider path
```

The generic OpenAI-compatible adapter defaults to no vendor-specific cache capability. Native adapters own provider-specific fields. Cache never owns the final decision; Stage 2 reruns with current threshold, exceptions, policy, approval, and challenge state.

## Execution order

```text
Ready in parallel:
  #131 challenge core
  #132 provider runtime

Environment lane:
  #120 live Claude smoke
    → #108 generic advice consumer

After #131 + #120:
  #139 Claude challenge integration

After #132, parallel:
  #134 OpenAI native adapter/cache
  #135 Anthropic native adapter/cache
  #136 Gemini native adapter/cache
  #137 xAI native adapter/cache
  #138 exact assessment cache + singleflight

Evaluation:
  #140 Lane A after #131
       model-backed lane after #132 + native provider
       Claude lane after #139
  #141 after #132 and the implementation lanes selected for its matrix
```

## Explicit non-claims

- No calibrated classifier/detector hard-gate; #100 disposition is **MORE-DATA**  
- No live challenge-response product  
- No real production classifier provider beyond fakes until #132 and a provider adapter land  
- No cross-provider cache API equivalence claim  
- No measured token/cost savings before #141 evidence  
- No silent global Claude/Codex install  
- No dual-host production supervision claim  
- FileActuator write or context transport ≠ explicit agent ACK  
- Codex JSONL tail / codexctl ≠ bidirectional control  
- #106 installer ≠ #120 live control-loop proof  
- OS SIGSTOP ≠ native CapPause  
- Managed worktree rollback ≠ primary checkout or external-side-effect rollback  

## Evaluation

Baseline offline synthetic benchmarks: [`docs/evaluation/m3_benchmarks.md`](../evaluation/m3_benchmarks.md). The report disposition is **MORE-DATA** and hard-gates remain disabled.

Follow-ups:

- #140 evaluates challenge appeal quality, semantic bypass resistance, recovery, and added cost.
- #141 evaluates provider prefix caching, Reinframe exact caching, singleflight, correctness invariance, and measured economics.

Both require a separate promotion issue for any enforcement or default-enable decision.

## Historical sources (not executable backlog)

- `docs/plans/2026-08-03-issue-queue.md` — superseded  
- Closed Epic #1 — foundation archive  
