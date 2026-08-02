# Anti-Tunnel Threat Model & Failure Mode Taxonomy

## 1. Overview
Long-horizon autonomous coding agents experience cognitive lock-in, patch churn, and assumption decay.
This threat model defines the six core agent failure modes, maps deterministic and model-assisted detection signals, and sets intervention confidence thresholds.

---

## 2. Failure Mode Taxonomy

### FM-1: Repeated Error Loop Fixation
- **Description**: Target agent encounters an error (compiler failure, test failure, runtime exception) and attempts near-identical code edits without resolving the underlying cause.
- **Root Cause**: Shallow diagnostic reasoning; fixing symptom strings rather than tracing root call stack.
- **Deterministic Signals**: Repeated error fingerprint count $\ge N$, file modification overlap $> 80\%$.
- **Model-Assisted Signals**: High confidence from Tunnel Classifier indicating identical proposed edits.

### FM-2: Wrong Root Cause Locking
- **Description**: Target agent misattributes a failure (e.g. blaming business logic for an uninitialized environment variable or missing package dependency) and edits unrelated code.
- **Root Cause**: Premature diagnostic hypothesis formation without checking environment logs.
- **Deterministic Signals**: Touch count on business logic files increases while test failures remain fixed on missing setup.
- **Model-Assisted Signals**: Assumption Auditor finds zero empirical evidence for agent's primary diagnostic claim.

### FM-3: Patch Churn & Progress Stagnation
- **Description**: Agent continually modifies, undoes, or refactors the same set of files without improving test pass rate or net functionality.
- **Root Cause**: Trial-and-error programming; loss of strategic architectural vision.
- **Deterministic Signals**: High line churn ratio ($> 300$ lines edited for $< 10$ net lines changed), zero change in passing test count across 4 consecutive turns.
- **Model-Assisted Signals**: Evidence Verifier reports net zero functional gain despite multi-turn diffs.

### FM-4: Scope Drift & Unbounded Refactoring
- **Description**: Agent assigned to fix a specific bug begins refactoring unrelated modules, rewriting build scripts, or rearchitecting database schemas.
- **Root Cause**: Goal creep; unconstrained tool invocation.
- **Deterministic Signals**: File modifications touch paths outside pre-approved target scope whitelist.
- **Model-Assisted Signals**: Scope Drift detector flags mismatch between issue acceptance criteria and current file diffs.

### FM-5: Ignoring Contradictory Empirical Evidence
- **Description**: Empirical test output or log output explicitly refutes the agent's working hypothesis, but the agent continues executing based on the refuted assumption.
- **Root Cause**: Confirmation bias; persuasive narrative self-reinforcement in agent memory context.
- **Deterministic Signals**: Agent re-introduces previously failed code snippets or discredited hypotheses.
- **Model-Assisted Signals**: Contrarian Reviewer identifies explicit contradiction between test logs and agent prompt assertions.

### FM-6: False Positive Deep Engineering Work (Negative Control)
- **Description**: Agent is legitimately engaged in complex multi-file architectural refactoring or deep algorithm implementation.
- **Risk**: Supervisor mistakenly interrupts healthy deep work, causing developer frustration and wasted tokens.
- **Suppression Signals**: Test pass rate is increasing, new unit tests are being added, file edits touch distinct files linearly without error repetition.

---

## 3. Signal Scoring & Hybrid Evaluation

$$\text{Confidence Score} = w_1 \cdot S_{\text{error\_count}} + w_2 \cdot S_{\text{patch\_churn}} + w_3 \cdot S_{\text{scope\_drift}} + w_4 \cdot S_{\text{reviewer\_assessment}}$$

| Signal Type | Weight ($w_i$) | Threshold | Action |
|---|---|---|---|
| Repeated Error Fingerprint | 0.35 | Count $\ge 3$ | Triggers SUSPECT state |
| Patch Churn Ratio | 0.25 | Churn $> 4.0$ & Pass Delta $= 0$ | Triggers SUSPECT state |
| Scope Drift Warning | 0.15 | Touch outside scope whitelist | Log warning / Advisory |
| Reviewer Tunnel Classification | 0.25 | Confidence $> 0.85$ | Triggers ZOOM_OUT / REPLAN |

### 3.1 Normative status of weights & thresholds (2026-08-02)
- Values above are **provisional design defaults**, **not** calibrated hard-gates.
- **No** ROC / false-positive dataset is checked into this repo yet (#40 / #41 / #74).
- Implementations MUST expose weights/thresholds as **config knobs** until evaluation evidence exists.
- Open contract gaps (must close before claiming reproducibility):
  - Exact normalization of each $S_*$ from raw measurements (fingerprint identity, churn formula, scope whitelist source).
  - FM-1 prose “$\ge N$” vs table “Count $\ge 3$” — **table wins as default N=3** until config overrides.
  - How per-signal `weight` fields in `TunnelSignal` combine with global $w_i$ table.

### 3.2 Dual Level axes
Intervention steps below are **Axis B (escalation)**.  
They are **not** the same as handshake **Axis A (Integration Level)**.  
Crosswalk: [`level_axes_mapping.md`](./level_axes_mapping.md).

---

## 4. Intervention Escalation Ladder (Axis B)

1. **B0 Observe**: Record event to SQLite WAL audit store.
2. **B1 Advisory**: Inject Evidence Pack summary and request re-planning (`ZOOM_OUT` / replan advice) with delivery+ACK (#68).
3. **B2 Guarded**: Pause or tool-defer execution; require discriminating experiment when available.
4. **B3 Full-control**: Rollback workspace Git checkpoint; **switch model family**; **escalate to human**; or terminate session.

### 4.1 Schema coverage note
Current `intervention.action_type` / `recommended_action` enums cover advisory zoom-out, pause, cancel, git-rollback, terminate-class actions more clearly than **switch model** / **escalate to human**. Those B3 actions remain product requirements and must land in #57 schema extensions before orchestrator claims B3 completeness.
