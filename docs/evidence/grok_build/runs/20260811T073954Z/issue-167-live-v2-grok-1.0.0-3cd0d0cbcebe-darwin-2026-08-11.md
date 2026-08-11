# Grok Build live control evidence (#167)

- **Disposition:** `NO_GO`
- **Grok version:** grok-1.0.0-3cd0d0cbcebe
- **OS:** darwin
- **Strongest ACK proven:** `session_visible`
- **Auth.json read:** no
- **Explicit ACK claimed:** no

## Scenarios

| ID | Status | Detail |
|----|--------|--------|
| ACP-OPTIONAL-001 | INCONCLUSIVE | loadSession negotiated but failed: grok acp: session/load: Invalid params |
| ADVICE-DEDUP-001 | INCONCLUSIVE | second delivery accepted at transport; durable/business InterventionID suppressi… |
| TRUST-001 | PASS | launched with --trust; doctor_ok=true |
| TRUST-STALE-001 | INCONCLUSIVE | command reinstalled; TrustStale=false msgs=valid reinframe grok hooks foundation |
| HOOK-MAP-001 | INCONCLUSIVE | no tools observed in hook invocation log |
| ACP-INIT-001 | PASS | protocolVersion=1 post_level=0 load=true cancel=false auth=[cached_token grok.co… |
| CHALLENGE-001 | PASS | challenge text transported with ChallengeID preserved; #131 remains authoritativ… |
| HOOK-ALLOW-001 | INCONCLUSIVE | file written but hook invocation not proven: exit_err=<nil> stdout_bytes=49067 s… |
| HOOK-FAIL-001 | INCONCLUSIVE | marker written but broken-hook invocation not proven (may be untrusted skip); ex… |
| HOOK-UNINSTALL-001 | PASS | reinframe-owned hooks removed |
| HOOK-FAIL-002 | INCONCLUSIVE | marker written but broken-hook invocation not proven (may be untrusted skip); ex… |
| HOOK-FAIL-004 | INCONCLUSIVE | marker written but broken-hook invocation not proven (may be untrusted skip); ex… |
| TRUST-RESTORE-001 | PASS | restored command doctor_ok=true |
| ACP-CLEANUP-001 | PASS | Close completed; owned PID 34348 not alive |
| ACP-SESSION-001 | PASS | session/new+prompt+post-prompt session-matched update; source-correlated session… |
| HOOK-DENY-001 | FAIL | denied file exists — host did not enforce deny; exit_err=<nil> stdout_bytes=52… |
| HOOK-FAIL-003 | INCONCLUSIVE | marker written but broken-hook invocation not proven (may be untrusted skip); ex… |
| STATIC-PERM-001 | NOT_RUN | optional Reinframe-owned static permission fragment not exercised |
| ACP-AUTH-001 | PASS | delegated auth methodId=cached_token headless=true no token field |

## Limitations

- HOOK-DENY-001 FAIL

## Non-claims

- No Level 2 / CapPause from hooks alone
- No cross-host ranking
- No credential material intentionally stored
