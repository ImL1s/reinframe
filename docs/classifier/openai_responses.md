# Native OpenAI Responses classifier adapter (#134)

## Source pin

| Field | Value |
|-------|--------|
| Kind | `openai_responses` |
| API path | `/v1/responses` |
| Official reference | https://developers.openai.com/api/docs/guides/prompt-caching |
| Retrieval date (UTC) | 2026-08-06 |

## Capability profiles

| Profile | CacheMode | prompt_cache_key | structured output |
|---------|-----------|------------------|-------------------|
| `openai-off-v1` | none | omitted | yes |
| `openai-implicit-v1` | implicit_prefix | omitted | yes |
| `openai-explicit-prefix-v1` | explicit_breakpoint | stable hash of model+profile+StablePrefixHash+ruleset+egress | yes |

Unknown profiles fail closed.

## Non-claims

- Generic `openai_compatible` Chat Completions remains **cache-neutral** and must not send OpenAI-native cache fields.
- `CacheHit` is true only when the provider reports **positive** `cached_tokens`.
- No Reinframe exact-assessment memoization (see #138).
- No measured cost/token savings (see #141).
- No Anthropic/Gemini/xAI support in this package path.
