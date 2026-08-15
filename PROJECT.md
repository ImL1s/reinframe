# Reinframe: Fail-Closed External AI Agent Supervision Control Plane

## Architecture Overview

Reinframe provides an independent, fail-closed policy, evaluation, and supervision control plane architecture for autonomous and semi-autonomous coding agents. It provides pre-tool hook gating, prevents tunneling through structured challenge verification, isolates multi-tenant workspaces, and models multi-host interactions (with non-production / foundation boundaries documented in README).

```text
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Reinframe Supervision Core                        │
├────────────────────────────────┬──────────────────────────┬─────────────────┤
│        Adapter Layer           │      Policy & Control    │   State & Store │
├────────────────────────────────┼──────────────────────────┼─────────────────┤
│ • OpenAI Codex (Hooks / stdio) │ • Fast / Slow Policy     │ • SQLite WAL    │
│ • Claude Code (PreTool / API)  │ • DualLaneRouter         │ • Event Ledger  │
│ • Grok Build (Hooks / ACP)     │ • Challenge State Engine │ • Worktree Mgr  │
│ • FileActuator Advice Delivery │ • Multi-LLM Classifiers  │ • Cache Protect │
└────────────────────────────────┴──────────────────────────┴─────────────────┘
```

---

## Key Modules & Packages

| Package | Purpose & Invariants |
| :--- | :--- |
| **`pkg/protocol`** | Canonical JSON schemas, 25 closed capability flags, `RuntimeAuthSnapshot`, and `ModelCatalogSnapshot`. |
| **`pkg/state`** | SQLite WAL-backed event store with atomic in-flight gating, busy retries, and persistence invariant guarantees. |
| **`pkg/adapter`** | Host adapters for OpenAI Codex, Anthropic Claude Code, xAI Grok Build, ACP, and atomic FileActuators. |
| **`pkg/challenge`** | Host-neutral appealable challenge state machine with 16-byte cryptographically secure nonces and 1-shot retry lifecycles. |
| **`pkg/policy`** | Dual-lane policy router isolating subscription vs. API billing channels; zero cross-lane fallback under rate limits or auth failure. |
| **`pkg/classifier`** | Multi-provider capability classifier supporting OpenAI Responses, Anthropic Messages, Google Gemini, and xAI Responses. |
| **`pkg/codexruntime`** | Subprocess supervisor, Windows Job Object / Unix process group lifecycle, and multi-tenant cache partition manager. |
| **`pkg/workspace`** | Managed git worktrees with strict clean-state checks, transaction rollback, and filesystem isolation. |
| **`pkg/detector`** | Heuristic detectors for repeated failures, verification churn, tool budgets, and hypothesis loops. |
| **`pkg/supervisor`** | Central orchestrator integrating intake mappers, detectors, policy router, and actuation pipelines. |
| **`pkg/evaluation`** | Benchmark harnesses, bypass resistance runners (20/20 attack vectors), and tri-host evaluation synthesizers. |
| **`cmd/*`** | Synthetic qualification and demonstration harnesses: `cmd/codexlive`, `cmd/claudelive`, `cmd/sparklive`, `cmd/groklive`, `cmd/streetwire`. |

---

## Security & Privacy Invariants

1. **Zero Delegated Credential Leakage**: Reinframe strictly never reads, logs, caches, or extracts host-owned credentials (`~/.codex/auth.json`, bearer headers, session cookies). Direct classifier provider API keys are handled in-process.
2. **Domain-Separated Deterministic Hashes**: Scope and auth generation identifiers are derived using one-way domain-separated SHA-256 digests (`ComputeScopeHash`, `ComputeAuthGenerationHash`).
3. **Fail-Closed Dual-Lane Isolation**: `chatgpt_subscription_quota` (Lane A) and `openai_api_tokens` (Lane B) never fall back across boundaries.
4. **OS-Level Process Tree Cleanup**: Guaranteed child process tree termination via Windows Job Objects (`KILL_ON_JOB_CLOSE`) and Unix process groups (`Setpgid`).
5. **Deterministic Bypass Resistance**: 100% rejection rate against nonce tampering, token replay, second retries, and scope expansions.

---

## Quality & Test Status

- **Unit & Integration Tests**: 100% PASS across all 18 packages (`go test -count=1 ./...`).
- **Static Analysis**: 0 warnings on `go vet ./...` and `golangci-lint`.
- **Cross-Platform Matrix**: Continuously tested across Linux (Ubuntu), macOS, and Windows runners.
