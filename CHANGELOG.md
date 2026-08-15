# Changelog

All notable changes to Reinframe will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Planned for v0.1.1: Official App Server Protocol Conformance & Claude Durable Challenge CLI

#### Fixed & Conformance
- **Codex App Server Official Schema Conformance**:
  - `thread/start` & `thread/resume`: Added support for numeric timestamps (`createdAt` Unix ms/s) and object statuses (`{"type": "active"}`).
  - `turn/start`: Decoupled `Turn` response parsing from `turn.model` and `turn.threadId` fields (inheriting proven thread model identity), matching official OpenAPI specs.
  - Item Approval Path Resolution: Introduced client item state cache `(threadID, turnID, itemID) -> (path, command)` from `item/started` events so `item/fileChange/requestApproval` accurately resolves file paths even when params omit nested item details.
  - Wire Approval Decisions: Translated internal decisions (`allow`/`deny`/`cancel`) to official schema results (`accept`/`acceptForSession`/`decline`/`cancel`).
  - Full Lifecycle Routing: Added handling for `item/completed`, `turn/completed`, `item/agentMessage/delta`, and `item/commandExecution/outputDelta`.
- **Claude Durable Challenge & Appeal Subcommand**:
  - Persistent Challenge Store: Added atomic `SaveToFile` and `LoadFromFile` for `challenge.Store` across process lifetimes.
  - `claudebridge appeal` CLI: Added standalone subcommand for submitting structured justifications with cryptographic nonce validation.
  - One-Shot Retry Identity: Scoped retry request and correlation IDs with tool use ID, preventing idempotent replay bypass.

---

## [v0.1.0] - 2026-08-15

### Initial Foundations Release: Experimental Multi-Host Control-Plane & Protocol Bridges

Reinframe provides experimental fail-closed external supervision, dynamic model discovery, and protocol foundations for AI coding agents (OpenAI Codex App Server, Anthropic Claude Code PreTool hooks, xAI Grok Build ACP stdio).

> [!NOTE]
> **Scope & Evidence Boundary**: `v0.1.0` establishes formal protocol foundations, JSON-RPC 2.0 stdio clients, capability negotiations, and offline/synthetic qualification harnesses (`cmd/codexlive`, `cmd/claudelive`, `cmd/sparklive`). It validates that local components and protocol bridges satisfy strict fail-closed invariants and schema contracts. True production interactive host containment and multi-environment live qualification remain under active development (Epics #80 and #182).

#### Added
- **Multi-Host Adapter Architecture**:
  - **OpenAI Codex**:
    - Project-local hooks (`.codex/hooks.json`) installer, validator, and PreTool / Permission mapping (#163).
    - Delegated ChatGPT authentication boundary with zero credential extraction (#183, ADR 006).
    - App Server stdio JSON-RPC 2.0 runtime client with 1MB frame limit and Windows Job Object / Unix process tree lifecycle management (#184).
    - Entitlement-aware dynamic model discovery (`model/list`) with auth/scope cache partitioning (#185).
    - Dual-lane router (`DualLaneRouter`) strictly isolating subscription vs. API token billing boundaries (#186).
    - GPT-5.3-Codex-Spark Pro live qualification harness (`cmd/sparklive`) with pinned evidence (#187).
    - Direct OpenAI API capability-gated profile (`gpt-5.3-codex-spark`, `/v1/responses`) (#188).
    - 6-dimension integration churn test suite (#189).
  - **Anthropic Claude Code**:
    - Project-local hooks installer and settings manager (`.claude/settings.json`) (#106, #117).
    - PreToolUse hook adapter with structured `additionalContext` challenge delivery (#96, #116, #139).
    - Live qualification harness (`cmd/claudelive`) with Draft 2020-12 JSON Schema and pinned evidence (#120).
    - Direct Anthropic Messages API classifier adapter (`pkg/classifier/anthropic_messages.go`) (#135).
  - **xAI Grok Build**:
    - Native hooks (`.grok/hooks`) installer and PreToolUse gate (#165).
    - ACP (Agent Control Protocol) stdio JSON-RPC bridge with safe-boundary prompts (#166).
    - Live qualification harness (`cmd/groklive`) with source-bound ACK and durable failure handling (#167, #199, #200).
    - Direct xAI Responses API classifier adapter (`pkg/classifier/xai_responses.go`) (#137).

- **Challenge Workflow & Bypass Resistance**:
  - Host-neutral appealable challenge state machine (`pkg/challenge`) (#131, #139).
  - Cryptographically secure 16-byte random nonces (`crypto/rand`) for replay and tampering prevention.
  - One-shot retry lifecycle (`RetryBudget = 1`) with strict rejection of second retries, expired sequences, and scope expansions.
  - Challenge benchmark runner (`pkg/evaluation/challenge_benchmark.go`) demonstrating 100% bypass resistance across 20/20 adversarial attack vectors (#140).

- **Policy Engine & Shadow Classifiers**:
  - Fast-path deterministic policy and slow-path classifier policy routing (#69, #86).
  - Multi-provider classifier engine (`pkg/classifier`) supporting OpenAI Responses, Anthropic Messages, Google Gemini `generateContent`, and xAI Responses (#132, #134, #135, #136, #137).
  - Process-local exact assessment cache with singleflight deduplication and session-isolated partitioning (#138).

- **Core Event Persistence & State Invariants**:
  - High-performance SQLite WAL event store (`pkg/state`) with busy retry backoff, in-flight operation gating, and domain error preservation (#9).
  - Canonical event schemas (`reinframe.agent_event.v1`, `reinframe.task_contract.v1`, `reinframe.challenge_record.v1`, etc.) with strict JSON validation (#6).
  - 25 closed capability negotiation flags spanning Level 0 (Observe) through Level 3 (Intervene & App Server) (#7, #58).

- **Workspace Management & File Actuation**:
  - Isolated git worktree management (`pkg/workspace`) with clean-state guarantees, checkpointing, and rollback (#99).
  - Atomic FileActuator channel (`pkg/adapter/file_actuator.go`) using `.tmp` promotion and JSONL advice framing (#97, #208).

- **CLI Tooling & Live Control Harnesses**:
  - `cmd/codexlive`: Live qualification runner for OpenAI Codex.
  - `cmd/claudelive`: Live qualification runner for Anthropic Claude Code.
  - `cmd/sparklive`: Live qualification runner for GPT-5.3-Codex-Spark Pro.
  - `cmd/groklive`: Live control and privacy scan runner for xAI Grok Build.
  - `cmd/streetwire`: Offline demonstration runner and integration playground.

#### Security & Privacy Invariants
- **Zero Delegated Host Token Extraction**: Reinframe strictly never reads, logs, caches, or extracts host-owned credentials (`~/.codex/auth.json`, bearer headers, session cookies). Direct classifier provider API keys are handled in-process via environment configuration and never crossed into agent session state.
- **Domain-Separated Deterministic Hashes**: Scope and authentication generation identifiers are derived using deterministic, domain-separated SHA-256 digests (`ComputeScopeHash`, `ComputeAuthGenerationHash`).
- **Fail-Closed Dual-Lane Routing**: Prohibits cross-lane fallback across billing boundaries during rate limits (429) or token expirations (401).
- **Process Tree Cleanup**: Employs OS-native process tree lifecycle controls (Windows Job Objects with `KILL_ON_JOB_CLOSE`, Unix process groups with `Setpgid`).

---

[v0.1.0]: https://github.com/ImL1s/reinframe/releases/tag/v0.1.0
