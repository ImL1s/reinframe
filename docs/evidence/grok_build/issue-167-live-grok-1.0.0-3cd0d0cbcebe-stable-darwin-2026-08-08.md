# Grok Build live control evidence (#167)

- **Reinframe commit:** `eac8480d2a5b06aa9cee2e017d50c44eab6b6b85`
- **Starting main:** `62889cb59916fa2dd412a2c5d511c7ac7c4b23c6`

- **Disposition:** `GO`
- **Grok version:** grok-1.0.0-3cd0d0cbcebe-stable
- **OS:** darwin
- **Strongest ACK proven:** `session_visible`
- **Auth.json read:** no
- **Explicit ACK claimed:** no

## Scenarios

| ID | Status | Detail |
|----|--------|--------|
| ACP-CLEANUP-001 | PASS | Close completed; owned PID 68494 not alive |
| ACP-OPTIONAL-001 | INCONCLUSIVE | loadSession negotiated but failed: grok acp: session/load: Invalid params |
| HOOK-DENY-001 | PASS | hook invoked and deny-side-effect absent; tool=run_terminal_command; exit_err=<n… |
| ACP-INIT-001 | PASS | protocolVersion=1 post_level=0 load=true cancel=false auth=[cached_token grok.co… |
| ACP-SESSION-001 | PASS | session/new+prompt+correlated update |
| ADVICE-DEDUP-001 | PASS | second delivery same InterventionID accepted by host transport; durable suppress… |
| CHALLENGE-001 | PASS | challenge text transported with ChallengeID preserved; #131 remains authoritativ… |
| HOOK-ALLOW-001 | PASS | exit_err=<nil> stdout_bytes=49600 stderr= |
| HOOK-FAIL-001 | PASS | marker written under broken hook (timeout) → host fail-open proven; exit_err=s… |
| HOOK-UNINSTALL-001 | PASS | reinframe-owned hooks removed |
| HOOK-FAIL-002 | PASS | marker written under broken hook (crash_nonzero_non_deny) → host fail-open pro… |
| HOOK-FAIL-004 | PASS | marker written under broken hook (oversized) → host fail-open proven; exit_err… |
| TRUST-001 | PASS | launched with --trust; doctor_ok=true |
| TRUST-RESTORE-001 | PASS | restored command doctor_ok=true |
| TRUST-STALE-001 | INCONCLUSIVE | command reinstalled; TrustStale=false msgs=valid reinframe grok hooks foundation |
| ACP-AUTH-001 | PASS | delegated auth methodId=cached_token headless=true no token field |
| HOOK-FAIL-003 | PASS | marker written under broken hook (malformed) → host fail-open proven; exit_err… |
| HOOK-MAP-001 | PASS | observed_tools=run_terminal_command |

## Non-claims

- No Level 2 / CapPause from hooks alone
- No cross-host ranking
- No credential material intentionally stored
