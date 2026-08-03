# Optional LLM / Reviewer advice

## Two-tier rule (normative)

From `docs/specs/adaptive_task_supervisor.md` §7:

| Path | When | Reviewer / LLM |
|------|------|----------------|
| **High-confidence deterministic** | Same error N times, verification_churn, tool budget, … | **Never called** — fixed `DefaultZoomOutAdvice` (or contract-enriched template) |
| **Uncertain / reviewer-worthy** | Ambiguous tunnel vs healthy work, high-risk incomplete evidence, `SlowInput.Uncertain=true` | **Optional** `ReviewerProvider.Generate` → may set `SuggestedAdvice` |

Linear “always Reviewer” is **forbidden** (cost control).

## Config

```yaml
schema_version: 1
session:
  local_only_reviewer: true   # ADR 003 default — blocks remote modes
reviewer:
  mode: local                 # local | openai_compatible | cloud
  model: "your-model-id"
  base_url: "http://127.0.0.1:11434/v1"   # required for local (loopback only)
  api_key_ref: "${REINFRAME_REVIEWER_API_KEY}"  # optional env placeholder
```

- **local** + `local_only_reviewer=true`: BaseURL must be loopback OpenAI-compatible.
- **openai_compatible / cloud**: only if `local_only_reviewer=false` (explicit opt-in).

Construction: `reviewer.NewProviderFromConfig(cfg)` → `policy.NewEngine(policy.EngineConfig{Reviewer: p, ReviewerModel: cfg.Reviewer.Model})`.

## OpenAI-compatible wire format

POST `{base_url}/chat/completions` with a system instruction that requests JSON:

```json
{
  "classification": "TUNNEL_VISION|NORMAL_PROGRESS",
  "tunnel_confidence": 0.0,
  "rationale": "...",
  "suggested_advice": "..."
}
```

Policy maps tunnel classification / high confidence → `ZOOM_OUT_PROMPT` with advice priority:

1. caller `AdvicePrompt`
2. `ReviewDecision.SuggestedAdvice`
3. contract-enriched template
4. `DefaultZoomOutAdvice`

## Demo

```bash
go run ./cmd/reviewerdemo
```

Shows A) deterministic template, B) fixture SuggestedAdvice, C) remote blocked by local_only.

## Honesty

- Not multi-role product hard-gates (TunnelClassifier as sole role stub).
- Not always-on LLM reminders.
- CI uses HTTP fixtures; live cloud is optional and opt-in.
