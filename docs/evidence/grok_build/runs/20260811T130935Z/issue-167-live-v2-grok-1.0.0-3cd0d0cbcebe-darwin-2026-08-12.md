# Grok Build live control evidence (#167)

- **Disposition:** `NO_GO`
- **Grok version:** grok-1.0.0-3cd0d0cbcebe
- **OS:** darwin
- **Strongest ACK proven:** `transport`
- **Auth.json read:** no
- **Explicit ACK claimed:** no

## Scenarios

| ID | Status | Detail |
|----|--------|--------|
| ACP-AUTH-001 | PASS | delegated auth methodId=cached_token headless=true no token field |
| ADVICE-DEDUP-001 | INCONCLUSIVE | second delivery accepted at transport; durable/business InterventionID suppressi… |
| CHALLENGE-001 | PASS | challenge text transported with ChallengeID preserved; #131 remains authoritativ… |
| HOOK-ALLOW-001 | PASS | exit_err=<nil> stdout_bytes=49679 stderr= |
| TRUST-RESTORE-001 | PASS | restored command doctor_ok=true |
| ACP-SESSION-001 | INCONCLUSIVE | prompt transport OK; post-prompt session-matched update lacks request/interventi… |
| HOOK-FAIL-001 | PASS | broken hook invoked and marker written (timeout); exit_err=<nil> stdout_bytes=53… |
| HOOK-FAIL-004 | PASS | broken hook invoked and marker written (oversized); exit_err=<nil> stdout_bytes=… |
| HOOK-UNINSTALL-001 | PASS | reinframe-owned hooks removed |
| STATIC-PERM-001 | NOT_RUN | optional Reinframe-owned static permission fragment not exercised |
| ACP-CLEANUP-001 | PASS | Close completed; owned PID 98810 not alive |
| ACP-INIT-001 | PASS | protocolVersion=1 post_level=0 load=true cancel=false auth=[cached_token grok.co… |
| HOOK-FAIL-003 | PASS | broken hook invoked and marker written (malformed); exit_err=<nil> stdout_bytes=… |
| HOOK-MAP-001 | PASS | observed_tools=run_terminal_command |
| ACP-OPTIONAL-001 | INCONCLUSIVE | loadSession negotiated but failed: grok acp: session/load: Invalid params |
| HOOK-DENY-001 | PASS | direct deny response/exit observed for tool=run_terminal_command; exit_err=<nil>… |
| HOOK-FAIL-002 | PASS | broken hook invoked and marker written (crash_nonzero_non_deny); exit_err=<nil> … |
| TRUST-001 | PASS | launched with --trust; doctor_ok=true |
| TRUST-STALE-001 | INCONCLUSIVE | command reinstalled; TrustStale=false msgs=valid reinframe grok hooks foundation |

## Limitations

- ACP-SESSION-001 INCONCLUSIVE
- TRUST-STALE-001 INCONCLUSIVE
- ADVICE-DEDUP-001 INCONCLUSIVE
- STATIC-PERM-001 NOT_RUN
- ACP-OPTIONAL-001 INCONCLUSIVE
- live_grok_executable.json missing
- invalid/missing live_grok_executable forbids GO/LIMITED_GO
- ACP-SESSION-001 INCONCLUSIVE
- TRUST-STALE-001 INCONCLUSIVE
- ADVICE-DEDUP-001 INCONCLUSIVE
- STATIC-PERM-001 NOT_RUN
- ACP-OPTIONAL-001 INCONCLUSIVE
- final_disposition mismatches evaluateDisposition want=LIMITED_GO got=NO_GO

## Non-claims

- No Level 2 / CapPause from hooks alone
- No cross-host ranking
- No credential material intentionally stored
