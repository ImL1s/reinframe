# Provider / exact-cache evaluation (#141)

## Lane

`provider_cache_eval_fake_ci` — deterministic fake HTTP only.

## Modes covered

| Mode | Intent |
|------|--------|
| `stage0_only` | No provider invocation (deterministic skip layer) |
| `uncached_provider` | Baseline call, no cache hit |
| `provider_cache_cold_write` | Usage present, zero cache-read / positive write — not a hit |
| `provider_cache_warm_read` | Transport reports positive cache-read tokens (OpenAI/Anthropic/Gemini/xAI) |
| `dynamic_only_provider_cache` | Stable prefix identical; dynamic events change → exact miss |
| `reinframe_exact_hit` | Exact cache skips second provider call |
| `singleflight_N_callers` | Concurrent identical keys → one provider call |
| `required_miss_after_model_change` | Model identity busts exact key |
| `required_miss_after_event_change` | Event content hash busts exact key |
| `invalid_admission_rejected` | Transport/parse failures never enter exact cache (count=0) |
| `generic_openai_compatible_cache_neutral` | Generic adapter rejects native cache profiles |

## Correctness aggregates (fake CI)

| Metric | Requirement on green run |
|--------|--------------------------|
| `stale_hit_rate` | `0` |
| `invalid_admission_count` | `0` (transport/parse failures never admitted) |
| `hard_gate_enabled` | `false` |
| `default_cache_on` | `false` |
| `disposition` | **MORE-DATA** |

Post-campaign fix PR **#169** expanded modes and proved `invalid_admission_count=0` on the exercised paths.

## Disposition

**MORE-DATA**

- No real provider credentials.
- No billed token cost or universal savings percentage.
- Default exact-cache enablement is **not** authorized by this evaluation.
- `hard_gate_enabled=false`.

Live economics require separate opt-in, budget-bounded runs.

## Run

```bash
go test ./pkg/evaluation/ -count=1 -race -run CacheEvalFakeCI
```
