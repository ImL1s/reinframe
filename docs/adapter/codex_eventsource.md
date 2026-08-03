# Codex EventSource (offline + near-live)

## Offline — `CodexRolloutSource`

One-shot scan of `rollout-*.jsonl` → `tool_call` / `error` `AgentEvent`s.  
Used by streetwire and unit fixtures.

## Near-live — `CodexTailSource`

Poll-follows a growing rollout file (default 50ms). Options:

- `StartAtEnd` — skip history
- `MaxEvents` — stop after N emissions (tests)
- `PollInterval`

Not a `codex exec` process attach and not a product daemon.

## Mapping (host → core)

| Codex JSONL | AgentEvent |
|-------------|------------|
| `session_meta` | session id / meta only (no event) |
| `response_item` + `custom_tool_call` / `function_call` | `tool_call` |
| `*_tool_call_output` with failure text | `error` |

## Honesty

#95 closed on offline scaffold + this near-live tail. Process-control product claim remains out of scope.
