# Challenge evaluation — Lane A (deterministic) (#140)

## Scope

Deterministic offline evaluation of the host-neutral challenge core (#131) via
`pkg/evaluation.ChallengeRunner.RunLaneA`.

| Layer | Measured |
|-------|----------|
| Open | Appealability routing (APPEALABLE / NON_APPEALABLE / HUMAN_REVIEW) |
| Justify | Schema/claim validity vs invalid prose |
| Retry | Semantic relationship, Stage 2, true idempotent replay (same CorrelationID) |
| Rewrite bind | Syntax rewrite stays bound (`rewrite_bound_ok`) |
| Hostile reject | Different target after justify must not ALLOW (`hostile_reject_ok`) |

**Not scored here:** live Claude host (Lane C / #139), model-backed offline (Lane B).

## Fixtures

`pkg/evaluation/testdata/challenge_lane_a/*.json`  
Schema: `reinframe.challenge_eval_case.v1`

## Disposition

Default **MORE-DATA**: fixture-bounded mechanics only; not production prevalence
or calibrated hard-gate evidence. `hard_gate_enabled` is always false.

## Run

```bash
go test ./pkg/evaluation/ -count=1 -race -run ChallengeLaneA
```
