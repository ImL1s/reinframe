# Classifier provider runtime (#132)

Generic foundation only. **Not** native OpenAI Responses, Anthropic Messages, Gemini, xAI, provider-native caching, memoization, singleflight, voting/failover, or hard-gate calibration.

## Canonical contract

`ClassifierProvider.Assess(ctx, ProviderRequest) (ProviderResult, error)`

- `ProviderRequest`: versioned input + `PromptPlan` + timeout/byte bounds.
- `ProviderResult`: `RawAssessment` + transport-only `ProviderUsage` + bounded `ProviderMeta`.
- Provider failures are typed; resolver owns PRODUCTIVITY fail-open vs SECURITY fail-closed.
- Public decision remains exactly `ALLOW | BLOCK`. Shadow remains `Enforced=false`.

## PromptPlan

- **StablePrefix**: policy, closed schema, reason codes, rules, stable examples, version labels.
- **DynamicSuffix**: session/task/action/evidence and request-specific meta.
- Hashes use deterministic structured JSON encoding (not delimiter concatenation).
- Dynamic-only changes preserve `StablePrefixHash`; change `InputHash` / `PromptHash`.

## Capabilities

- Trusted profiles only. Unknown profile fails closed.
- Default profile: `generic-none-v1` → `CacheMode=none`, no native structured output, no cache key/telemetry/continuation.
- Never infer capabilities from model text.
- Generic adapter never sends `cache_control`, `prompt_cache_key`, `cachedContent`, `x-grok-conv-id`.

## Strict RawAssessment parser

Exactly one JSON object; closed fields; integer severity 0–100; closed reason codes; evidence IDs validated against request input; reject fences, prose, duplicates, floats, coercion, oversized, invalid UTF-8, deep nesting, and usage/meta injection keys in content.

## Generic OpenAI-compatible adapter

- Env placeholder API key only (`${REINFRAME_CLASSIFIER_API_KEY}`).
- Loopback base URL by default (ADR 003); tests may set `AllowRemote`.
- Bounded request/response reads; no unbounded `io.ReadAll`.
- No redirect following to alternate hosts by default.
- Bounded retries for 429/5xx/transport only; non-retryable 4xx not retried.
- Usage only from response `usage` object — never from model content.

## Audit

`ProviderCallAudit` records provider/model/profile, hashes, usage, HTTP status, latency, retries, request id, parse/fallback class. No raw prompts, secrets, or unrestricted bodies.

## Configuration

```yaml
classifier_provider:
  kind: openai_compatible   # or none/empty
  model: "..."
  base_url: "http://127.0.0.1:11434/v1"
  path: "/v1/chat/completions"
  api_key_ref: "${REINFRAME_CLASSIFIER_API_KEY}"
  timeout_ms: 1500
  max_input_bytes: 65536
  max_output_bytes: 8192
  capabilities_profile: "generic-none-v1"
```

Separate from `reviewer.*`. Empty kind keeps FakeClassifierProvider / no network.
