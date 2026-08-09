# Spec: Post-#212/#213 residual close

## Goal
Make fail-closed recovery and closed v2 GO gates complete enough that:
- old suppress markers migrate safely
- GO cannot ignore privacy/provenance/capability failures
- Grok pre-cancel stays retryable
- JSONL missing trailing newline fails closed
- installed groklive validates with embedded schema

## Non-goals
- New live #167 GO report
- Live #108 E2E, exactly-once, explicit ACK, CapPause, multi-host ranking
- Rollback #210–#213

## Acceptance
1. Legacy pipe marker (exactly 3 `|`) → migrate to JSON-array key + atomic rewrite; non-unique pipes → fail closed
2. GO requires valid preflight (usable=true, non-empty version), clean privacy scan, present capability manifest
3. Grok `ctx.Err()` before SessionPrompt → DeliveryBoundary=not_sent
4. Non-empty JSONL without trailing `\n` → Open fails
5. Committed v2 schema embedded; drift test vs docs path; report emission uses same bytes
6. Tests drive shipped Open/DeliverPending/evaluateDisposition/runReport paths; combined packages green

## Skip interview rationale
User + skeptic panel provided exhaustive P1/P2 list; no product ambiguity.
