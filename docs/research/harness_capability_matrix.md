# Target Agent Harness Capability & Integration Surface Matrix

## Overview
This matrix analyses coding agent frameworks/harnesses against operational dimensions relevant to **external Anti-Tunnel supervision**.

**Last revalidated:** 2026-08-15 (OAuth/Spark boundary alignment & deep-research pass).  
**Status:** *Partial* — high-signal version pins and known conflicts refreshed; **not** every Yes/No cell re-proven against first-party sources. Treat cells without a per-cell citation as **hypotheses**, not hard gates.

### Integration Level (this document’s axis)
These levels describe **how deeply Reinframe can integrate with a target harness** (capability surface), **not** the intervention escalation ladder after tunnel detection.  
See [`level_axes_mapping.md`](./level_axes_mapping.md) for the dual-axis map vs Intervention Levels and the **Three Orthogonal Operational Axes** (Integration Capability, Credential Class, Model Support State).

| Integration Level | Meaning (capability surface) |
|---|---|
| **L0 Observe-only** | Passive log/stdout tailing, git/diff observation; no reliable advice delivery |
| **L1 Advisory** | Can inject zoom-out / replan guidance (requires advice/context delivery) |
| **L2 Guarded** | Can pause/cancel/restrict tools **via harness-native control** (or an explicitly accepted OS pause contract) |
| **L3 Full-control** | Checkpoint/rollback, model switch, subagent orchestration, headless control |

### Protocol contract (code) — do not conflate with matrix “target level”
From `pkg/protocol/capability.go` (handshake gates):

| Negotiated level | Required flags (current code) |
|---|---|
| L0 | `CapEventStream` only |
| L1 | + `CapToolInspection` (**#65 will also require `CapAdviceDelivery` for true Advisory**) |
| L2 | + `CapDiffInspection`, `CapPause`, `CapCancel`, `CapResume` |
| L3 | + `CapCheckpoint`, `CapRollback`, `CapHeadless`, `CapCLIControl`, `CapMCP`, `CapSubagents`, `CapSwitchModel` |

**Hard rule:** A harness with matrix **Pause Task = No** cannot be handshake-gated as protocol L2 under current `Level2RequiredMask` unless `CapPause` is redefined (e.g. OS SIGSTOP accepted) — tracked as a product decision issue.

---

## Comparative Matrix (24 Dimensions × 12 Frameworks)

| Metric / Dimension | Claude Code | OpenAI Codex CLI | Cursor Agent | Aider | OpenHands | Cline | Roo Code | Goose | LangGraph | AutoGen | OpenAI Agents SDK | Automode (oh-my-agy) |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| **1. Native Subagents** | Yes | No* | No | No | Yes | No | No | **Yes** | Yes | Yes | Yes | Yes |
| **2. Headless Mode** | Yes (`-p`) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| **3. CLI Control** | Yes | Yes | Yes | Yes | Yes | Extension | Extension | Yes | CLI/API | CLI/Python | Python SDK | CLI |
| **4. SDK / API** | Anthropic API | OpenAI API / App Server | Cursor RPC | Python API | REST/Python | VSCode Extension | VSCode Extension | Rust/Python | Python/TS | Python API | Python SDK | CLI / Hooks |
| **5. Hook Support** | Yes (hooks / settings) | Yes (hooks.json / App Server) | No | Git / Pre-commit | Event Hooks | VSCode Events | VSCode Events | Extensions | State Graph | Event Callbacks | Agent Hooks | Lifecycle Hooks |
| **6. Event Stream** | Stdio / JSONL | Stdio JSON / App Server RPC | RPC | Stdio | WebSocket / REST | Extension Host | Extension Host | Stdio | State Stream | Event Stream | Event Stream | NDJSON Stream |
| **7. MCP Support** | Yes | Yes | Yes | Partial* | Yes | Yes | Yes | Yes | Custom Tool | Custom Tool | Custom Tool | Native |
| **8. Pause Task** | **No** (OS SIGSTOP only) | **No** (CLI) / Planned (App Server) | No | No | **Yes** (SDK pause/resume) | Yes | Yes | No | State Interrupt | No | State Interrupt | Yes |
| **9. Cancel Task** | Yes (SIGINT / stop) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Yes |
| **10. Resume Session** | Yes (`--resume`) | Yes | Yes | Yes | Yes | Yes | Yes | Yes | Checkpoint Resume | State Resume | Thread Resume | Yes |
| **11. Checkpoint** | Git / Session | Git | Git | Git Commit | Docker / Git | Git | Git | Git | Memory Checkpoint | DB State | Thread State | Git / State |
| **12. Rollback** | Git reset | Git reset | Git restore | Git undo | Container Reset | Git restore | Git restore | Git undo | State Rewind | State Rollback | Thread Rollback | Git Checkpoint |
| **13. Switch Model** | Yes | Yes (dynamic catalog) | Yes | Yes | Yes | Yes | Yes | Yes | Node Router | Agent Router | Model Router | Yes |
| **14. Custom Provider** | Anthropic / Bedrock | OpenAI / Azure / Custom | Proprietary | Any LLM | Any LLM | Any LLM | Any LLM | Any LLM | Custom Endpoint | Custom Endpoint | Custom Endpoint | Any Provider |
| **15. OpenAI Compatible** | Proxy | Native | No | Native | Native | Native | Native | Native | Native | Native | Native | Native |
| **16. Local Models** | Proxy | Ollama / vLLM | No | Ollama | Ollama / Local | Ollama / LMStudio | Ollama / LMStudio | Ollama | Local LLM | Local LLM | Local LLM | Ollama / vLLM |
| **17. Token / Cost Usage** | Stdio Summary | Response Header / Quota | Metrics UI | Usage Output | Session API | UI Panel | UI Panel | CLI Summary | Graph State | Callback Cost | Run Usage | Event Store |
| **18. Tool Call / Output** | Streamed | Streamed | Streamed | Streamed | WebSocket Stream | UI Stream | UI Stream | Stdio | Event Log | Callback Log | Event Log | Streamed NDJSON |
| **19. Patch / Diff / Error** | Git Diff | Git Diff | Workspace Diff | Git Diff | Event Diff | Editor Diff | Editor Diff | Git Diff | State Diff | Execution Log | Run Artifacts | Audit Diff |
| **20. Extension Point** | Hooks / Subagents | Hooks / App Server / MCP | VSCode Ext | Python Modules | Micro-agents | VSCode Ext | VSCode Ext | Extensions | Custom Nodes | Custom Agents | Custom Tools | Adapters / Hooks |
| **21. License** | Proprietary CLI | MIT / Apache | Proprietary | Apache-2.0 | MIT | Apache-2.0 | Apache-2.0 | Apache-2.0 | MIT | MIT | MIT | MIT |
| **22. Known Limits** | Pause not native; UI lock | Rate limits / Preview churn | Closed Ext API | Stdio parsing | Docker overhead | IDE bound | IDE bound | Rust plugin API | Code overhead | Python runtime | SDK preview | Subagent max_depth |
| **23. Verified Version** | **v2.1.220** (npm 2026-08-02) | **rust-v0.146.0** / App Server | v0.42 *(not revalidated)* | **v0.86.2** (PyPI aider-chat) | **v1.8.0** (GH 2026-07-30) | v3.2 *(not revalidated)* | v2.1 *(not revalidated)* | v1.2 *(subagents docs revalidated)* | **v1.2.10** (PyPI) | v0.4.0 *(not revalidated)* | v0.1.0 *(not revalidated)* | v2.4 *(not revalidated)* |
| **24. Source Link** | npm `@anthropic-ai/claude-code` | github.com/openai/codex | cursor.com | github.com/Aider-AI/aider | github.com/All-Hands-AI/OpenHands | github.com/cline | github.com/RooVetGit | goose-docs.ai | pypi.org/project/langgraph | github.com/microsoft/autogen | github.com/openai | github.com/ImL1s |

\* **Footnotes (2026-08-15 revalidation)**  
1. **Goose Native Subagents:** first-party docs describe first-class internal/external subagents → **Yes**.  
2. **Claude Code Pause:** still **No** as harness-native pause. **#72 decision (option 1 strict native):** OS `SIGSTOP` is **not** `CapPause` and must not be advertised as `SupportsPause`.  
3. **Claude Code / Codex as “L2 targets”:** under current protocol masks CLI-only setups **cannot hard-gate L2** while Pause=No. Recommended mapping: **Integration L1 (hooks + advisory)** with *aspirational* L2 if App Server or pause contracts are solved.  
4. **Codex App Server & OAuth Delegation:** Codex CLI / `codex app-server` manages ChatGPT OAuth credentials natively. Reinframe operates as an external supervisor and never extracts tokens directly (#183/#184).  
5. **Codex Models & Spark Preview:** Dynamic model catalog discovery (#185) queries the authenticated account scope. Research preview models such as **GPT-5.3-Codex-Spark** (announced 2026-02-12 for ChatGPT Pro) require live empirical qualification (#187) before support claims; subscription access does not imply OpenAI API access (#188).  
6. **OpenHands Pause=Yes** matches current SDK pause/resume docs — valid L2 *capability* candidate for that harness only.  
7. **Aider MCP=Partial** retained: native MCP still incomplete; third-party bridges exist.  

---

## Integration Level Mapping (capability surface only)

### Level 0: Observe-only Integration
- **Typical harnesses:** IDE extensions without control APIs; any agent when only logs/git are available (e.g. Codex EventSource #107/#118).
- **Supervision:** Tail logs, git status, diff churn, process stats.
- **Intervention:** Human notifications / audit only (no `CapAdviceDelivery`).

### Level 1: Advisory Integration
- **Typical harnesses:** Stdio agents + hook-capable CLIs (Aider, Goose, **Claude Code with hooks**, **Codex with project-local hooks #163**).
- **Supervision:** Event/tool streams, cost signals where available.
- **Intervention:** Inject zoom-out / replan advice; requires delivery caps (#65). **Does not require CapPause.**

### Level 2: Guarded Integration
- **Typical harnesses:** Agents with **native pause/cancel/tool gate** (e.g. OpenHands pause API; Codex App Server runtime #184).
- **Supervision:** Real-time events, pre-tool inspection.
- **Intervention:** Pause, cancel, reject tool, request re-plan.
- **Gate:** Must satisfy protocol `Level2RequiredMask` **or** a documented CapPause equivalence decision.

### Level 3: Full-control Integration
- **Typical harnesses:** Supervisor-native / graph agents with checkpoint+subagent+model switch (LangGraph-class, custom SDKs, Automode).
- **Intervention:** Workspace checkpoint/rollback, model switch, subagent replace, terminate.

---

## Authoritative References & Retrieval Dates

- [Introducing GPT-5.3-Codex-Spark](https://openai.com/index/introducing-gpt-5-3-codex-spark/) (Published: 2026-02-12)
- Codex Auth: `https://developers.openai.com/codex/auth` (Retrieved: 2026-08-15)
- Codex Models: `https://developers.openai.com/codex/models` (Retrieved: 2026-08-15)
- Codex App Server: `https://developers.openai.com/codex/app-server` (Retrieved: 2026-08-15)
- Codex SDK: `https://developers.openai.com/codex/sdk` (Retrieved: 2026-08-15)

---

## Revalidation log
| Date | Change |
|---|---|
| 2026-08-02 | Deep-research: refresh Claude Code / OpenHands / Codex / LangGraph / Aider versions; Goose Subagents→Yes; document Pause/L2 gate conflict; dual-axis pointer |
| 2026-08-15 | Epic #182 sync: document Codex App Server runtime, delegated OAuth boundary, dynamic model catalog, and GPT-5.3-Codex-Spark research preview status |
