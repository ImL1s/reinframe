# Grok Build live v2 qualification run

- **RUN_ID**: `20260811T084150Z`
- **Binary reinframe_commit / starting_main_sha (binary-bound)**: `af1b3cc93912489399317aa8491d1adaf41ced00`
- **reinframe_dirty**: `False`
- **reinframe_commit_src**: `ldflags`
- **Grok version**: `grok 1.0.0 (3cd0d0cbcebe)`
- **goos/goarch**: `darwin` / `arm64`
- **final_disposition (harness)**: **LIMITED_GO**
- **limitations**: ['TRUST-STALE-001 INCONCLUSIVE', 'ADVICE-DEDUP-001 INCONCLUSIVE', 'STATIC-PERM-001 NOT_RUN', 'ACP-OPTIONAL-001 INCONCLUSIVE']
- **scenario counts**: PASS 15 · FAIL 0 · INCONCLUSIVE 3 · NOT_RUN 1
- **strongest_proven ACK**: `session_visible` (source-correlated; `explicit_claimed=False`)
- **privacy.complete**: `True` (`raw_thoughts_stored=False`; closed-allowlist trust_launch)

## Honesty

Immutable mechanical evidence from `cmd/groklive all --live` on binary-bound main tip `af1b3cc93912489399317aa8491d1adaf41ced00` (ldflags, dirty=false).
`starting_main_sha` in the formal report equals `reinframe_commit` (binary-bound; not a hardcoded historical SHA).
Scenario statuses and disposition were **not** hand-edited.

### Non-claims
- No Level 2 / CapPause / explicit product ACK
- No cross-host ranking; public #168 remains **MORE_DATA** until matched Codex/Claude live
- No #108 live E2E / exactly-once
- Prior runs `20260810T170640Z` and `20260811T073954Z` are **not** overwritten
- Capability `honesty_note` text describes harness path only — it is **not** disposition GO

## Provenance errata

See `PROVENANCE_ERRATA.md` for post-run honesty_note / starting_main_sha reconciliation. Mechanical **LIMITED_GO** is unchanged.
