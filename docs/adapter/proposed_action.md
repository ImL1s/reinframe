# ProposedAction projection (#115)

**Status:** library contract  
**Schema version:** `reinframe.proposed_action.v1`

## Purpose

Host tool names and proposed work must not be conflated. Claude Code sends:

```json
{"tool_name":"Bash","tool_input":{"command":"go test -race ./..."}}
```

- `ToolName` = host tool id (`Bash`)
- `Command` = shell text (`go test -race ./...`)
- Policy over-SOP / future classifiers use `Command` via `FullSuiteCommand` / `ProposedAction`

## Type

See `pkg/adapter/proposed_action.go` (`ProposedAction`).

| Field | Notes |
|-------|--------|
| SchemaVersion | closed version string |
| ToolName | never a full shell command |
| ToolClass | shell / edit / read / search / other / unknown |
| Command | shell text when applicable |
| RedactedPayload | bounded JSON; secrets → `[REDACTED]` |
| Source | provenance (`claude_pretool`, `codex_rollout`, …) |

## Mapping entry points

- `MapClaudePreToolUseJSON` → `ClaudePreToolInput.Proposed`
- `ProposedActionFromClaudePreTool`
- `ProposedActionFromCodexTool` (shared shape; does not invent missing fields)
- `HookRequest.Proposed` for policy (`EvaluateBeforeTool`)

## Non-claims

- Not classifier runtime (#105)
- Not live Claude install (#120)
- Not calibrated hard-gate (#100)
