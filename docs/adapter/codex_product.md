# OpenAI Codex Product & Runtime Capabilities (#107, #163, Epic #182)

## Capability Manifests & Integration Surfaces

Reinframe models OpenAI Codex support across multiple independent integration lanes:

| Surface / Lane | Status | Level / Mask | Notes |
|---|---|---|---|
| **EventSource JSONL offline/tail (#95/#107/#118)** | **Shipped** | Level 0 Observe (`CapEventStream`) | Collision-safe source identity; cursor persistence |
| **Project-local hooks control (#163)** | **Shipped Foundation** | Level 1 Advisory (`CapHooks`, `CapToolGate`, `CapContextInjection`) | `.codex/hooks.json` PreTool/Permission mapping; live proof #164 |
| **Delegated ChatGPT Auth Boundary (#183)** | **Open / Ready** | Auth Delegation | Codex CLI / App Server owns OAuth keyring; Reinframe never extracts tokens |
| **Codex App Server Runtime (#184)** | **Open (blocked by #183)** | Level 2 Guarded / Session Control | JSON-RPC stdio protocol; programmatic turn & session lifecycle |
| **Dynamic Model Catalog (#185)** | **Open (blocked by #184)** | Model Discovery | Account/scope-aware model discovery; no static OAuth inventory |
| **Dual-Lane Subscription/API Routing (#186)** | **Open (blocked by #185)** | Transport Routing | Explicit separation of `subscription` vs `api` lanes |
| **GPT-5.3-Codex-Spark Qualification (#187)** | **Open (blocked by #186 + env)** | Model Qualification | ChatGPT Pro research preview; un-qualified until live evidence recorded |
| **Spark Opt-in API Profile (#188)** | **Open (blocked by API entitlement)** | API Lane | Separate opt-in API lane for design partners; not implied by Pro |
| **Auth/Catalog/Model-Churn Suite (#189)** | **Open (blocked by #186)** | Test Suite | Synthetic test harness for catalog churn and token expiry |

`DefaultCodexCapabilityManifest()` negotiates **Level 0 Observe**.  
`CodexHooksFoundationManifest()` advertises **Level 1 Advisory** (when project hooks are trusted).

---

## The Three Orthogonal Operational Axes

1. **Host Integration Capability:** `observe` (L0) | `hooks/tool gate` (L1) | `App Server session control` (L2/L3) | `ACK levels` (transport vs session vs cognitive ACK).
2. **Credential / Transport Class:** `ChatGPT subscription via Codex-owned auth` | `Codex API-key mode` | `direct OpenAI API key` (ClassifierProvider).
3. **Model Support State:** `discovered` | `selectable` | `capability-pinned` | `live-qualified`.

See [`docs/research/level_axes_mapping.md`](../research/level_axes_mapping.md) and [`docs/specs/codex_oauth_spark_boundary.md`](../specs/codex_oauth_spark_boundary.md) for normative contracts.

---

## Operator Surface

```bash
# 1. Observe-only JSONL tailing (cmd/codexctl)
go run ./cmd/codexctl list -root "$HOME/.codex/sessions"
go run ./cmd/codexctl select -root "$HOME/.codex/sessions" -path /abs/rollout.jsonl
go run ./cmd/codexctl caps
go run ./cmd/codexctl doctor -path /abs/rollout.jsonl -cursor /tmp/cursor.json

# 2. Project-local hooks control (cmd/codexhooks #163)
go run ./cmd/codexhooks plan -project .
go run ./cmd/codexhooks install -project .
go run ./cmd/codexhooks doctor -project .
```

- Multiple rollouts → **must** pass explicit `-path` (no recency auto-pick).  
- Cursor JSON persists byte offset; truncation resets offset and bumps generation.  
- Never writes Codex session/transcript files directly.

---

## Core Invariants & Governance Rules

1. **Zero Token Extraction:** Codex CLI / App Server owns authentication tokens. Reinframe never extracts or proxies ChatGPT OAuth tokens.
2. **Zero Silent Substitution:** If a specified model (e.g. `gpt-5.3-codex-spark`) is unavailable in the authenticated account scope, Reinframe fails closed with a deterministic error (`MODEL_UNAVAILABLE`).
3. **Subscription Access != API Access:** ChatGPT Pro subscription access to GPT-5.3-Codex-Spark does not imply OpenAI API access. API lanes (#188) require explicit API entitlement.
4. **ClassifierProvider Decoupling:** Reinframe's internal `ClassifierProvider` (`pkg/classifier/`) utilizes direct API keys (`/v1/responses` in #134) and is never routed through subscription OAuth transports.

---

## Authoritative References & Retrieval Dates

- [Introducing GPT-5.3-Codex-Spark](https://openai.com/index/introducing-gpt-5-3-codex-spark/) (Published: 2026-02-12)
- Codex Auth: `https://developers.openai.com/codex/auth` (Retrieved: 2026-08-15)
- Codex Models: `https://developers.openai.com/codex/models` (Retrieved: 2026-08-15)
- Codex App Server: `https://developers.openai.com/codex/app-server` (Retrieved: 2026-08-15)
- Codex SDK: `https://developers.openai.com/codex/sdk` (Retrieved: 2026-08-15)
