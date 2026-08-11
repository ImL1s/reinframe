# Privacy errata — run 20260810T170640Z

## Status

| Claim | Original | Corrected |
|-------|----------|-----------|
| `privacy.complete` | `true` | **INVALID / REVOKED** for privacy honesty of this run's sidecars |
| `raw_thoughts_stored` | `false` | **FALSE NEGATIVE** — host `thought` was present in `trust_launch.json` |
| Harness `final_disposition` | `NO_GO` | **Unchanged** (mechanical; not upgraded) |

## What was wrong

`trust_launch.json` stored bounded but still-raw host **stdout** from `grok --output-format json`.
That JSON included private-reasoning field **`thought`**, plus plaintext **`sessionId`** and **`requestId`**.

The v2 report privacy scanner did not detect nested/escaped `thought` keys, so it reported
`raw_thoughts_stored=false` and `complete=true` while public evidence retained private content.

## What we did

1. Replaced `trust_launch.json` with a **closed allowlist** capture (no raw stdout; no `thought`; IDs hashed).
2. Published this errata (`PRIVACY_ERRATA.json` + this file).
3. Product code now: sanitizes trust captures; rejects evidence writes with private reasoning keys;
   walks nested/escaped JSON for `thought` and related keys.

## Quota contamination

The same campaign observed **Grok Build 402 / balance exhausted** on the host CLI.
`session/prompt: Internal error` and several hook-failure **INCONCLUSIVE** outcomes must be treated as
**quota-contaminated** until a fresh run with available balance re-probes ACP. Do **not** treat this
run alone as proof of a Reinframe SessionPrompt request-shape bug.

## Non-claims (still)

- No live v2 GO
- No explicit ACK / CapPause / Level 2 / ranking
- No live #108 E2E
