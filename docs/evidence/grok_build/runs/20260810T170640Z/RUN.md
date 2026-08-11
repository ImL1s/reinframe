# Grok Build live v2 qualification run

- **RUN_ID**: `20260810T170640Z`
- **Starting main**: `3a218bdf99b1d54bf9d2a603e8d8c54cdeee27fe`
- **Binary reinframe_commit**: `3a218bdf99b1d54bf9d2a603e8d8c54cdeee27fe`
- **reinframe_dirty**: `False`
- **reinframe_commit_src**: `ldflags`
- **Grok version**: `grok 1.0.0 (3cd0d0cbcebe)`
- **goos/goarch**: `darwin` / `arm64`
- **final_disposition (harness)**: **NO_GO**
- **limitations**: ['ACP-SESSION-001 FAIL']
- **scenario counts**: {'PASS': 9, 'INCONCLUSIVE': 7, 'FAIL': 2, 'NOT_RUN': 1}
- **strongest_proven ACK**: `transport`
- **explicit_claimed**: `False`
- **privacy.complete**: `True`
- **caps_digest**: `load=true pause=false cancel=false resume=false tool=false diff=false`

## Honesty

This directory is **immutable mechanical evidence** from `cmd/groklive all --live` on the stated main tip.
Scenario statuses and disposition were **not** hand-edited.
Public product disposition for ranking (#168) remains **MORE_DATA** (Grok-only lane; no cross-host ranking).
No live #108 E2E, exactly-once, CapPause/Level 2, or multi-host ranking is claimed from this run.

Historical v1 evidence remains under `docs/evidence/grok_build/issue-167-live-grok-1.0.0-*` and `HISTORICAL_v1.md`.


## Privacy errata (post-merge correction)

See `PRIVACY_ERRATA.md` / `PRIVACY_ERRATA.json`.

- Original `privacy.complete=true` / `raw_thoughts_stored=false` on the formal report was a **false negative**: `trust_launch.json` retained host `thought` inside escaped stdout.
- `trust_launch.json` has been replaced with a closed allowlist capture; **mechanical `NO_GO` is unchanged**.
- This run is **quota-contaminated** (Grok 402 balance exhausted observed mid-campaign); do not treat ACP Internal error alone as a proven Reinframe SessionPrompt shape bug without a clean re-run.
