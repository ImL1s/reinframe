# Challenge evaluation — Lane B model offline fake (#140)

## Scope

Offline model-backed checks using **fake HTTP only**:

- challenge re-eval with `FakeClassifierProvider` + Stage-2 exception;
- native OpenAI Responses adapter (`httptest`) structured assessment;
- exact-cache hit skips second provider call;
- malformed parse + 401 no-retry.

**Not included:** live Claude host (Lane C), real API credentials, measured savings.

## Disposition

**MORE-DATA** — mechanics only; `hard_gate_enabled=false`.

## Run

```bash
go test ./pkg/evaluation/ -count=1 -race -run ModelLaneB
```
