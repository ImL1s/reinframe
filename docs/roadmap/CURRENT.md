# Reinframe current executable roadmap

**Status:** current (2026-08-06) — provider/cache complete; host-control lanes **#163/#165/#166** Ready  
**Executable status source:** this file plus live GitHub issue labels. Epic #80 remains open.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Generic OpenAI-compatible cache-neutral |
| Native OpenAI Responses | **#134 / PR #153** | `/v1/responses`; explicit profiles |
| Native Anthropic Messages | **#135 / PR #154** | `claude_api` only; explicit recommended |
| Native Gemini generateContent | **#136 / PR #155** | Implicit; explicit objects deferred |
| Native xAI Responses | **#137 / PR #156** | Classifier only — **not** Grok Build host control |
| Exact assessment cache | **#138 / PR #157** + **#169** + **#172** | Process-local; default disabled; session partition; cancel-safe + panic-safe singleflight |
| Challenge eval Lane A | **#140 / PR #159** | Deterministic fixtures/metrics; MORE-DATA |
| Challenge eval Lane B offline | **#140 / PR #160** | Fake native transports only |
| Provider/cache eval | **#141 / PR #161** + **#169** | Fake CI; disposition **MORE-DATA** |
| Offline detector/classifier foundation | **#100 / PR #129** | MORE-DATA; no hard-gate |
| Codex EventSource | **#95/#107/#118** | JSONL offline/tail **observe-only** (L0); hooks not shipped |
| Claude PreTool bridge | **#96/#106/#117** | Experimental API + project-local install; **not** live smoke |

## Implemented host foundations

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| Codex project-local hooks | **#163** | Foundation only; live proof **#164**; not Level 2 / not explicit ACK |

## Ready (repository-executable host foundations)

| Issue | Notes |
|-------|-------|
| **#165** | Grok Build native hooks foundation |
| **#166** | Grok Build ACP stdio bridge |

## Residual open work

### Environment / live evidence

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#164** | #163 + interactive Codex CLI | Live Codex hooks ALLOW/BLOCK/context proof |
| **#167** | #165 + #166 + authenticated Grok CLI | Live Grok hooks + ACP proof |
| **#120** | Interactive Claude session | Live Claude ALLOW/BLOCK/context smoke |
| **#139** | #120 | Live Claude challenge delivery |
| **#140 Claude host lane** | #139 | Manual evidence only |

### Product (after at least one proven live host lane)

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#108** | First of #164 \| #167 \| #120 | Real multi-host advice consumer + honest ACK layers |

### Evaluation

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#168** | Matched live host lanes | Cross-host tunneling/intervention quality |

### Epic

| Issue | Notes |
|-------|-------|
| **#80** | Open until residual environment/product close criteria are met |

## Explicit non-claims

- No Codex **control** until #163; no live Codex proof until #164
- No Grok Build host adapter until #165/#166; no live Grok proof until #167
- xAI Responses classifier ≠ Grok Build host integration
- No fail-closed Grok hook claim (timeout/crash/malformed is host fail-open)
- Hook gating is CapToolGate — not native CapPause / Level 2
- Context/transport/JSON-RPC success is **not** explicit agent ACK
- No live Claude smoke (#120), challenge delivery (#139), or dual-host production supervision
- No calibrated hard-gate; #100/#140/#141 remain **MORE-DATA** where stated
- No default exact-cache enablement; no universal provider savings %
- No claim that GPT/Codex tunnels more without #168 matched evidence

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- Adapter pins: `docs/classifier/*`, `docs/adapter/*`
