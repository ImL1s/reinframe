# Codex project-local hooks control (#163)

## Profile

`reinframe.codex_hooks.2026-08-06.v1`

Official pin (retrieved 2026-08-06):

- https://developers.openai.com/codex/hooks
- https://github.com/openai/codex

## What this is

Project-local **hook-control foundation** mapping Codex command-hook stdin/stdout into Reinframe `HookRequest` / `ProposedAction` / `HookDecision` and back to the pinned Codex response shape.

This is **not** live production proof (#164), not process attach, not transcript mutation, and not explicit agent ACK.

## Install surface

```bash
# From repo root (after build)
go build -o codexhooks ./cmd/codexhooks
./codexhooks plan -project .
./codexhooks install -project .
# Operator must review+trust via Codex CLI: /hooks
./codexhooks doctor -project .
./codexhooks uninstall -project .
./codexhooks manifest
```

Install writes only `<project>/.codex/hooks.json` with Reinframe ownership markers. Unrelated user hooks are preserved. Symlink parents, cross-project paths, and rollout/transcript paths are rejected.

## Events

| Event | Mapping |
|-------|---------|
| `SessionStart` | Optional bounded `additionalContext` |
| `UserPromptSubmit` | Optional bounded `additionalContext` |
| `PreToolUse` | ProposedAction + EvaluateHook → allow/deny + optional challenge context |
| `PermissionRequest` | allow / deny / fall-through (empty decision) |
| `PostToolUse` | No-op foundation |
| `Stop` / `SessionEnd` | No-op foundation |

### Covered local tools

Bash, `apply_patch` / Edit / Write, local MCP (`mcp__…`), other local function tools.

### Explicitly unsupported

Hosted tools such as `WebSearch` — not represented as gated.

## Decision mapping

```text
allow  → permissionDecision allow
deny   → decision block + permissionDecision deny + reason
defer  → deny + bounded additionalContext (challenge path)
```

Appealable BLOCK additionalContext may include canonical `ChallengeID` and one-shot retry instructions. It does **not** grant permission from text alone and does not implement a second challenge state machine.

Ordinary deny does **not** set `continue: false` (unsupported for PreToolUse; would fail the hook).

## Capability manifest

`CodexHooksFoundationManifest()` may advertise CapEventStream, CapToolInspection, CapHooks, CapToolGate, CapContextInjection, CapTurnBoundary, and CapAdviceDelivery (additionalContext path only).

Never: CapPause, CapInterventionAck from hook response, Level 2, explicit ACK.

`DefaultCodexCapabilityManifest()` remains observe-only Level 0 until live proof upgrades claims.

## Failure semantics

- Malformed/oversized/invalid UTF-8 stdin → adapter fail-closed deny for tool events
- Hook crash/timeout: host fail-open for that guardrail (Codex continues tool call per host docs)
- Trust: changed hook content invalidates doctor profile hash; operator must re-trust via `/hooks`
- Not containment: sandbox/approval policy still required above hooks

## Non-claims

- No live Codex smoke (#164)
- No hosted-tool gating
- No explicit agent ACK from hook exit/JSON success
- No native pause/cancel/resume
- No rollout/transcript writes
- No private API / process injection
