# Grok Build ACP stdio bridge (#166)

## Profile

`reinframe.grok_build_acp.v1` — JSON-RPC protocolVersion **1**.

Launch (production):

```text
grok --no-auto-update agent stdio
```

Pins (2026-08-06): `~/.grok/docs/user-guide/15-agent-mode.md`, docs.x.ai ACP/headless.

## Methods

| Method | Purpose |
|--------|---------|
| `initialize` | Handshake + capabilities |
| `session/new` | Create session |
| `session/load` | When advertised |
| `session/prompt` | Safe-boundary advice delivery |
| `session/update` (notif) | Stream → AgentEvent summary |

## ACK layers

| Layer | When recorded |
|-------|----------------|
| `transport` | JSON-RPC success |
| `session_visible` | `session/update` observed after delivery |
| `explicit` | **Never** from JSON-RPC alone |
| `behavioral` | Future live proof only |

## Non-claims

- Never read/write/log `~/.grok/auth.json`
- No CapPause unless negotiated live
- Hooks remain #165; composition is optional
- Live proof is **#167**

## Process safety

- Explicit executable resolution; no shell interpolation
- Bounded message size / queue / startup timeout
- Graceful interrupt then kill; no orphan claim without Wait
