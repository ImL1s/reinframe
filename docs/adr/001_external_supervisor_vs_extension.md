# ADR 001: External OS Process Supervisor vs IDE Extension vs Pure MCP Architecture Choice

- **Status**: Decided & Approved
- **Date**: 2026-08-02
- **Deciders**: Architecture Team
- **Technical Story**: Issue #3

---

## Context and Problem Statement
To supervise autonomous AI coding agents (Claude Code, Codex CLI, Cursor Agent, Aider, OpenHands, etc.), Reinframe requires an architecture that can monitor process events, inspect workspace diffs, manage process life cycles, enforce budget limits, and execute interventions across Windows, macOS, and Linux.
We evaluated three structural patterns:
1. **Option A**: Pure IDE Extension (VSCode/Cursor Extension).
2. **Option B**: Pure MCP Server (Model Context Protocol).
3. **Option C**: External OS Process Supervisor + MCP Bridge (Hybrid).

---

## Decision Drivers
- Cross-platform process isolation and crash survival.
- Ability to monitor headlessly in CI/CD without an active IDE GUI.
- Ability to supervise non-IDE CLI agents (Claude Code CLI, Codex CLI, custom scripts).
- Support for MCP tool-calling and resource exposure.
- Zero dependency on proprietary editor extension hosts.

---

## Considered Options

### Option A: Pure IDE Extension
- **Pros**: Direct access to editor DOM, active document state, and IDE terminal.
- **Cons**: Tied to VSCode runtime; cannot monitor headless CLI agents or CI runners; crashes if IDE host crashes.

### Option B: Pure MCP Server
- **Pros**: Standardized protocol; easy integration with MCP-compliant agents.
- **Cons**: Passive tool-provider model; cannot independently kill, pause, or spawn process trees unless agent explicitly invokes the tool; lacks OS process supervision capabilities.

### Option C: External OS Process Supervisor + MCP Bridge (Selected)
- **Pros**:
  - Operates as a standalone OS binary across Windows, macOS, and Linux.
  - Can spawn, observe, pause, SIGINT, or terminate target agent processes.
  - Exposes an embedded MCP bridge for agents that support active MCP advisory tool calls.
  - Survives IDE crashes and functions headlessly in CI/CD pipelines.
- **Cons**: Requires building cross-platform process management abstractions.

---

## Decision Outcome
**Selected Option**: **Option C — External OS Process Supervisor with Embedded MCP Bridge**.

### Consequences
- **Positive**: Reinframe can supervise any CLI process agent, tail log files for observe-only agents, and host an MCP bridge for interactive agents.
- **Negative**: Must manage OS process groups, signals, stdio pipes, and Job Objects on Windows.
- **Mitigation**: Implement a clean `TargetAgentAdapter` interface in Go with platform-specific OS process bindings (`sys/windows`, `sys/unix`).
