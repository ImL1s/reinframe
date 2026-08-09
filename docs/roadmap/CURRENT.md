# Reinframe current executable roadmap

**Status:** current (2026-08-09) — Grok **historical live evidence** (#167) under **#199** requalification (public **MORE_DATA**); **#108** foundation only (**#200** open); **#168 MORE-DATA**; governance **#201**  
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
| Grok Build ACP stdio | **#166** + residual **#180**; hardening **#191** | Foundation on main; #191 fixes delegated-auth, canonical levels, official headless argv, Windows Job Object tree; live **#167** |
| Cross-host eval framework | **#168** | Fake + historical Grok pin; disposition **MORE-DATA**; no ranking; open until matched Codex/Claude live |

## Residual open work


### Evidence / delivery residuals (active)

| Issue | Notes |
|-------|-------|
| **#201** | Public claim downgrade (docs) |
| **#199** | Requalify #167 closed-schema GO gates; public MORE_DATA until closed |
| **#200** | Source-bound ACK + durable delivery + live #108 E2E composition |

Dependency order: **#201 → #199 → #200 → re-sync README/Epic/#168**.

### Environment / live evidence (still open)

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#164** | Interactive Codex + project trust | Live hooks smoke |
| **#120** | Interactive Claude | Live Claude smoke |
| **#139** | #120 | Claude challenge delivery |
| **#140 Claude host lane** | #139 | Manual evidence |

### Recently merged (Grok campaign) — claims bounded by residual issues

| Issue | Notes |
|-------|-------|
| **#167** / PR #193 | Historical live harness + v1 evidence on main; **unconditional GO withdrawn** pending **#199** |
| **#108** / PR #194 | Foundation consumer/ledger on main; **live product complete withdrawn** pending **#200** |
| **#191** | Grok ACP official-contract / lifecycle hardening on main |
| **#201** | Immediate public-status downgrade (this wording) |

### Evaluation

| Issue | Blocker | Notes |
|-------|---------|-------|
| **#168** live match | #164/#120 matched data | Grok live pin present; disposition **MORE-DATA**; no ranking |

### Epic

| Issue | Notes |
|-------|-------|
| **#80** | Open until residual environment/product close criteria are met |

## Explicit non-claims

- Historical Grok live run present (#167 v1); public disposition MORE_DATA until #199; no live Codex/Claude until #164/#120
- No fail-closed Grok hook security (host fail-open)
- CapToolGate ≠ CapPause / Level 2
- JSON-RPC / file append / hook exit ≠ explicit agent ACK
- xAI classifier ≠ Grok Build host
- No dual-host production supervision; no hard-gate; no default cache enablement
- No cross-host tunneling ranking without matched multi-host live evidence
- No Level 2 / CapPause from hooks alone; transport ≠ explicit ACK
- No source-correlated session_visible or explicit ACK without #199/#200
- No exactly-once delivery claim

## Evaluation artifacts

- `docs/evaluation/challenge_lane_a.md`
- `docs/evaluation/challenge_lane_b_model.md`
- `docs/evaluation/provider_cache_141.md`
- `docs/evaluation/cross_host_168.md`
- Adapter pins: `docs/classifier/*`, `docs/adapter/*`
