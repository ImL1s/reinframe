# Street wire: how Reinframe connects (research → practice)

## What research concluded

1. **Library control plane is ready** (M2.0 + M2.1 tests on `main`).
2. **Live host adapters are not** (#95 Codex live EventSource, #96 Claude PreTool, #97 Actuator).
3. Long **Codex review** sessions need tool-budget / hypothesis detectors (#98), not only compile-error N=3 (#82).
4. Offline path: **read Codex rollout JSONL** → `AgentEvent` (scaffold toward #95).
5. **#98 library detectors landed** (tool-budget + hypothesis-loop fire/no-fire tests). **Not** live host intervention.

## Connection map

```text
┌─────────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│ Claude Code hooks   │     │ Codex CLI        │     │ Log stdout      │
│ PreTool / Prompt    │     │ live process     │     │ (any harness)   │
└─────────┬───────────┘     └────────┬─────────┘     └────────┬────────┘
          │ #96                      │ #95                     │ LogObserver
          │                          │                         │
          ▼                          ▼                         ▼
                 protocol.AgentEvent / TaskSubmitted
                                   │
                    supervisor / detector / policy
                    (#82 #85 #98 library detectors)
                                   │
          ┌────────────────────────┼────────────────────────┐
          ▼                        ▼                        ▼
   HookGate deny/defer      Pending ZOOM_OUT           HumanAlerter
          │                        │
          ▼                        ▼
   host tool gate           Actuator.Deliver  (#97 real, Fake today)
```

## Run the street demo

```bash
cd /path/to/reinframe

# Auto-pick newest ~/.codex/sessions/**/rollout-*.jsonl + synthetic loops + #98 demo
go run ./cmd/streetwire

# Explicit offline wire (read-only; does not control Codex)
go run ./cmd/streetwire -codex "$HOME/.codex/sessions/2026/08/03/<rollout-….jsonl>"

# Synthetic only (CI-friendly; no home Codex required)
go run ./cmd/streetwire -no-codex
```

What you should see:

- **A)** exec/spawn counts from the rollout (when supplied/auto-found); optional #98 tool-budget signal count on offline scan
- **B)** synthetic 3× failure → PreTool defer → Deliver → ACK → allow
- **C)** typo contract + criteria met → full suite **deny** (`disproportionate_scope`)
- **D)** #98 library: tool-budget fire + short no-fire; hypothesis-loop fire + new-evidence no-fire

## Residual detectors (#98 library)

| Detector | Fire | No-fire |
|----------|------|---------|
| `ToolBudgetChurnDetector` | tools since progress ≥ max (default 30, provisional) | short session; progress resets window |
| `HypothesisLoopDetector` | same conclusion fingerprint ≥ N without new evidence IDs | new evidence IDs each probe; under threshold |

Thresholds are **provisional** — not M3 calibrated hard-gates (#100).

## What is NOT connected yet

| Piece | Status |
|-------|--------|
| Live `codex exec` process attach | #95 open |
| Claude PreTool blocking real tools | #96 open |
| Real advice injection into agent | #97 open |
| #98 auto intervention on live hosts | open (library only) |
| Calibrated hard-gates | #100 open |

## Honesty

Streetwire proves **in-process wiring**, **offline Codex observation**, and **#98 library signals**.

It does **not** prove:

- production dual-host supervision
- live Codex attach (#95 product claim)
- Claude PreTool bridge (#96)
- real harness Actuator inject (#97)
- calibrated review-session hard-gates (#100)
