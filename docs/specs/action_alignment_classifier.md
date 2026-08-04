# Action Alignment Classifier — two-stage design (#104)

**Status:** normative research design (2026-08-04)  
**Parent:** Epic #80  
**Implements issue:** #104 (design only — shadow implementation is #105)  
**Evidence:** Claude Auto Mode capture is an **architecture pattern**, not a content/policy to copy.

## Public decision contract

The **classifier Stage 2 public outcome** is only:

```text
ALLOW | BLOCK
```

A model `severity` score is **evidence for the Harness**, never direct authority to block.

**Note:** Existing HookGate still has three *transport* actions (`allow` | `deny` | `defer`) for pending-advisory latch. That `defer` is **not** a Stage 2 classifier public decision — it is adapter tool-gating until advice ACK. Stage 0 may *map* to HookGate defer as a pre-classifier control, but must not expand the Stage 2 public enum.

## Stage 0 — deterministic pre-rules (no model)

Runs first on every PreTool / proposed action:

| Rule class | Example | Decision |
|------------|---------|----------|
| Hard deny tool/path | `DeniedTools`, scope whitelist | **BLOCK** |
| Budget exhausted | `BudgetExhausted` | **BLOCK** |
| Pending advisory latch | `PendingAdvisoryInterventionID` | HookGate **defer** (tool gate; not Stage 2 enum) or **BLOCK** if policy requires |
| Verification churn / over-SOP | before_tool disproportionate | **BLOCK** |
| High-confidence detector latch | repeated_error already enqueued | HookGate **defer**/policy existing path (not Stage 2 public) |

Stage 0 **must not** call Reviewer or Stage 1 classifier.

## Stage 1 — raw severity (optional model)

### Input (curated — not full transcript)

```text
TaskContract / Task Anchor
Proposed Action (tool name + bounded args summary)
Relevant Recent Events (IDs + short digests)
EvidenceLedger summary
Classifier ruleset (versioned hash)
```

**Forbidden inputs:** unrestricted full transcript, raw secrets, main-agent self-justification as sole evidence.

### Output (raw assessment only)

```json
{
  "severity": 0,
  "reason_code": "NORMAL_PROGRESS",
  "evidence_event_ids": [],
  "model_id": "",
  "model_version": "",
  "ruleset_hash": ""
}
```

- `severity` ∈ **0–100** integer  
- Stage 1 grades **misalignment / productivity harm only** — does **not** apply user exceptions or repo-policy overrides  
- Must cite `evidence_event_ids` from the supplied pack  
- Security-policy classifiers (e.g. Claude security) that ignore looping/wasted tokens are a **different class** — Reinframe productivity classifiers must not be conflated with security content  

### Event window (decision)

Prefer **contract-bound recent events** over blind “last N messages”:

1. Events since last TaskContract revision, capped by count and bytes  
2. Always include last detector signals and last validation records  
3. Exclude raw tool stdout bodies (digests only)  

## Stage 2 — deterministic resolver (Harness owns ALLOW|BLOCK)

Inputs: Stage 1 payload + user intent + repository policy + TaskContract risk + flaky/exemption flags + **classifier profile threshold** + adapter capability + fail-open/closed policy.

### Output

```json
{
  "decision": "ALLOW",
  "reason_code": "below_threshold",
  "feedback": "",
  "raw_severity": 0,
  "threshold": 50,
  "profile_id": "provisional-default",
  "resolver_reason": "stage0_skip|stage1_applied|stage0_block"
}
```

Only `ALLOW` and `BLOCK` are public. Advice / replan / escalation remain separate intervention types (existing ZOOM_OUT path).

### Threshold policy

- Default profile threshold **provisional 50** (document only — **not** a product hard-gate)  
- Hard-gate promotion requires #100 benchmarks + **separate** promotion issue  
- Shadow mode (#105): compute Stage1+Stage2, **log only**, never change HookGate outcome  

### Policy classes

| Class | Owner | Notes |
|-------|-------|-------|
| `PRODUCTIVITY` | Reinframe Action Alignment | loops, churn, scope, evidence gap |
| `SECURITY` | separate future track | do not reuse productivity thresholds |

## Mapping to existing Reinframe surfaces

| Existing | Role under this design |
|----------|------------------------|
| `HookGate` / `EvaluateBeforeTool` | Stage 0 (+ final apply of Stage 2 in future enforce mode) |
| Detectors (#82/#85/#98) | Stage 0 signals / Stage 1 evidence |
| Optional LLM Reviewer (PR #103) | May power Stage 1 **only** when `Uncertain`/shadow; never replace Stage 0 |
| FileActuator | Transport for feedback/ZOOM_OUT — not classifier authority |

## Non-goals (#104)

- Implementing shadow runtime (#105)  
- Calibrated hard-gates (#100)  
- Copying Claude security rule text  
- Claiming Stage 2 is a second LLM call (implementation may be pure code; Stage 2 mechanism is Harness-owned and may be deterministic-only)

## Acceptance for #104

- [x] This document exists and is linked from CURRENT roadmap  
- [x] Stage 0 / 1 / 2 contracts specified  
- [x] Public decision is ALLOW|BLOCK only  
- [x] Shadow vs enforce separation documented  
- [x] No claim of calibrated thresholds  

## Next

#105 — shadow-mode implementation recording Stage1/Stage2 without blocking.  

## Wire contract (#119)

Implementation contract: [`action_alignment_wire_contract.md`](action_alignment_wire_contract.md) and ADR [`005-classifier-provider.md`](../adr/005-classifier-provider.md).
