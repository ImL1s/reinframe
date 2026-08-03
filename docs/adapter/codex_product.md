# Codex product observation (#107)

## Capability manifest (honest)

| Surface | Supported |
|---------|-----------|
| Observe events (JSONL offline/tail) | **yes** |
| Inject message | no |
| Pre-tool gate | no |
| Pause/cancel/resume native | no |
| Checkpoint/rollback via Codex | no |
| Explicit agent ACK | no |

`DefaultCodexCapabilityManifest()` negotiates **Level 0 Observe**.

## Operator surface

```bash
go run ./cmd/codexctl list -root "$HOME/.codex/sessions"
go run ./cmd/codexctl select -root "$HOME/.codex/sessions" -path /abs/rollout.jsonl
go run ./cmd/codexctl caps
go run ./cmd/codexctl doctor -path /abs/rollout.jsonl -cursor /tmp/cursor.json
```

- Multiple rollouts → **must** pass explicit `-path` (no recency auto-pick).  
- Cursor JSON persists byte offset; truncation resets offset and bumps generation.  
- Never writes Codex session/transcript files.

## Mapping

See `docs/adapter/codex_eventsource.md` + shared `parseCodexRolloutLine`.
