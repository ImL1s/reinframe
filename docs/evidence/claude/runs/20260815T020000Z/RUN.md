# Claude Live Control Qualification Run (#120)

- **Schema**: `reinframe.claude_live_control.v1`
- **Generated At**: 2026-08-15T02:00:00Z
- **Harness**: reinframe.claude_live_harness.v1
- **Platform**: windows/amd64
- **Final Disposition**: **GO**
- **Passed Scenarios**: 4 / 4

## Mandatory Scenarios Summary

1. **CLAUDE-ALLOW-001** (ALLOW verifiable side effect): **PASS** — ALLOW fixture evaluated cleanly and produced verifiable side effect on disk
2. **CLAUDE-BLOCK-001** (BLOCK side effect absent without terminate): **PASS** — BLOCK fixture withheld tool execution without continue:false; no side effect produced
3. **CLAUDE-LOOP-001** (Session loop continuity after deny): **PASS** — Session loop continuity confirmed: turn 1 deny omitted continue:false, turn 2 benign tool approved
4. **CLAUDE-CTX-001** (Bounded reason/context transport): **PASS** — Bounded reason transport (11 runes <= 500 max) observable without session termination

## Honesty Boundaries & Non-Claims
- Synthetic disposable sandbox execution; zero contamination of operator ~/.claude configuration.
- Closed response shapes; ordinary tool deny omits `continue: false`.
- Context injection strictly bounded within MaxHookReasonRunes (500 runes).
- All local user identifiers and home directory paths redacted.
