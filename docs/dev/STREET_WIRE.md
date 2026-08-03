# Street wire: how Reinframe connects (research → practice)

## Status (M2.2 residual adapters)

| Piece | Status |
|-------|--------|
| Offline Codex rollout → AgentEvent | **done** (`CodexRolloutSource`) |
| Near-live Codex JSONL tail | **done** (`CodexTailSource` poll follow) |
| Claude PreTool / prompt bridge | **done** (API + `cmd/claudebridge`; experimental) |
| FileActuator advice channel | **done** (JSONL; pending ACK) |
| #98 tool-budget / hypothesis-loop | **library done** + thin `EvaluateSlow` ZOOM_OUT |
| Process-control daemon / global host install | **not claimed** |
| Calibrated hard-gates (#100), git rollback runtime (#99) | **open / out of streetwire** |

## Connection map

```text
Claude PreToolUse JSON ──#96──► EvaluateClaudePreTool → HookDecision → host JSON
Claude UserPrompt JSON ──#96──► TaskSubmitted (#84 mappers)
Codex rollout JSONL ──offline/tail──► AgentEvent → detectors (#82 #85 #98)
                              │
                    supervisor / policy
                              │
              FileActuator JSONL (#97) ←── AdvisoryDelivery (pending ACK)
```

## Run

```bash
go run ./cmd/streetwire -no-codex   # A optional, B–F synthetic residual paths
go run ./cmd/streetwire             # + offline Codex if ~/.codex/sessions has rollouts

# Claude bridge CLI
echo '{"session_id":"s","tool_name":"Bash"}' | go run ./cmd/claudebridge pretool -deny-tool Bash
```

Sections: **A** offline Codex · **B** M2.0 loop · **C** over-SOP · **D** #98 library · **E** Claude bridge · **F** FileActuator.

## Honesty

Streetwire proves **in-process wiring**, offline/near-live **observation**, Claude **fixture/CLI bridge**, and a **file advice channel**.

It does **not** prove:

- automatic install into `~/.claude/settings.json` or Codex product config
- live Claude session E2E block in CI
- live Codex process attach / SIGSTOP pause
- calibrated M3 hard-gates or git checkpoint product (#99/#100)
- dual-host production supervision without residual host consumers of FileActuator
