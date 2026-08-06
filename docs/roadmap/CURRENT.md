# Reinframe current executable roadmap

**Status:** current (2026-08-06) — host-control foundations **#163/#165/#166** (incl. residual **#180**) + offline **#168** framework on main  
**Executable status source:** this file plus live GitHub issue labels. Epic #80 remains open.

## Implemented — narrow DoD, do not reopen for the same scope

| Track | Issues/PRs | Honesty boundary |
|-------|------------|------------------|
| Appealable `BLOCK` core | **#131 / PR #148** | Host-neutral; no live Claude delivery |
| Classifier provider foundation | **#132 / PR #150** | Generic OpenAI-compatible cache-neutral |
| Native OpenAI/Anthropic/Gemini/xAI | **#134–#137** | Classifier adapters only; xAI ≠ Grok host |
| Exact assessment cache | **#138** + fixes | Process-local; default off; panic-safe singleflight |
| Challenge/cache offline eval | **#140** A/B, **#141** | MORE-DATA |
| Codex EventSource | **#95/#107/#118** | JSONL observe-only L0 |
| Claude PreTool bridge | **#96/#106/#117** | Experimental; not live smoke |
| Host roadmap sync | **#170** | Public status for Codex/Grok lanes |
| Codex project-local hooks | **#163** | Foundation; live **#164** |
| Grok Build native hooks | **#165** | Foundation; host fail-open; live **#167** |
| Grok Build ACP stdio | **#166** + residual **#180** | Negotiated auth/load/cancel; process-group cleanup; headless observe-only; transport/session_visible ACK; live **#167** |
| Cross-host eval framework | **#168** offline | Fake hosts only; disposition **MORE-DATA**; issue stays open for live match |

## Residual open work

### Environment / live evidence

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#164** | Interactive Codex + project trust | Live hooks smoke |
| **#167** | Authenticated Grok CLI + trust | Live hooks + ACP |
| **#120** | Interactive Claude | Live Claude smoke |
| **#139** | #120 | Claude challenge delivery |
| **#140 Claude host lane** | #139 | Manual evidence |

### Product

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#108** | First of #164 \| #167 \| #120 | Real multi-host advice + honest ACK |

### Evaluation

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#168** live match | #164/#167/#120 matched data | Framework exists; disposition MORE-DATA |

### Epic

| Issue | Notes |
|-------|-------|
| **#80** | Open until residual environment/product close criteria are met |

## Explicit non-claims

- No live Codex/Grok/Claude host proof until #164/#167/#120
- No fail-closed Grok hook security (host fail-open)
- CapToolGate ≠ CapPause / Level 2
- JSON-RPC / file append / hook exit ≠ explicit agent ACK
- xAI classifier ≠ Grok Build host
- No dual-host production supervision; no hard-gate; no default cache enablement
- No cross-host tunneling ranking without matched live evidence

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- `docs/evaluation/cross_host_168.md`
- Adapter pins: `docs/classifier/*`, `docs/adapter/*`
