# Challenge evaluation — Lane A (deterministic) & Benchmark (#140)

## Scope

Deterministic offline evaluation of the host-neutral challenge core (#131) via:
1. `pkg/evaluation.ChallengeRunner.RunLaneA` (case fixtures evaluation)
2. `pkg/evaluation.ChallengeBenchmarkRunner.Run` (comprehensive appeal, bypass resistance, and recovery quality benchmark)

Detailed benchmark findings, metrics tables, and security threat matrices are published in:
- [`docs/evaluation/challenge_benchmark_report.md`](./challenge_benchmark_report.md)

| Layer | Measured |
|-------|----------|
| Open | Appealability routing (APPEALABLE / NON_APPEALABLE / HUMAN_REVIEW) |
| Justify | Schema/claim validity vs invalid prose |
| Retry | Semantic relationship, Stage 2, true idempotent replay (same CorrelationID) |
| Rewrite bind | Syntax rewrite stays bound (`rewrite_bound_ok`) |
| Hostile reject | Different target after justify must not ALLOW (`hostile_reject_ok`) |
| Nonce security | Nonce tampering, corruption, and missing nonce rejection |
| Replay defense | Rejection of unauthorized correlation ID replays on consumed challenges |
| Budget enforcement | 1-shot retry budget exhaustion rejection (`retry_budget_exhausted`) |
| Lifecycle quality | End-to-end state transitions (`OPEN -> JUSTIFIED -> RETRY_PENDING -> ALLOWED_ONCE -> EXHAUSTED`) |
| Latency percentiles | Mean, P50, P95, P99, Max recovery latency metrics |

**Not scored here:** live Claude host (Lane C / #139), model-backed offline (Lane B).

## Fixtures & Benchmarks

- Fixtures directory: `pkg/evaluation/testdata/challenge_lane_a/*.json`  
  Schema: `reinframe.challenge_eval_case.v1`
- Benchmark runner: `pkg/evaluation/challenge_benchmark.go`  
  Report Schema: `reinframe.challenge_benchmark_report.v1`

## Disposition

Default **MORE-DATA**: fixture-bounded mechanics and synthetic benchmarks only; not production prevalence
or calibrated hard-gate evidence. `hard_gate_enabled` is always false.

## Run

```bash
# Run Lane A fixture evaluation
go test ./pkg/evaluation/ -run ChallengeLaneA

# Run Appeal & Bypass Benchmark Suite
go test ./pkg/evaluation/ -run ChallengeBenchmark

# Run all evaluation suites
go test ./pkg/evaluation/...
```
