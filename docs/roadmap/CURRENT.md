# Reinframe current executable roadmap

**Status:** current (2026-08-04) — open-set sync (#142) after residual library drain + PR #133
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
| Post-merge P1 hygiene | **PR #133** | Eval rate denominators; workspace fail-closed |

## Active backlog (open only — must match `gh issue list --state open`)

Full open set: **#80, #108, #120, #131, #132, #134, #135, #136, #137, #138, #139, #140, #141, #142**.

### Ready product / docs work

| Issue | Pri | Notes |
|-------|-----|-------|
| **#131** | P1 | Appealable BLOCK challenges + one-shot semantic retries (host-neutral). Public classifier enum stays `ALLOW \| BLOCK`. |
| **#132** | P1 | Classifier provider runtime, usage telemetry, capability-safe generic adapter |
| **#142** | P1 | Governance: sync README/CURRENT/Epic open set (this docs track) |

### Blocked by environment

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#120** | P0 | interactive Claude only | Pinned live project-local ALLOW/BLOCK smoke — `BLOCKED_BY_ENVIRONMENT` (impl prereqs #115/#116/#117 closed) |
| **#108** | P0 | **#120** | Real advice consume / SafeBoundary / ACK; challenge ownership stays #131/#139 |

### Blocked by code dependencies

| Issue | Pri | Blocked by | Notes |
|-------|-----|------------|-------|
| **#134** | P1 | **#132** | OpenAI native Responses classifier adapter |
| **#135** | P1 | **#132** | Anthropic native Messages adapter |
| **#136** | P1 | **#132** | Gemini native generateContent adapter |
| **#137** | P1 | **#132** | xAI native Responses adapter |
| **#138** | P1 | **#132** | Exact-assessment memoization / singleflight / cache observability |
| **#139** | P1 | **#131** and **#120** | Claude appeal delivery + structured one-shot retries |
| **#140** | P2 | **#131** (Lane A); **#139** / **#132**+provider for later lanes | Challenge appeal / bypass / recovery benchmarks (no hard-gate) |
| **#141** | P2 | **#132**; exact-cache lane **#138**; provider-cache lanes **#134–#137** | Provider/exact-cache economics benchmarks (no hard-gate) |
| **#80** | epic | — | Residual tracker; keep OPEN |

## Execution order (remaining)

```text
Ready, parallel:
  #131 challenge core
  #132 provider runtime
  #142 docs/open-set governance (docs-only)

Environment lane:
  #120 pinned live Claude ALLOW/BLOCK/context smoke
    → #108 generic advice consumer

After #131 + #120:
  #139 Claude challenge delivery / one-shot retry

After #132, parallel:
  #134 OpenAI native cache
  #135 Anthropic native cache
  #136 Gemini native cache
  #137 xAI native cache
  #138 exact cache + singleflight

Evaluation (follow-ups to closed #100 MORE-DATA — do not reopen #100):
  #140 Lane A after #131; Claude lane after #139; model lane after #132 + provider
  #141 after #132 plus #138 and at least one native provider lane

Epic #80 stays open until product-complete or explicit scope transfer.
Future hard-gate promotion only after LIMITED-GO evidence + separate issue
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
- #119 FakeProvider / #105 shadow ≠ production provider runtime (#132 open)
- #131 appeal design ≠ live Claude retry proof (#139 needs #131 and #120)
- #140/#141 evaluation issues ≠ implementation of challenge/providers/cache
- No native provider, token-savings, or cache-hit claims until measured

## Evaluation

Offline synthetic benchmarks: [`docs/evaluation/m3_benchmarks.md`](../evaluation/m3_benchmarks.md). Hard-gates not enabled. Report disposition **MORE-DATA**. Follow-up datasets: #140 (challenges), #141 (provider/cache).

## Historical sources (not executable backlog)

- `docs/plans/2026-08-03-issue-queue.md` — superseded
- Closed Epic #1 — foundation archive
