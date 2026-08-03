# Tool-budget and hypothesis-loop detectors (#98)

Library detectors for long **review** sessions. Thresholds are **provisional** (not M3 calibrated — see #100).

## ToolBudgetChurnDetector

**Failure mode:** `tool_budget_churn`  
**Fire:** `tools_since_progress >= MaxToolCalls` (default 30, or `TaskContract.ToolBudget.MaxToolCalls` via `ObserveWithBudget`).  
**Progress:** `file_change` / `evidence` / `criterion_met` events or `MarkProgress` reset the window.  
**No-fire:** short sessions under budget; healthy work that marks progress.

## HypothesisLoopDetector

**Failure mode:** `hypothesis_loop`  
**Fingerprint:** `NormalizeFingerprint(text)` of conclusion/rationale.  
**Fire:** same fingerprint reaches threshold (default 3) **without** any new evidence ID.  
**No-fire:** each probe attaches a new evidence ID (file path, criterion id, …); under-threshold repeats.

## Policy wiring (thin)

`policy.EvaluateSlow` treats `tool_budget_churn` and `hypothesis_loop` as high-confidence deterministic `ZOOM_OUT_PROMPT` candidates (same path as repeated_error_loop).  
**Does not** auto-intervene on live hosts without EventSource + Actuator + supervisor composition.

## Honesty

Closing #98 as **library complete** means fire/no-fire tests + optional policy recognition.  
It does **not** mean live review-session auto-supervision.
