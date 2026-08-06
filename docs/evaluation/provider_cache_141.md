# Provider / exact-cache evaluation (#141)

## Lane

`provider_cache_eval_fake_ci` — deterministic fake HTTP only.

## Modes covered

| Mode | Intent |
|------|--------|
| `uncached_provider` | Baseline call, no cache hit |
| `provider_cache_warm_read` | Transport reports positive cache-read tokens (OpenAI/Anthropic/Gemini/xAI) |
| `reinframe_exact_hit` | Exact cache skips second provider call |
| `singleflight_N_callers` | Concurrent identical keys → one provider call |
| `required_miss_after_model_change` | Model identity busts exact key |
| `required_miss_after_event_change` | Event content hash busts exact key |
| `generic_openai_compatible_cache_neutral` | Generic adapter rejects native cache profiles |

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
