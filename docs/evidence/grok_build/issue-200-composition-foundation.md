# #200 composition evidence (foundation + unit)

## What is proven offline (this PR)

Unit/integration tests drive the **shipped** APIs:

| Path | Proof |
|------|--------|
| Bare `Acknowledge(id, "acked")` | Unit: `ErrBareAcknowledgeExplicit` |
| `AcknowledgeSource` + Grok + explicit | Unit: `ErrExplicitACKNotSupported` |
| Symlink ledger path | Unit: open rejects |
| `AcknowledgeSource` session_visible + ledger | Code path + API tests (source fields on transition) |
| Host-accepted + ledger fail | `ErrDurableWriteFailed` + `StateAmbiguous`; sidecar suppress marker for restart |
| Ambiguous restart (process-sim) | Unit: re-open ledger + new queue → `StateSuppressed` (no second Deliver) |
| Ledger without `DedupeHostFamily` | Fail-closed at `NewAdvisoryDelivery` (Grok actuator infers `grok_build`) |
| Pre-send not_sent + ledger fail (#208) | No suppress marker; retryable after durability repair |
| Suppress recovery incomplete (#208) | Open fails closed (no usable ledger with partial `seen`) |
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
