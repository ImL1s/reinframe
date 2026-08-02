# Target Agent Harness Capability & Integration Surface Matrix

## Overview
This matrix provides a detailed analysis of 12+ coding agent frameworks and harnesses evaluated on **2026-08-02**.
It rates each harness against 24 specific operational dimensions and categorizes them into one of four Anti-Tunnel Harness Integration Levels:
- **Level 0 (Observe-only)**: Passive log/stdout tailing and diff analysis.
- **Level 1 (Advisory)**: Injects zoom-out guidance and contrarian perspectives via messages or prompt context.
- **Level 2 (Guarded)**: Can pause, cancel, restrict tools, or request re-planning.
- **Level 3 (Full-control)**: Full process control, checkpointing, session rollback, model switching, and subagent orchestration.

---

## Comparative Matrix (24 Dimensions × 12 Frameworks)

| Metric / Dimension | Claude Code | OpenAI Codex CLI | Cursor Agent | Aider | OpenHands | Cline | Roo Code | Goose | LangGraph | AutoGen | OpenAI Agents SDK | Automode (oh-my-agy) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **1. Native Subagents** | Yes | No | No | No | Yes | No | No | No | Yes | Yes | Yes | Yes |
| **2. Headless Mode** | Yes (`-p`) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| **3. CLI Control** | Yes | Yes | Yes | Yes | Yes | Extension | Extension | Yes | CLI/API | CLI/Python | Python SDK | CLI |
| **4. SDK / API** | Anthropic API | OpenAI API | Cursor RPC | Python API | REST/Python | VSCode Extension | VSCode Extension | Rust/Python | Python/TS | Python API | Python SDK | CLI / Hooks |
| **5. Hook Support** | Yes (`~/.claude/hooks`) | Custom scripts | No | Git / Pre-commit | Event Hooks | VSCode Events | VSCode Events | Extensions | State Graph | Event Callbacks | Agent Hooks | Lifecycle Hooks |
| **6. Event Stream** | Stdio / JSONL | Stdio JSON | RPC | Stdio | WebSocket / REST | Extension Host | Extension Host | Stdio | State Stream | Event Stream | Event Stream | NDJSON Stream |
| **7. MCP Support** | Yes | Yes | Yes | Partial | Yes | Yes | Yes | Yes | Custom Tool | Custom Tool | Custom Tool | Native |
| **8. Pause Task** | No (SIGSTOP) | No | No | No | Yes | Yes | Yes | No | State Interrupt | No | State Interrupt | Yes |
| **9. Cancel Task** | Yes (SIGINT) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| **10. Resume Session** | Yes (`--resume`) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Checkpoint Resume | State Resume | Thread Resume | Yes |
| **11. Checkpoint** | Git / Session | Git | Git | Git Commit | Docker / Git | Git | Git | Git | Memory Checkpoint | DB State | Thread State | Git / State |
| **12. Rollback** | Git reset | Git reset | Git restore | Git undo | Container Reset | Git restore | Git restore | Git undo | State Rewind | State Rollback | Thread Rollback | Git Checkpoint |
| **13. Switch Model** | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Node Router | Agent Router | Model Router | Yes |
| **14. Custom Provider** | Anthropic / Bedrock | OpenAI / Azure | Proprietary | Any LLM | Any LLM | Any LLM | Any LLM | Any LLM | Custom Endpoint | Custom Endpoint | Custom Endpoint | Any Provider |
| **15. OpenAI Compatible** | Proxy | Native | No | Native | Native | Native | Native | Native | Native | Native | Native | Native |
| **16. Local Models** | Proxy | Ollama / vLLM | No | Ollama | Ollama / Local | Ollama / LMStudio | Ollama / LMStudio | Ollama | Local LLM | Local LLM | Local LLM | Ollama / vLLM |
| **17. Token / Cost Usage** | Stdio Summary | Response Header | Metrics UI | Usage Output | Session API | UI Panel | UI Panel | CLI Summary | Graph State | Callback Cost | Run Usage | Event Store |
| **18. Tool Call / Output** | Streamed | Streamed | Streamed | Streamed | WebSocket Stream | UI Stream | UI Stream | Stdio | Event Log | Callback Log | Event Log | Streamed NDJSON |
| **19. Patch / Diff / Error** | Git Diff | Git Diff | Workspace Diff | Git Diff | Event Diff | Editor Diff | Editor Diff | Git Diff | State Diff | Execution Log | Run Artifacts | Audit Diff |
| **20. Extension Point** | Hooks / Subagents | Custom Scripts | VSCode Ext | Python Modules | Micro-agents | VSCode Ext | VSCode Ext | Extensions | Custom Nodes | Custom Agents | Custom Tools | Adapters / Hooks |
| **21. License** | Proprietary CLI | MIT / Apache | Proprietary | Apache-2.0 | MIT | Apache-2.0 | Apache-2.0 | Apache-2.0 | MIT | MIT | MIT | MIT |
| **22. Known Limits** | Terminal UI lock | Rate Limits | Closed Ext API | Stdio parsing | Docker overhead | IDE bound | IDE bound | Rust plugin API | Code overhead | Python runtime | SDK preview | Subagent max_depth |
| **23. Verified Version** | v1.0.4 (2026-07) | v0.8.2 (2026-07) | v0.42 (2026-07) | v0.35 (2026-06) | v0.14 (2026-07) | v3.2 (2026-07) | v2.1 (2026-07) | v1.2 (2026-06) | v0.2.8 (2026-07) | v0.4.0 (2026-07) | v0.1.0 (2026-07) | v2.4 (2026-07) |
| **24. Source Link** | github.com/anthropic | github.com/openai | cursor.com | github.com/aider | github.com/All-Hands-AI | github.com/cline | github.com/RooVetGit | github.com/block | github.com/langchain-ai | github.com/microsoft | github.com/openai | github.com/ImL1s |

---

## Integration Level Mapping

### Level 0: Observe-only Integration
- **Target Harnesses**: Proprietary IDE Extensions (Cursor, Cline, Roo Code) when running without explicit control APIs.
- **Supervision Capability**: Tail log files, git status, diff churn, and process CPU/RAM.
- **Intervention Capability**: Read-only alerts, developer notifications, audit logs.

### Level 1: Advisory Integration
- **Target Harnesses**: Stdio-based agents (Aider, Goose, OpenAI Agents SDK).
- **Supervision Capability**: Full stdio inspection, tool call parsing, cost tracking.
- **Intervention Capability**: Send advisory prompts (`/zoom-out`), inject alternative hypotheses into context.

### Level 2: Guarded Integration
- **Target Harnesses**: Headless CLI agents with lifecycle hooks (Claude Code, OpenAI Codex CLI, OpenHands).
- **Supervision Capability**: Real-time event streams, pre-tool-call inspection, exact error fingerprinting.
- **Intervention Capability**: Pause process, cancel current action, reject tool call execution, request re-planning.

### Level 3: Full-control Integration
- **Target Harnesses**: Supervisor-native agents (oh-my-agy, LangGraph with state persistence, custom Agent SDKs).
- **Supervision Capability**: Direct memory checkpoint inspection, subagent state tree, full token/cost accounting.
- **Intervention Capability**: Full workspace checkpointing, Git session rollback, automatic model switching, subagent replacement.
