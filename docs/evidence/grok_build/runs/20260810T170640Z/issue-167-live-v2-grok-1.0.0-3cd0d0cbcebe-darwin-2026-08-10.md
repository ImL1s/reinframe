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
| ACP-INIT-001 | PASS | protocolVersion=1 post_level=0 load=true cancel=false auth=[cached_token grok.co… |
| CHALLENGE-001 | FAIL | session/prompt challenge: grok acp: session/prompt: Internal error |
| HOOK-FAIL-002 | INCONCLUSIVE | broken hook (crash_nonzero_non_deny) installed but marker not written — cannot… |
| HOOK-FAIL-004 | INCONCLUSIVE | broken hook (oversized) installed but marker not written — cannot claim fail-o… |
| STATIC-PERM-001 | NOT_RUN | optional Reinframe-owned static permission fragment not exercised |
| ADVICE-DEDUP-001 | INCONCLUSIVE | second delivery error: grok acp: session/prompt: Internal error; not durable sup… |
| HOOK-ALLOW-001 | PASS | exit_err=<nil> stdout_bytes=49930 stderr= |
| HOOK-FAIL-001 | INCONCLUSIVE | broken hook (timeout) installed but marker not written — cannot claim fail-ope… |
| HOOK-MAP-001 | PASS | observed_tools=run_terminal_command |
| TRUST-RESTORE-001 | PASS | restored command doctor_ok=true |
| TRUST-STALE-001 | INCONCLUSIVE | command reinstalled; TrustStale=false msgs=valid reinframe grok hooks foundation |
| ACP-SESSION-001 | FAIL | session/prompt: grok acp: session/prompt: Internal error |
| HOOK-DENY-001 | PASS | direct deny response/exit observed for tool=run_terminal_command; exit_err=exit … |
| TRUST-001 | PASS | launched with --trust; doctor_ok=true |
| ACP-CLEANUP-001 | PASS | Close completed; owned PID 9004 not alive |
| ACP-OPTIONAL-001 | INCONCLUSIVE | loadSession negotiated but failed: grok acp: session/load: Invalid params |
| HOOK-FAIL-003 | INCONCLUSIVE | broken hook (malformed) installed but marker not written — cannot claim fail-o… |
| HOOK-UNINSTALL-001 | PASS | reinframe-owned hooks removed |

## Limitations

- ACP-SESSION-001 FAIL

## Non-claims

- No Level 2 / CapPause from hooks alone
- No cross-host ranking
- No credential material intentionally stored
