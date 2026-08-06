# Cross-host evaluation framework (#168)

## Lane

`cross_host_eval_fake` — deterministic fake host adapters only (no network, no credentials, no real transcripts).

## Disposition

**MORE-DATA**

- Fake Codex hooks / Grok hooks / Grok ACP / Claude PreTool fixtures.
- Matched live lanes (#164 / #167 / #120) required before any cross-host tunneling ranking.
- ACK layers kept separate; never invent explicit ACK.
- Host fail-open documented for Grok hooks.
- Tunneling scores are zero in the fake framework (no anecdote ranking).

## Run

```bash
go test ./pkg/evaluation/ -count=1 -run CrossHostEvalFake
```
