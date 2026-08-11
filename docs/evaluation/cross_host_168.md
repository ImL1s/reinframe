# Cross-host evaluation framework (#168)

## Lanes

| Lane | Description |
|------|-------------|
| `cross_host_eval_fake` | Deterministic fake host adapters only (CI; no network/credentials/transcripts). |
| `cross_host_eval_partial_live` | Fake fixtures **plus** one live Grok Build pin from #167. |

## Disposition

**MORE-DATA** (unchanged after single live host)

- Fake Codex hooks / Grok hooks / Grok ACP / Claude PreTool fixtures remain.
- **Historical Grok live pin present** after #167 live run (public MORE_DATA; #199 v2 gates closed) (`grok 1.0.0` darwin, hooks allow/deny/fail-open, ACP `session_visible`).
- Matched live lanes still missing: **#164 Codex**, **#120 Claude**.
- **No host/model ranking**; tunneling scores stay zero.
- ACK layers kept separate; never invent explicit ACK.
- Host fail-open recorded for Grok hooks (live + fake).

## Live pin (Grok #167)

- Historical v1 evidence: `docs/evidence/grok_build/issue-167-live-grok-1.0.0-3cd0d0cbcebe-stable-darwin-2026-08-08.json`
- Clean-quota re-run (main `694e09d…` post-#225, run `20260811T073954Z`):  
  `docs/evidence/grok_build/runs/20260811T073954Z/`  
  - Grok profile: `grok 1.0.0 (3cd0d0cbcebe)` darwin/arm64  
  - Harness disposition: **NO_GO** (mechanical; primary limitation `HOOK-DENY-001 FAIL` — host deny not enforced / fail-open)  
  - Not quota-contaminated (no 402 / balance exhausted in campaign log)  
  - **ACP-SESSION-001 PASS** with source-correlated **session_visible**; **CHALLENGE-001 PASS**  
  - Privacy: `privacy.complete=true`, `raw_thoughts_stored=false`; `trust_launch` closed allowlist (hashed IDs; no raw `thought`)  
  - Scenario counts: PASS 8 · FAIL 1 · INCONCLUSIVE 9 · NOT_RUN 1  
  - Strongest proven ACK: **session_visible** (`explicit_claimed=false`)  
  - Optional residual: `ACP-OPTIONAL-001` `session/load: Invalid params` (INCONCLUSIVE; not first-order NO_GO)  
  - Ranking still blocked by matched-host gaps: **#164 Codex**, **#120 Claude**
- Prior v2 campaign (main `3a218bd…`, run `20260810T170640Z`):  
  `docs/evidence/grok_build/runs/20260810T170640Z/`  
  - Grok profile: `grok 1.0.0 (3cd0d0cbcebe)` darwin/arm64  
  - Harness disposition: **NO_GO** (mechanical; primary limitation `ACP-SESSION-001 FAIL` — `session/prompt` Internal error; `CHALLENGE-001` also FAIL)  
  - Classification: **quota-contaminated NO_GO** (host CLI also reported **402 usage balance exhausted** mid-run; do not treat Internal error alone as proven ACP request-shape failure without a clean re-probe)  
  - Privacy: original formal report `privacy.complete=true` / `raw_thoughts_stored=false` was a **false negative** (raw Grok `thought` in `trust_launch` stdout); see `PRIVACY_ERRATA.md` — disposition not upgraded  
  - Scenario counts: PASS 9 · FAIL 2 · INCONCLUSIVE 7 · NOT_RUN 1  
  - Strongest proven ACK: **transport** only (`explicit_claimed=false`; not source-correlated session_visible)  
  - Remaining matched-host gaps for ranking: **#164 Codex**, **#120 Claude** (and Grok balance + session/prompt reliability for GO)
- Sample size: n=1 OS (darwin/arm64) for both pins
- API: `evaluation.AttachLiveGrok167Lane` / `DefaultLiveGrok167Pin` (historical pin path)

## Run

```bash
go test ./pkg/evaluation/ -count=1 -run 'CrossHostEvalFake|AttachLiveGrok'
```
