# Native Gemini generateContent classifier adapter (#136)

## Source pin

| Field | Value |
|-------|--------|
| Kind | `gemini_generate_content` |
| REST surface | `{origin}/v1beta/models/{model}:generateContent` |
| Official caching reference | https://ai.google.dev/gemini-api/docs/caching |
| Official API reference | https://ai.google.dev/api |
| Retrieval date (UTC) | 2026-08-06 |
| Auth | `x-goog-api-key` header from `${ENV}` only |

## Capability profiles

| Profile | CacheMode | Min eligible input tokens (est.) | Claim |
|---------|-----------|----------------------------------|-------|
| `gemini-off-v1` | none | — | No cache eligibility claim |
| `gemini-implicit-v1` | implicit_prefix | 2048 | Implicit only; no manual breakpoint |
| `gemini-implicit-min1024-v1` | implicit_prefix | 1024 | Second fixture profile (versioned min) |

Below-minimum estimated input (UTF-8 bytes of plan text / 4, **no padding**) → audit `CacheBackend=gemini_generate_content:cache_ineligible`.

Unknown profiles fail closed at config/capability lookup.

## Explicit cache objects

**Deferred.** This adapter does not create, list, or delete Gemini explicit cache resources. A later issue may add them only with measured reuse/#100 evidence.

## Usage normalization

| Provider field | ProviderUsage |
|----------------|---------------|
| `promptTokenCount` | `InputTokens` |
| `cachedContentTokenCount` | `CacheReadTokens` |
| `candidatesTokenCount` | `OutputTokens` |
| `thoughtsTokenCount` | `ReasoningTokens` |

`CacheHit` is true only when `UsagePresent && CacheReadTokens > 0`.

## Non-claims

- No Gemini Interactions API.
- No OpenAI-compatible path while claiming native Gemini behavior.
- No prompt padding to force cache eligibility.
- No Reinframe exact-assessment memoization (#138).
- No measured cost/token savings (#141).
