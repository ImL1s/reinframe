# Claude Code bridge (#96)

**Status:** experimental product bridge (fixture + CLI).  
**Not:** production dual-host supervision claim; real session inject is #97 consumer + host install.

## Surfaces

| Host (Claude Code) | Reinframe core |
|--------------------|----------------|
| PreToolUse JSON | `MapClaudePreToolUseJSON` → `HookRequest` → `EvaluateHook` / `EvaluateBeforeTool` |
| UserPromptSubmit JSON | `MapClaudeUserPromptBridge` → `TaskSubmitted` (+ optional contract) |

Host hook type names stay in `pkg/adapter` only — never `pkg/protocol` type identifiers.

## CLI entry

```bash
# Deny a tool
echo '{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"ls"}}' \
  | go run ./cmd/claudebridge pretool -deny-tool Bash

# Defer while advisory pending
echo '{"session_id":"s1","tool_name":"Edit"}' \
  | go run ./cmd/claudebridge pretool -pending-iv iv-1

# Prompt → TaskSubmitted
echo '{"session_id":"s1","prompt":"fix typo"}' \
  | go run ./cmd/claudebridge prompt
```

Stdout is Claude-compatible decision JSON (`decision`, `hookSpecificOutput.permissionDecision`, plus `reinframe` meta).

## Optional host wiring (document only)

Claude Code hooks (settings) can invoke a binary:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": ".*",
        "hooks": [
          {
            "type": "command",
            "command": "/path/to/claudebridge pretool"
          }
        ]
      }
    ]
  }
}
```

Exact settings schema varies by Claude Code version — verify against current docs before install.  
Reinframe does **not** auto-install into `~/.claude/settings.json`.

## Honesty

- Deny/defer decisions are produced by **shipped** core APIs.
- Without a host hook install, this is library + CLI only.
- Advice delivery into the agent session requires #97 channel consumer (e.g. FileActuator JSONL tailer), not this bridge alone.
