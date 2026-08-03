# FileActuator — local advice channel (#97)

Non-fake `InterventionActuator` that appends `ZOOM_OUT_PROMPT` (and other interventions) as JSON lines to a file.

## Envelope (`reinframe.advice.v1`)

```json
{
  "schema": "reinframe.advice.v1",
  "intervention_id": "iv-zoom-…",
  "session_id": "…",
  "action_type": "ZOOM_OUT_PROMPT",
  "advice_prompt": "…",
  "delivery_mode": "advice",
  "requires_ack": true,
  "delivered_at": "RFC3339Nano",
  "fingerprint": "…"
}
```

## Behaviour

- `Deliver` → `Accepted=true`, `AckStatus=pending` (explicit ACK required; no AutoAck theater).
- Works with `AdvisoryDelivery` + orchestrator vertical slice (defer until ACK).
- Target harness must **consume** the file (tail / IPC stub). Writing the file alone does not inject into Claude/Codex process memory.

## Usage

```go
act := &adapter.FileActuator{Path: "/var/run/reinframe/advice.jsonl"}
del, _ := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
    Actuator: act, SupportsAdviceDelivery: true,
})
```

## Honesty

This satisfies #97 “production-shaped actuator + tests” with a documented local channel.  
It does **not** claim live Claude/Codex session injection without a host-side consumer.
