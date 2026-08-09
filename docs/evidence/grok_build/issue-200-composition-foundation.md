# #200 composition evidence (foundation + unit)

## What is proven offline (this PR)

Unit/integration tests drive the **shipped** APIs:

| Path | Proof |
|------|--------|
| Bare `Acknowledge(id, "acked")` | Returns `ErrBareAcknowledgeExplicit` — cannot mint explicit |
| `AcknowledgeSource` with Grok profile + `AckLayer=explicit` | Returns `ErrExplicitACKNotSupported` |
| `AcknowledgeSource` with `session_visible` | Durable ledger append + state SESSION_VISIBLE |
| Ledger write failure after host deliver | Surfaces `ErrDurableWriteFailed` (no silent success) |
| Symlink ledger path | `OpenDurableAdviceLedger` rejects |
| FAILED state | Not permanent suppress (retry allowed) |

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
