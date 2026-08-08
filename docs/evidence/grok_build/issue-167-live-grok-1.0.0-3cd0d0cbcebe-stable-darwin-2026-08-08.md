# Grok Build live control evidence (#167)

- **Disposition:** `GO`
- **Grok version:** grok-1.0.0-3cd0d0cbcebe-stable
- **OS:** darwin
- **Strongest ACK proven:** `session_visible`
- **Auth.json read:** no
- **Explicit ACK claimed:** no

## Scenarios

| ID | Status | Detail |
|----|--------|--------|
| ACP-OPTIONAL-001 | INCONCLUSIVE | loadSession negotiated but failed: grok acp: session/load: Invalid params |
| ADVICE-DEDUP-001 | PASS | second delivery same InterventionID accepted by host transport; durable suppress… |
| HOOK-FAIL-001 | PASS | marker written under broken hook (timeout) → host fail-open proven; exit_err=s… |
| HOOK-FAIL-004 | PASS | marker written under broken hook (oversized) → host fail-open proven; exit_err… |
| TRUST-RESTORE-001 | PASS | restored command doctor_ok=true |
| HOOK-DENY-001 | PASS | hook invoked and deny-side-effect absent; tool=run_terminal_command; exit_err=<n… |
| HOOK-FAIL-003 | PASS | marker written under broken hook (malformed) → host fail-open proven; exit_err… |
| HOOK-UNINSTALL-001 | PASS | reinframe-owned hooks removed |
| TRUST-001 | PASS | launched with --trust; doctor_ok=true |
| TRUST-STALE-001 | INCONCLUSIVE | command reinstalled; TrustStale=false msgs=valid reinframe grok hooks foundation |
| ACP-INIT-001 | PASS | protocolVersion=1 post_level=0 load=true cancel=false auth=[cached_token grok.co… |
| CHALLENGE-001 | PASS | challenge text transported with ChallengeID preserved; #131 remains authoritativ… |
| HOOK-ALLOW-001 | PASS | exit_err=<nil> stdout_bytes=49600 stderr= |
| ACP-CLEANUP-001 | PASS | Close completed; owned PID 68494 not alive |
| ACP-SESSION-001 | PASS | session/new+prompt+correlated update |
| HOOK-FAIL-002 | PASS | marker written under broken hook (crash_nonzero_non_deny) → host fail-open pro… |
| HOOK-MAP-001 | PASS | observed_tools=run_terminal_command |
| ACP-AUTH-001 | PASS | delegated auth methodId=cached_token headless=true no token field |

## Limitations

- TRUST-STALE-001 INCONCLUSIVE

## Non-claims

- No Level 2 / CapPause from hooks alone
- No cross-host ranking
- No credential material intentionally stored
