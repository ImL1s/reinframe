# M3 synthetic + FP benchmarks (#100)

**Status:** offline evaluation library + `cmd/bench`  
**Hard-gates:** **never enabled** by this track  
**Thresholds:** provisional only (profile `provisional-50`)

## Entry

```bash
go test -count=1 -race ./pkg/evaluation/
go run ./cmd/bench -dataset testdata/evaluation -out docs/evaluation/reports/synthetic-m3-v1.json
```

## What is measured separately

1. Detector signal quality (#82/#85/#98)
2. Shadow classifier Stage 2 ALLOW|BLOCK vs labels (#105)
3. Exception paths (user / repo / flaky) after raw score
4. False-block rate (primary PRODUCTIVITY safety metric)

## Dataset

- Schema: `reinframe.benchmark_case.v1`
- Location: `testdata/evaluation/*.json`
- Classes: positive_deviation, healthy_counterexample, boundary_robustness

## Disposition

Each report ends with exactly one of:

| Disposition | Meaning |
|-------------|---------|
| **NO-GO** | Remain shadow/advisory |
| **LIMITED-GO recommendation** | Propose narrow follow-up promotion issue only |
| **MORE-DATA** | Expand dataset before any promotion |

The synthetic suite is expected to return **MORE-DATA** (small sample). It is **not** scientific calibration.

## Non-claims

- Does not flip shadow → enforced mode
- Does not transfer thresholds across models/versions
- Severity is not treated as a probability without calibration evidence
