# Process-local exact RawAssessment cache (#138)

## What this is

| Layer | Behavior |
|-------|----------|
| Provider prompt cache (#134–#137) | Reuses provider-side prefix computation; call still happens |
| **Reinframe exact cache (#138)** | Skips the provider call when Stage-1 input identity is exact |

Does **not** cache `ResolvedDecision`. Stage 2 (thresholds, exceptions, challenge authority) always reruns on the consumer side.

## Key (content-addressed, secret-free)

Version: `reinframe.exact_assessment_cache_key.v1`

Includes provider kind/model/profile/egress, parser schema, **stable-prefix hash** (not full prompt/input hashes — those embed retry_budget and other Stage-2-only fields), ruleset, policy class, task ID/objective/acceptance, contract/evidence revisions, window meta, event ID+content hashes, proposed-action semantic fingerprint (tool/command/path/args/payload hash), model-visible exception flags, challenge justification identity **except** retry budget, and **`session_id`** for session/tenant partition.

Empty `SessionID` is a shared default partition only (callers that omit session share that partition). Egress profile further isolates multi-egress deployments.

On hit, host rebinds `Assessment.PromptHash` / ruleset fields to the current request so `ValidateProviderResultForRequest` stays consistent across session/retry-budget changes within the same partition.

**Non-cacheable:** `SECURITY` policy class, legacy fixture mode, missing ruleset hash when ID set, missing stable-prefix hash.

**Wiring:** `NewClassifierProviderFromConfigWithExactCache` or `WrapWithExactCache`.

## Admission

Only successful Stage-1 results with OK parse status and no error/fallback class. Never admits timeout/transport/parse/fail-open outcomes.

## Bounds (process-local)

Default when enabled: max 1024 entries, 16 MiB, TTL 10m, singleflight on. Disabled by default in config.

## Singleflight

Concurrent identical keys produce one provider call.

- Shared `Assess` runs under `context.WithoutCancel` so one waiter cancel does not cancel peers.
- The first registrant (leader) receives real provider usage; additional waiters get zero-usage `reinframe_exact` coalesced results (no token double-count).
- If the leader cancels before observing the shared result, usage is best-effort unattributed rather than double-counted; remaining waiters still receive the assessment.
- Provider failures are not admitted; later callers may retry.

## Observability

Exact hit sets `Usage.CacheBackend=reinframe_exact` with zero provider tokens for that invocation. Provider usage from the source call is not rewritten onto waiters.

## Config

```yaml
classifier_cache:
  exact:
    enabled: true
    max_entries: 1024
    max_bytes: 16777216
    ttl: 10m
  singleflight: true
```
