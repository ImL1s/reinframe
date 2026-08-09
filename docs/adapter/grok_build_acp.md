# Grok Build ACP stdio bridge (#166 / #191)

## Profile

`reinframe.grok_build_acp.v1` — JSON-RPC protocolVersion **1** (exact; fail-closed).

Launch (production):

```text
grok --no-auto-update agent stdio
```

Pins (2026-08-06): https://docs.x.ai/build/integrations/acp , https://docs.x.ai/build/cli/headless-scripting

## Methods

| Method | Purpose |
|--------|---------|
| `initialize` | Handshake; exact `protocolVersion` required; closed capability/auth parse |
| `authenticate` | **Delegated auth only**: advertised `methodId` + `_meta.headless` — **no token field** |
| `session/new` | Create session (after initialize) |
| `session/load` | Only when `loadSession` negotiated |
| `session/prompt` | Safe-boundary advice delivery (`ContentBlock[]`) |
| `session/cancel` | Only when cancel negotiated |
| `session/update` (notif) | Stream → AgentEvent summary |

### Authenticate contract (#191)

Credential ownership stays with the Grok process / environment (`XAI_API_KEY` or local login).
Reinframe **never** accepts, stores, forwards, logs, or error-echoes raw credentials or `~/.grok/auth.json`.

Official request shape:

```json
{"methodId":"…","_meta":{"headless":true}}
```

## Process cleanup (#191)

- **Unix:** child `Setpgid`; Close signals the process group (`Kill(-pid)`), graceful → force.
- **Windows:** Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` + `TerminateJobObject` on force; not root-only `Process.Kill()`.
- Headless observe uses the same ownership helpers.
- Reader EOF/crash fails all pending RPCs immediately with a terminal transport error.
- No shell interpolation; explicit executable path only.

## Capability manifest

- `NewGrokACPFoundationManifest()` — pre-handshake: **no** achieved caps; `NegotiatedLevel = -1`.
- `ManifestFromNegotiated` builds a `protocol.CapabilityManifest` and calls **`protocol.EvaluateAchievableLevel`**.
- Partial pause/cancel/resume **never** yields Level 2 without the full Level 1 mask + `CapDiffInspection` + native pause/cancel/resume.
- Session/prompt proves advice delivery + event stream only; tool/diff inspection require explicit ads.
- No unbounded `Raw map[string]any` on negotiated caps (bounded `CapsDigest` only).

## Headless observe-only (separate profile)

`reinframe.grok_build_headless_observe.v1`:

```text
grok --no-auto-update -p <PROMPT> --output-format streaming-json
```

- Observe-only: no CapToolGate / CapAdviceDelivery / explicit ACK.
- Thoughts omitted from summaries.
- Fake-exec tests lock argv order/shape.

## ACK layers

| Layer | When recorded |
|-------|----------------|
| `transport` | JSON-RPC success |
| `session_visible` | `session/update` observed after delivery |
| `explicit` | **Never** from JSON-RPC alone |
| `behavioral` | Future live proof only |

## Non-claims

- Never read/write/log `~/.grok/auth.json`
- No CapPause / Level 2 unless canonical evaluator says so from full mask
- Hooks remain #165; composition is optional
- Historical live run under **#167** (darwin/`grok 1.0.0`, harness `cmd/groklive`); public disposition **MORE_DATA** — **#199** v2 gates closed on main (no new live v2 GO without full matrix/correlation)
- **#108** foundation + **#200** source-bound ACK / durable honesty on main; live E2E composition **not claimed**
- Transport proven; source-correlated `session_visible` and explicit ACK **not** claimed for current Grok profile

## Process safety

- Explicit executable resolution; no shell interpolation
- Bounded message size / queue / startup timeout / auth method count
- Graceful interrupt then force tree kill; pending RPC terminal on reader death

## CI gate note

Four-job CI (Lint + ubuntu/macos/windows) is required for merge of #191; local Superpowers exact-head APPROVE is mirrored on the PR.
