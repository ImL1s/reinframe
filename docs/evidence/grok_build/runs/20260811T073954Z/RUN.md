# Grok Build live v2 qualification run

- **RUN_ID**: `20260811T073954Z`
- **Starting main / binary reinframe_commit**: `694e09d2a76062544b7a0e244ace4431d5d34818`
- **reinframe_dirty**: `False`
- **reinframe_commit_src**: `ldflags`
- **Grok version**: `grok 1.0.0 (3cd0d0cbcebe)`
- **goos/goarch**: `darwin` / `arm64`
- **final_disposition (harness)**: **NO_GO**
- **limitations**: ['HOOK-DENY-001 FAIL']
- **scenario counts**: {'PASS': 8, 'INCONCLUSIVE': 9, 'FAIL': 1, 'NOT_RUN': 1}
- **strongest_proven ACK (as recorded)**: `session_visible` (`explicit_claimed=false`) — **legacy temporal correlation only**; see `LEGACY_CORRELATION.md` (not source-correlated under post-P1-A / #199 rules)
- **privacy.complete**: `True` (`raw_thoughts_stored=false`; `trust_launch` closed allowlist, no raw `thought`)
- **caps_digest**: load negotiated; `session/load` optional path remains INCONCLUSIVE (`Invalid params`)

## Honesty

This directory is **immutable mechanical evidence** from `cmd/groklive all --live` on main tip `694e09d…` (post-#225 privacy exact-key fix).
Scenario statuses and disposition were **not** hand-edited to a stronger result.
Recorded `session_visible` must **not** be cited as source-correlated product evidence; see `LEGACY_CORRELATION.md`.

### What improved vs `20260810T170640Z` (quota-contaminated NO_GO)

- No host **402 / balance exhausted** markers in this campaign log.
- **ACP-SESSION-001 PASS** as recorded (session-matched update; **legacy** temporal correlation — not #199 source-correlated).
- **CHALLENGE-001 PASS**.
- **trust_launch** is closed-allowlist only (hashed session/request IDs; `stdout_raw=false`; no `thought` field).

### What still blocks GO / LIMITED_GO

- **HOOK-DENY-001 FAIL** — host did not enforce deny (denied side-effect file still written). Consistent with Grok hooks fail-open; not upgraded to product “deny proven”.
- Multiple HOOK-* **INCONCLUSIVE** (invocation log / broken-hook proof incomplete).
- **ACP-OPTIONAL-001** `session/load: Invalid params` remains optional/INCONCLUSIVE (not first-order NO_GO cause).
- Public #168 ranking disposition remains **MORE_DATA** (Grok-only lane; no cross-host ranking).

### Non-claims

- No Level 2 / CapPause / explicit ACK.
- No live #108 E2E, exactly-once, multi-host ranking.
- Historical v1 and prior v2 run `20260810T170640Z` are **not** overwritten.

Report JSON/MD regenerated once with a correctly ldflags-bound `groklive` binary (`-X main.reinframeCommit` / `-X main.reinframeDirty=false`) after the live phases completed; scenarios themselves are the live harness output.


## Privacy errata (session identity)

Post-merge correction: `target_session_id` values in `scenarios.json` and the formal report were **plaintext host session UUIDs**. They have been replaced with **SHA-256 hex** digests of the original UUID. Mechanical disposition (**NO_GO** / HOOK-DENY-001) is unchanged. Future harness builds hash session IDs before write (`sha256Hex(sid)`).
