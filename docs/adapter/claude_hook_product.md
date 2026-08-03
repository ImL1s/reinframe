# Claude Code project-local hook productization (#106)

## Capability table (pinned experimental)

| Claude surface | Reinframe | Notes |
|----------------|-----------|-------|
| PreToolUse | `EvaluateClaudePreTool` / HookGate | Primary control surface |
| UserPromptSubmit | `MapClaudeUserPromptBridge` | Intake only |
| PostToolUse / Stop | observation / turn_end (future) | Not installed by default |
| SessionStart / PreCompact | restore pending (future) | Version-dependent |

Unsupported fields: never emit claimed `defer` as allow; map Reinframe `defer` to host **block** with reason (existing bridge).

## Installer

`ClaudeSettingsManager` + CLI:

```bash
go run ./cmd/claudeinstall plan   -settings .claude/settings.json -command 'go run ./cmd/claudebridge pretool'
go run ./cmd/claudeinstall install -settings .claude/settings.json -command '…'
go run ./cmd/claudeinstall doctor  -settings .claude/settings.json
go run ./cmd/claudeinstall uninstall -settings .claude/settings.json
```

- **Project-local by default** (operator passes path).  
- No silent global `~/.claude` write.  
- Idempotent; preserves foreign hooks; backup `.reinframe.bak`.  
- Malformed JSON fail-closed.

## Live smoke

Opt-in / manual: pin Claude Code version, OS, Reinframe commit, redacted settings, ALLOW/BLOCK fixture.  
Without pinned evidence, README stays **experimental**.

## ACK honesty

Installer does not deliver advice. FileActuator transport remains `transport` at best until #108.
