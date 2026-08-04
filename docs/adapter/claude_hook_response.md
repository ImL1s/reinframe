# Claude PreTool response semantics (#116)

**Pinned profile:** `reinframe.claude_hook_response.v1`  

**Host pin honesty:** Claude Code does not publish a single stable public Hook JSON schema version pin that Reinframe can freeze forever. This profile is Reinframe’s closed subset of the documented PreToolUse fields (`decision`, `hookSpecificOutput.permissionDecision` = allow|deny|ask). Unknown host mode uses fail-closed (`unsupported_host_version`). Live version evidence is tracked under #120.

## Tool-level BLOCK

Use:

- `decision: "block"`
- `hookSpecificOutput.permissionDecision: "deny"` (or `"ask"` for native defer)

**Do not** set `continue: false` for ordinary tool deny — that is treated as whole-session stop.

## Defer

| Host mode | Behavior |
|-----------|----------|
| headless / NativeDefer=false | degrade to deny + `defer_degraded:` reason |
| interactive + NativeDefer=true | `permissionDecision: ask` |

## Productivity vs security

- Productivity timeout may fail-open when configured
- Deterministic tool/security deny never fails open from productivity timeout flag
