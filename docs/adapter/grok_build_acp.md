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
| `initialize` | Handshake + capabilities (stored for negotiation) |
| `authenticate` | Only advertised auth methods; never reads `auth.json` |
| `session/new` | Create session |
| `session/load` | Only when `loadSession` negotiated |
| `session/prompt` | Safe-boundary advice delivery (`ContentBlock[]`) |
| `session/cancel` | Only when cancel negotiated |
| `session/update` (notif) | Stream → AgentEvent summary |

## Process cleanup

- Unix: child `Setpgid`; Close signals the process group (`Kill(-pid)`).
- Windows: `CREATE_NEW_PROCESS_GROUP`; interrupt then kill.
- No shell interpolation; explicit executable path only.

## Capability manifest

- `NewGrokACPFoundationManifest()` — pre-handshake defaults (conservative).
- `ManifestFromNegotiated(ParseGrokACPNegotiatedCaps(initResult))` — **required** after initialize for loadSession/cancel/authMethods/CapPause.
- CapPause only when pause **and** cancel **and** resume are all advertised.

## Headless observe-only (separate profile)

`reinframe.grok_build_headless_observe.v1` — `grok -p --output-format streaming-json`.

- Observe-only: no CapToolGate / CapAdviceDelivery / explicit ACK.
- Thoughts omitted from summaries.
- Tool approvals still require ACP.

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
