# M1 Minimum Vertical Slice Scope, Non-Goals & Compatibility Matrix

## 1. Overview
The goal of Milestone M1 is to deliver a fully functional, verifiable end-to-end vertical slice of the Anti-Tunnel Harness.
This document defines the exact boundaries of M1, explicit non-goals, and target OS compatibility requirements.

---

## 2. Milestone M1 Minimum Vertical Slice Requirements

### Target Scope (In-Scope for M1)
1. **Target Agent Adapter**:
   - One fully functional `LogObserverAdapter` for tailing stdout/stderr/log files (Level 0).
   - One `MockAgentAdapter` for deterministic E2E integration testing.
2. **Reviewer Provider**:
   - One `OpenAICompatibleProvider` connecting to local endpoints (Ollama/vLLM) or OpenAI REST API.
   - One `MockReviewerProvider` returning predictable JSON classification schemas.
3. **Event Stream & Persistence**:
   - Canonical Event model parsing (AgentSession, ToolCall, TestResult).
   - SQLite WAL event store persisting append-only logs.
4. **Deterministic Detector**:
   - `RepeatedErrorDetector` calculating normalized error string fingerprints.
5. **State Machine & Policy**:
   - State transition engine (OBSERVE $\rightarrow$ EXECUTE $\rightarrow$ AUDIT $\rightarrow$ SUSPECT $\rightarrow$ ZOOM_OUT $\rightarrow$ RESUME).
   - Cooldown timer and minimum confidence threshold.
6. **Reviewer Roles**:
   - `EvidencePackBuilder` constructing objective diff and log summaries (`fork_turns: none`).
   - `TunnelClassifier` requesting structured JSON classification.
7. **Intervention & Rollback**:
   - `Adjudicator` triggering `ZOOM_OUT` re-planning advice or Git checkpoint rollback.
8. **Empirical End-to-End Test**:
   - One reproducible E2E test (`test/e2e/vertical_slice_test.go`) asserting the full detection $\rightarrow$ review $\rightarrow$ intervention $\rightarrow$ session recovery trajectory.

---

## 3. Explicit Non-Goals for M1
To prevent scope creep during initial delivery, the following features are strictly **NON-GOALS for M1**:
- Direct multi-model voting / consensus algorithms (verifiable discriminating experiments are preferred in M2).
- Automatic remote database or cloud server state rollbacks (only Git workspace rollbacks are in scope).
- Subagent recursive nesting beyond `max_depth = 1`.
- Custom GUI / Web Dashboard (CLI stdio logs and structured JSON files only).
- Proprietary IDE extension binaries (standalone CLI supervisor binary only).

---

## 4. Target OS Compatibility Matrix

| OS Platform | Architecture | Supported Mode | Test Runner | Status |
|---|---|---|---|---|
| **macOS 14+ (Sonoma/Sequoia)** | Apple Silicon (arm64) & Intel (x64) | Full Supervisor & Stdio | Native | Primary |
| **Linux (Ubuntu 22.04+)** | x64 & arm64 | Full Supervisor & Stdio | Docker / Native | Primary |
| **Windows 11 / Server 2022** | x64 | Full Supervisor (Job Objects) | PowerShell / Cmd | Primary |
