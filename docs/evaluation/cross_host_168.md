# Cross-host evaluation framework (#168)

## Lanes

| Lane | Description |
|------|-------------|
| `cross_host_eval_fake` | Deterministic fake host adapters only (CI; no network/credentials/transcripts). |
| `cross_host_eval_partial_live` | Fake fixtures **plus** one live Grok Build pin from #167. |

## Disposition

**MORE-DATA** (unchanged after single live host)

- Fake Codex hooks / Grok hooks / Grok ACP / Claude PreTool fixtures remain.
- **Historical Grok live pin present** after #167 live run (public MORE_DATA; #199 v2 gates closed) (`grok 1.0.0` darwin, hooks allow/deny/fail-open, ACP `session_visible`).
- Matched live lanes still missing: **#164 Codex**, **#120 Claude**.
- **No host/model ranking**; tunneling scores stay zero.
- ACK layers kept separate; never invent explicit ACK.
- Host fail-open recorded for Grok hooks (live + fake).

## Live pin (Grok #167)

- Evidence: `docs/evidence/grok_build/issue-167-live-grok-1.0.0-3cd0d0cbcebe-stable-darwin-2026-08-08.json`
- Strongest ACK observation: post-prompt session/update (not source-correlated product claim)
- Sample size: n=1 OS (darwin/arm64)
- API: `evaluation.AttachLiveGrok167Lane` / `DefaultLiveGrok167Pin`

## Run

```bash
go test ./pkg/evaluation/ -count=1 -run 'CrossHostEvalFake|AttachLiveGrok'
```
