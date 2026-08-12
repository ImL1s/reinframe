# Grok Build live v2 qualification run

- **RUN_ID**: `20260811T130935Z`
- **live_binary_commit / starting_main_sha**: `66ca8ae5503b9e6103078f76594f71a91e0a4970`
- **report_generator_commit**: `a0627bde788c8bdd07c260aede1ddff84023e825` (formal re-eval under executable-binding gate)
- **derived**: `True`
- **Grok version**: `grok 1.0.0 (3cd0d0cbcebe)`
- **goos/goarch**: `darwin` / `arm64` (live campaign; not re-run)
- **final_disposition (harness, re-eval)**: **NO_GO**
- **re-eval reason**: `live_grok_executable.json` absent — new gate forbids GO/LIMITED_GO without content-bound Grok CLI identity across phases (Pro R6/R7). Scenarios and transport ACK honesty retained; **not** a fresh live campaign.
- **limitations**: live_grok_executable missing; ACP-SESSION-001 INCONCLUSIVE; TRUST-STALE-001 INCONCLUSIVE; ADVICE-DEDUP-001 INCONCLUSIVE; STATIC-PERM-001 NOT_RUN; ACP-OPTIONAL-001 INCONCLUSIVE
- **strongest_proven ACK**: `transport` (`source_correlated=false`, `explicit_claimed=false`)
- **ACP-SESSION-001**: status=`INCONCLUSIVE` ack=`transport` session_correlated=`false`
- **Formal report (authoritative under current gate)**: `issue-167-live-v2-grok-1.0.0-3cd0d0cbcebe-darwin-2026-08-12.json`
- **Historical formal (pre executable-binding gate; LIMITED_GO as recorded)**: `issue-167-live-v2-grok-1.0.0-3cd0d0cbcebe-darwin-2026-08-11.json` — superseded for qualification

## Honesty

Live scenarios from binary `66ca8ae…` remain transport-only; no source-correlated session_visible.
Post-R6 code requires `live_grok_executable.json`; this pin predates that artifact → mechanical **NO_GO** on re-eval.
Public #168 remains **MORE_DATA**. A future clean live `groklive all` can re-qualify with content-bound Grok executable.

### Non-claims
- No Level 2 / CapPause / explicit product ACK
- No source-correlated session_visible
- No false LIMITED_GO under the executable-binding gate
