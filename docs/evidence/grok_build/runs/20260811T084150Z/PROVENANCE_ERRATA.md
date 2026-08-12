# Provenance / honesty-note errata (post-run)

**RUN_ID:** `20260811T084150Z`  
**Live binary reinframe_commit:** `af1b3cc93912489399317aa8491d1adaf41ced00`  
**Mechanical disposition:** **LIMITED_GO** (unchanged)

## What happened

The live campaign ran on binary `af1b3cc…`. At that tip:

1. `cmd/groklive/report.go` still hardcoded `starting_main_sha` to historical `62889cb…`.
2. Adapter capability `honesty_note` templates still contained unconditional “evidence GO” wording.

Scenario statuses, counts, ACK layers, and privacy capture were mechanical live outputs and were **not** hand-upgraded.

## Correction (documented, not disposition change)

1. Product templates fixed so future runs cannot emit disposition-GO honesty notes; `starting_main_sha` is binary-bound `reinframe_commit`.
2. Formal report regenerated via `groklive report` with ldflags-bound binary (`reinframeCommit=af1b3cc…`, `dirty=false`) so provenance/honesty_note match corrected generators while scenarios remain the live harness map.
3. `acp_manifest.json` honesty notes neutralized to the same disposition-neutral text.

## Non-claims

- This errata does **not** change **LIMITED_GO** → GO.
- This errata does **not** invent ranking, explicit ACK, CapPause, or Level 2.
