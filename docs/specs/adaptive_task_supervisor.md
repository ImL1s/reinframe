# Adaptive Task Supervisor — Normative Model

**Status:** Normative (2026-08-02)  
**Product identity:** Reinframe is an **Adaptive Task Supervisor**, not only an “anti-tunnel detector.”

It decides whether the agent’s **next action** is still matched to:

```text
user intent + task risk + progress + evidence sufficiency + action cost
```

---

## 1. Four deviation classes

| Class | Name | Symptom | Typical control |
|-------|------|---------|-----------------|
| A | **direction_fixation** | Wrong hypothesis / repeated identical failures | Zoom-out, replan, optional pause |
| B | **process/SOP fixation (verification_churn)** | Same successful validation re-run without workspace/contract change | PreTool deny redundant work |
| C | **scope_drift** | Small task expands into unbounded rewrite | Scope deny / replan |
| D | **evidence_gap** | High-risk or criterion-required work finishes without proof | Stop block / require targeted validation |

Detectors map into these classes (non-exhaustive):

```text
direction_fixation
progress_stagnation
scope_drift
verification_churn
evidence_gap
```

M2 first slice focuses on **direction_fixation** (repeated failure).  
M2.1 adds **verification_churn** / effort calibration. Other classes follow.

---

## 2. Phased responsibilities (harness-agnostic)

Core types **must not** use Claude-specific hook names. Adapters map host events onto core events.

| Phase | Core event / stage | Responsibility |
|-------|--------------------|----------------|
| Task intake | **TaskSubmitted** | Capture immutable user request; start contract build |
| Contract | **TaskContract** (revisioned) | Complexity, risk, criteria, evidence & tool budgets |
| Observation | tool/failure/file/test events | Update **EvidenceLedger** |
| Pre-action control | **before_tool** boundary | Fast-path allow/deny/defer (primary brake for over-SOP) |
| After-action | **after_tool** | Progress / validation records |
| Completion | **turn_end** / stop-equivalent | Allow stop if evidence enough; block if evidence_gap |
| Compact / resume | session lifecycle | Preserve contract, ledger, pending interventions |

### Claude Code adapter mapping (example only)

| Claude Code surface | Core |
|---------------------|------|
| `UserPromptSubmit` | TaskSubmitted (+ contract builder) |
| `PostToolUse` / failures | observation events → EvidenceLedger |
| `PreToolUse` | before_tool gate (main brake) |
| `Stop` | turn_end / completion adjudication |
| `PreCompact` | persist contract/ledger/pending |
| `SessionStart` | restore state |

Other harnesses map similarly (Codex tool gate, CLI stdin, MCP pull, observe-only human alert).

---

## 3. Three separated data objects

### 3.1 TaskEnvelope (immutable user request)

User original ask. **Do not revise in place** when the user appends constraints; open a new envelope or new contract revision instead.

Existing `protocol.TaskEnvelope` remains the envelope surface (prompt, scope whitelist, depth, timeout). Additional intake fields live on **TaskSubmitted**.

### 3.2 TaskContract (revisioned workload / evidence budget)

Derived at intake (and revisable when intent or policy changes):

- Complexity / risk / confidence  
- Success criteria  
- Required evidence  
- Allowed scope  
- Validation / tool / subagent / reviewer budgets  
- Provenance: user_explicit | repository_policy | heuristic | model  

### 3.3 EvidenceLedger (what was actually proven)

- Criterion status  
- Validation records (with fingerprint: command + scope + workspace revision + contract revision + purpose)  
- Tool call counts  
- Last progress / workspace hash  

**Nil defaults:** First M2 repeated-failure slice (#69/#70/#71) may pass `TaskContract` / `EvidenceLedger` as nil/default. Policy APIs must reserve pointers so M2.1 does not break callers.

---

## 4. Control flow

```text
TaskSubmitted
    → TaskContract Builder (optional in M2.0 slice)
    → Agent work
    → Observation events → EvidenceLedger
    → Detectors (class-specific)
    → Policy Pre-Adjudication
         ├─ high-confidence deterministic → Intervention
         └─ uncertain / high-risk → optional Reviewer → final adjudication
    → Pending intervention
    → Adapter delivery at declared SafeBoundary
    → AckPolicy handling
    → Audit / Store
```

### Over-SOP (verification_churn) — primary brake is **before_tool**

Not “remind at stop after waste.” Deny the second no-information-gain tool at **PreTool / before_tool**.

Stop/turn_end is for **completion hygiene** (evidence_gap or clean exit), not the only advice channel.

### Direction fixation

Prefer **turn_end** zoom-out when possible; use before_tool only if the next action is dangerous or continues the stuck path.

---

## 5. SafeBoundary (adapter-declared)

```text
before_tool   — before tool/command execution (primary for over-SOP)
after_tool    — after tool result
turn_end      — end of agent turn / stop-equivalent
next_input    — before next user/agent input
```

Adapters declare which boundaries they support. Core queues interventions with a preferred boundary; delivery degrades if unsupported.

---

## 6. AckPolicy (multi-layer)

```text
explicit     — agent_ack (explicit replan receipt) — required in #71 fake slice
transport    — delivery_receipt only (harness accepted message)
behavioral   — next actions match intervention intent
none         — no ack; human escalate if critical
```

Many real harnesses only guarantee **transport**.  
`CapInterventionAck` / AckPolicy must drive degradation (see #71).

---

## 7. Policy pre-adjudication (Reviewer optional)

Linear “always Reviewer” is forbidden for cost control.

High-confidence deterministic examples (no Reviewer):

- Same successful validation, unchanged workspace, same contract revision, re-run  
- N-th identical error fingerprint  
- Explicit token/tool budget exceeded  
- Forbidden path write  

Reviewer-worthy:

- Unclear deep healthy work vs tunnel  
- Contradictory evidence vs hypothesis  
- Task complexity shift  
- Competing hypotheses / high-risk rollback  

---

## 8. Validation fingerprint (for verification_churn)

Must include more than command string:

```text
command
+ target scope
+ relevant workspace revision
+ task contract revision
+ validation purpose
```

Counterexamples (must **not** treat as over-SOP):

- Code changed after last validation → re-test allowed  
- User asked flaky investigation → repeated runs allowed  
- High-risk security change → short prompt ≠ trivial  
- Repository policy requires docs lint → still allowed  

---

## 9. M2 vs M2.1 backlog

| Track | Focus | Issues |
|-------|--------|--------|
| **M2.0** | Direction fixation control loop | #80, #69, #82, #70, #71 |
| **M2.1** | Effort calibration / verification_churn | TaskContract+Ledger protocol, TaskSubmitted intake, SOP-churn detector, effort-calibration slice |

---

## 10. Related packages (current main)

- `pkg/protocol` — schemas, capabilities, task model types  
- `pkg/state` — event store  
- `pkg/adapter` — EventSource, Actuator, HookGate, AdvisoryDelivery, LogObserver  
- `pkg/reviewer` — ReviewerProvider + Fake (optional stage)  
- `pkg/config` — versioned config schema  
