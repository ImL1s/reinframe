# ADR & Threat Model: Delegated ChatGPT Authentication & Credential-Owner Boundary

- **Status**: Decided & Approved
- **Date**: 2026-08-15
- **Deciders**: Security & Protocol Team, Architecture Team
- **Technical Story**: Issue #183 (P0 Security/Codex)
- **Target Surface**: `pkg/protocol/auth.go`, `pkg/config/schema.go`, `pkg/codexruntime/`

---

## 1. Context and Problem Statement

Reinframe supervises AI agent harnesses (including OpenAI Codex CLI) as an out-of-process, deterministic supervisor. When interacting with OpenAI Codex, developers authenticate via their ChatGPT subscription (OAuth/Web login) or explicit developer API keys.

Operating a supervisor in proximity to agent harnesses introduces significant security, credential-theft, and data-governance risks:
1. **Credential Boundary Leakage**: If Reinframe attempts to read or manage Codex tokens (e.g. parsing `~/.codex/auth.json`), Reinframe becomes a high-value credential broker, susceptible to exfiltration via malicious repository scripts, supply chain attacks, or untrusted project configuration overrides.
2. **Data & Billing Policy Confusion**: Consumer ChatGPT subscriptions and developer API keys operate under fundamentally different billing models, rate limits, and data retention/training policies. Silent fallback between subscription and API key modes violates organizational data governance and unexpected cloud billing.
3. **Cache Partition Contamination**: If supervisor assessment caches do not isolate user identity, authentication mode, and target scope, sensitive reasoning artifacts or cached decisions could be reused across scopes or after session revocation.
4. **Untrusted Project Exploitation**: Repositories opened by the developer might include `.reinframe.yaml` / `.reinframe.json` configuration files attempting to redirect binaries or hijack credential owners.

---

## 2. Delegated Credential Ownership

### Decision
Reinframe formally establishes a closed, versioned credential-owner boundary where credential ownership is **delegated entirely to the child `codex` process** (`CredentialOwnerCodexProcess = "codex_process"`).

```
+-------------------------------------------------------------------------+
|                              REINFRAME                                  |
|                                                                         |
|  +-----------------------+     Safe Argv Probe     +-----------------+  |
|  |    RuntimeService     | ----------------------> |  CLI Executable |  |
|  |                       | <---------------------- |  (codex status) |  |
|  +-----------------------+   (Bounded Hashes Only) +-----------------+  |
|              |                                              |           |
|              v                                              v           |
|  +-----------------------+                         +-----------------+  |
|  |  RuntimeAuthSnapshot  |                         |  Codex Process  |  |
|  |   (Zero Raw Secrets)  |                         |   Local Store   |  |
|  +-----------------------+                         | (~/.codex/...)  |  |
|                                                    +-----------------+  |
+-------------------------------------------------------------------------+
       |                                                      |
       | Reinframe Domain                                     | Child Process Domain
   [NO READ/WRITE] <==========================================+
```

### Normative Rules
1. **Zero Credential Custody**: Reinframe MUST NOT store, decrypt, persist, or refresh Codex OAuth access tokens, refresh tokens, session cookies, or API keys for the child process.
2. **Process-Local Authentication**: Authentication flows (`codex login`) remain solely between the developer, the Codex executable, and OpenAI authentication endpoints.
3. **Supervisor Gatekeeper Role**: Reinframe queries read-only status projection from the executable using safe argv invocations and derives bounded, non-reversible cryptographic hashes (`ScopeHash`, `AuthGenerationHash`).

---

## 3. Prohibited Credential Sources

### Normative Rules
1. **Prohibited Path Access**: Reinframe code MUST NEVER open, read, stat, or parse `~/.codex/auth.json`, `%USERPROFILE%\.codex\auth.json`, or any child token cache path.
2. **Prohibited Token Extraction**: Reinframe MUST NOT scrape child process memory, inspect OS keychain records, or intercept IPC streams to extract bearer tokens.
3. **No Private API Reverse Engineering**: Reinframe MUST NOT emulate internal OpenAI authentication endpoints or inject counterfeit authentication headers.
4. **Codebase Sentinels**: Automated sentinels (`codexruntime.AssertNoProhibitedPathAccess` and static analyzers) enforce that prohibited paths and token patterns are structurally impossible to access.

---

## 4. Subscription vs API Billing & Data Policy Boundary

### Problem
OpenAI applies different terms of service, data logging agreements, and billing meters to ChatGPT subscription accounts versus direct OpenAI Platform API keys:
- **ChatGPT Subscription**: Subject to consumer/team ChatGPT terms, zero-data-retention options depending on subscription tier, flat monthly billing.
- **API Key**: Metered token-based billing, standard API data retention policies.

### Normative Rules
1. **Strict Isolation**: `RuntimeAuthModeChatGPTSubscription` and `RuntimeAuthModeAPIKey` are distinct, closed enum values. A subscription authentication state CANNOT satisfy an `api_key` requirement, and an API key state CANNOT satisfy a `chatgpt_subscription` requirement.
2. **Preflight Verification**: `RuntimeService.EnsureReady()` verifies that actual projected auth mode matches configured `required_auth`. If mismatched, execution MUST fail closed before model selection or task intake.
3. **No Silent Fallback**: When configured for `chatgpt_subscription`, Reinframe MUST NOT silently fall back to developer environment API keys (`OPENAI_API_KEY`) if the subscription is unauthenticated or expired.

---

## 5. Logout, Expiry, and Scope Change Behavior

### Cache Partitioning Contract
Reinframe maintains deterministic cache partitioning via `CachePartitionManager`:

$$\text{PartitionKey} = \text{profile} \parallel \text{owner} \parallel \text{mode} \parallel \text{ScopeHash} \parallel \text{AuthGenerationHash}$$

```
                           +------------------------+
                           |  RuntimeAuthSnapshot   |
                           +------------------------+
                                       |
                   +-------------------+-------------------+
                   |                                       |
                   v                                       v
         [ScopeHash Changed]                    [AuthGenHash Changed]
          (Files modified,                        (Logout, re-login,
         target scope adjusted)                   token rotated/expired)
                   |                                       |
                   +-------------------+-------------------+
                                       |
                                       v
                         +--------------------------+
                         |  Invalidate Partition    |
                         |  Key & Clear Assessment  |
                         +--------------------------+
```

### Invalidation Triggers
1. **Scope Alteration**: Any change to target repositories, directories, or files recomputes `ScopeHash = SHA256(sorted_scopes)` and invalidates the cache partition (`ReasonScopeChanged`).
2. **Auth Generation Rotation**: Re-login, account switching, or session renewal recomputes `AuthGenerationHash = SHA256(profile : generationSeed)` and invalidates the cache partition (`ReasonAuthGenChanged`).
3. **Unauthenticated / Expired State**: When status shifts away from `authenticated`, active partition keys are immediately destroyed (`ReasonUnauthenticatedState`).

---

## 6. Redaction, Retention, and Serialization Rules

### Normative Rules
1. **Schema Redaction**: All protocol structures mark sensitive fields with protocol tags (`redact:"none"`, `redact:"sensitive"`, `redact:"sanitize"`, `redact:"path"`).
2. **Bounded Non-Secret Snapshots**: `RuntimeAuthSnapshot` contains only enum state, profile identifier, version string, and 64-character SHA-256 hashes.
3. **Diagnostics & Logging Safety**:
   - `RuntimeAuthSnapshot.Format`, `String()`, `GoString()` implement secret-safe formatting.
   - `MarshalJSON` validates and redacts any prohibited strings.
   - Configuration decoders (`UnmarshalJSONDocument`) reject JSON payloads containing prohibited keys (`oauth_token`, `refresh_token`, `access_token`, `session_token`, `api_key` under codex runtime, `cookie`, `client_secret`).
4. **Zero Secret Retention**: Neither in-memory structures nor SQLite event databases retain raw credentials.

---

## 7. Failure Semantics & Untrusted Configuration Defense

### Failure Matrix

| Condition | Failure Code | Behavior |
| :--- | :--- | :--- |
| **Unauthenticated** | `ErrOperatorRequired` | Halts execution; prompts operator for interactive login if allowed. Zero API fallback. |
| **Expired Session** | `ErrSessionExpired` | Halts turn progression immediately. Prevents executing tools without valid auth. |
| **Auth Mode Mismatch** | `ErrAuthModeMismatchCfg` | Fails preflight startup before model selection or task contract initialization. |
| **Runtime Unavailable** | `ErrRuntimeUnavailable` | Fails closed; requires operational runtime environment. |
| **Disabled Runtime** | `ErrRuntimeDisabled` | Fails closed if invoked while disabled. |

### Untrusted Project Override Defense
Project-level configuration files (`.reinframe.yaml` / `.reinframe.json` in workspace roots) are treated as untrusted:
- **Executable Pinned**: Project configs CANNOT override `codex_runtime.executable`.
- **Credential Owner Pinned**: Project configs CANNOT override `codex_runtime.credential_owner`.
- **Integrity Digest Pinned**: Project configs CANNOT override `codex_runtime.binary_sha256`.
- **Isolation Enforced**: Project configs CANNOT disable `workspace.enforce_isolation`.
- **No Secret Injection**: Project configs CANNOT inject raw secret strings into placeholders.

Any attempted override of these surfaces fails immediately via `config.ValidateUntrustedProjectOverride()`.

---

## 8. Threat Model Summary

| Threat | Attack Vector | Mitigation in Reinframe |
| :--- | :--- | :--- |
| **T-1: Credential Exfiltration** | Malicious workspace script reads `auth.json` via Reinframe APIs. | Reinframe never opens, reads, or stores `auth.json`. Sentinel checks prevent path access. |
| **T-2: Binary Hijacking** | Repo config sets `executable: "/tmp/evil"` to intercept commands. | `ValidateUntrustedProjectOverride` blocks executable overrides from project configs. Safe argv execution prevents shell interpolation. |
| **T-3: Cross-Tenant Cache Poisoning** | Stale cache reused after user logs out or switches accounts. | `AuthGenerationHash` and `ScopeHash` dynamically partition cache keys. Logout/expiry clears partition. |
| **T-4: Policy / Billing Confusion** | Silent fallback to API key when subscription expires. | Strict `required_auth` enforcement and fail-closed state transitions. |
| **T-5: Log / Snapshot Leaks** | Tokens logged in error messages or database snapshots. | Secret-safe formatters, struct validators, and prohibited key filters in JSON deserialization. |
