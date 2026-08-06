# Classifier provider runtime (#132)

Generic foundation only. **Not** native OpenAI Responses, Anthropic Messages, Gemini, xAI, provider-native caching, memoization, singleflight, voting/failover, or hard-gate calibration.

## Construction path

YAML alone does **not** auto-wire a network provider. Use:

```go
p, err := classifier.NewClassifierProviderFromConfig(cfg.ClassifierProvider, classifier.ProviderFactoryOptions{})
```

- `kind` empty / `none` → `FakeClassifierProvider` (no network)
- `kind: openai_compatible` → `OpenAICompatibleProvider` (loopback-only by default)

## Canonical endpoint URL contract

```yaml
classifier_provider:
  kind: openai_compatible
  model: "..."
  base_url: "http://127.0.0.1:11434"   # origin only — no path, query, fragment, or userinfo
  path: "/v1/chat/completions"           # absolute path; default if empty
  api_key_ref: "${REINFRAME_CLASSIFIER_API_KEY}"
  timeout_ms: 1500
  max_input_bytes: 65536
  max_output_bytes: 8192
  capabilities_profile: "generic-none-v1"
```

Built with `url.URL` join — never `base + path` string concat.  
Documented config reaches **`/v1/chat/completions`**, never `/v1/v1/chat/completions`.

## Canonical contract

`ClassifierProvider.Assess(ctx, ProviderRequest) (ProviderResult, error)`

- `ProviderRequest` is the per-call authority for timeout and byte limits.
- Config and capability values are **upper bounds only** (never widen a stricter request).
- Provider failures are typed; resolver owns PRODUCTIVITY fail-open vs SECURITY fail-closed.
- Public decision remains exactly `ALLOW | BLOCK`. Shadow remains `Enforced=false`.

## PromptPlan

- **StablePrefix**: classifier policy, schema, reason codes, rules, examples, version labels.
  Stable `RulesetHash` is the **classifier prompt/policy** hash (`builtin-ruleset-v1` by default).
- **DynamicSuffix**: session/task, **current** `RulesetID`/`RulesetHash`, evidence, proposed action.
- Changing only current task RulesetHash preserves StablePrefixHash and changes InputHash/PromptHash.
- `ValidateProviderRequest` recomputes hashes and binds Input ↔ DynamicSuffix (rejects mutation / stale hashes).

## Capabilities

- Trusted profiles only. Unknown profile fails closed.
- Default: `generic-none-v1` → `CacheMode=none`, no native structured output.
- Generic adapter never sends `cache_control`, `prompt_cache_key`, `cachedContent`, `x-grok-conv-id`.

## Strict RawAssessment parser

Exactly one JSON object; second Decode must return `io.EOF`.  
JSON whitespace only: SP/TAB/LF/CR (NBSP/EM SPACE rejected at boundaries).  
Evidence IDs require a non-empty allowlist when present.

## Retry / timeout

- Overall Assess context timeout covers HTTP attempts, Retry-After sleeps, and parsing.
- `Retry-After` honored, capped at 5s and remaining deadline.
- Bounded retries for 429/5xx/transport only.

## Secrets

- `${ENV}` placeholders only; identifier `[A-Za-z_][A-Za-z0-9_]*`.
- Disabled `kind: none` requires all other fields empty.
- Validation errors never echo raw secret values.

## Audit

`ProviderCallAudit` includes model_version, reasoning_tokens, cache_key_hash, and `usage_present`.  
No raw prompts, secrets, or unrestricted bodies.
