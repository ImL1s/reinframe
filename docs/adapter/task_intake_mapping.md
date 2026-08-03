# Task intake mapping (host → core)

**Issue:** #84  
**Normative model:** [`docs/specs/adaptive_task_supervisor.md`](../specs/adaptive_task_supervisor.md) §2  

Core uses **`protocol.TaskSubmitted`**. Host hook names (e.g. Claude Code “UserPromptSubmit”) appear **only** in adapter mappers and fixtures — never as `pkg/protocol` type names.

## Mapping table

| Host surface | Core |
|--------------|------|
| Claude Code UserPromptSubmit (JSON fixture / future hook adapter) | `TaskSubmitted` via `adapter.MapClaudeUserPromptSubmitJSON` |
| Codex user input / thread message | `TaskSubmitted` via `adapter.MapCodexUserInputJSON` |
| API task payload | `TaskSubmitted` via `adapter.MapAPITaskPayloadJSON` |
| CLI initial prompt | `TaskSubmitted` via `adapter.MapCLIInitialPrompt` |
| Generic host payload | `TaskSubmitted` via `adapter.IntakeFromHost` |

Optional: `TaskIntakeOptions.BuildContract` → `protocol.BuildContractFromSubmitted` draft.

## Package rules

- `pkg/protocol` — host-agnostic types only  
- `pkg/adapter` — host mappers + labels (`HostHintClaudeCode`, …)  
- Live Claude Code / Codex **actuators** are **out of scope** for #84 (fixtures + stubs only)

## Flow

```text
host event JSON / fields
  → adapter mapper
  → TaskSubmitted
  → optional TaskContract draft
  → AgentEventFromTaskSubmitted / store
```
