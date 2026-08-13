# Legacy correlation note — `20260811T073954Z`

This pin predates GPT-5.6 Pro / #199 source-correlation hardening (pre-prompt watermark +
request / intervention / challenge identity match).

## What remains valid

- Mechanical harness disposition **NO_GO** (HOOK-DENY-001 FAIL / host fail-open) is retained.
- Live execution evidence (hooks, challenge transport, etc.) remains historical fact.
- Scenario JSON on disk is **immutable** campaign output.

## What must not be claimed

- **Do not** treat recorded `ACP-SESSION-001` PASS + `session_visible` as
  **source-correlated** under current gates.
- Correlation was temporal (session-matched update without request/intervention/challenge
  identity binding). Under current product rules this would be **INCONCLUSIVE** + **transport**.

## Superseding primary pin

Use `docs/evidence/grok_build/runs/20260811T130935Z/` for current campaign claims.
Under the executable-binding gate that pin re-evaluates to **NO_GO** (missing
`live_grok_executable.json`; scenarios retained; not a fresh live campaign).
Strongest ACK remains **transport**; provenance remains **derived=true** split.
