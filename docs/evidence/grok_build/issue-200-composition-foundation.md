# #200 composition evidence (foundation + unit)

## What is proven offline (this PR)

Unit/integration tests drive the **shipped** APIs:

| Path | Proof |
|------|--------|
| Bare `Acknowledge(id, "acked")` | Unit: `ErrBareAcknowledgeExplicit` |
| `AcknowledgeSource` + Grok + explicit | Unit: `ErrExplicitACKNotSupported` |
| Symlink ledger path | Unit: open rejects |
| `AcknowledgeSource` session_visible + ledger | Code path + API tests (source fields on transition) |
| Host-accepted + ledger fail | Code returns `ErrDurableWriteFailed` and `StateAmbiguous` (suppresses redelivery) |
| Live E2E composition | **Not claimed** |

## What is still not claimed

```text
live source record → durable PENDING → GrokACPActuator → source-correlated receipt
→ durable ACK → restart duplicate suppression against live host
```

Historical #167 live harness called ACP `SessionPrompt` directly; it does **not** compose the #108 consumer end-to-end live.

## How to re-run unit evidence

```bash
go test ./pkg/adapter/ ./pkg/supervisor/ ./cmd/groklive/ -count=1
```
