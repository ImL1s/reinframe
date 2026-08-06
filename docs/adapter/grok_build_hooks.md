# Grok Build native hooks (#165)

## Profile

`reinframe.grok_build_hooks.2026-08-06.v1`

Pins (retrieved 2026-08-06):

- https://docs.x.ai/build/features/hooks
- https://docs.x.ai/build/features/permissions
- Local: `~/.grok/docs/user-guide/10-hooks.md`

## Honesty

- **Native hook foundation only**; live proof is **#167**.
- Host timeout / crash / malformed output is **fail-open** — only an explicit valid `{"decision":"deny"}` blocks PreToolUse.
- Never CapPause / Level 2 / explicit ACK from hook response.
- Never write `~/.grok/auth.json` or session history.
- xAI classifier provider (#137) is unrelated.

## Install

```bash
go build -o grokhooks ./cmd/grokhooks
./grokhooks plan -project .
./grokhooks install -project .
# Operator: /hooks-trust or --trust for project hooks
./grokhooks doctor -project .
./grokhooks uninstall -project .
./grokhooks manifest
```

Writes `<project>/.grok/hooks/reinframe-pretool.json` with ownership markers.

## PreToolUse mapping

```text
ALLOW → {"decision":"allow"}
BLOCK/DEFER → {"decision":"deny","reason":"..."}  (+ optional ChallengeID text)
```

## Capability

`NewGrokHooksFoundationManifest()`: CapHooks + CapToolGate (explicit deny only); `fail_closed_hooks=false`.
