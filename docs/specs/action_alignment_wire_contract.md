# Action Alignment — implementation wire contract (#119)

**Status:** normative wire contract (2026-08-04)  
**Builds on:** `action_alignment_classifier.md` (#104 concept)  
**Blocks:** #105 shadow runtime  
**Depends on merge:** #115 `ProposedAction` (`reinframe.proposed_action.v1`)

## 1. Observed fact / inference / design choice

| Item | Class | Note |
|------|-------|------|
| Captured Claude Auto Mode Stage 1 request shape (rules + curated context + proposed action → raw score) | **Observed fact** | Architecture pattern only; not a policy to copy |
| Claude Stage 2 resolver implementation | **UNKNOWN** | Not public; do not claim second LLM call |
| Stage 2 public outcome `ALLOW \| BLOCK` | **Reinframe design** | Model severity is evidence only |
| HookGate `defer` | **Reinframe design** | Transport latch; not Stage 2 enum |
| Severity integer 0–100 | **Reinframe design** | Not a probability |
| Provisional threshold 50 | **Reinframe design** | **Not calibration** (#100 + promotion) |

## 2. Versioned closed schemas

### ProposedAction

Canonical reference: `pkg/adapter/proposed_action.go` / `docs/adapter/proposed_action.md`  
Schema: `reinframe.proposed_action.v1`

### ClassifierInput — `reinframe.classifier_input.v1`

```json
{
  "schema_version": "reinframe.classifier_input.v1",
  "task_anchor": { "task_id": "", "objective": "", "session_id": "" },
  "contract_revision": 0,
  "evidence_revision": 0,
  "proposed_action": { "$ref": "reinframe.proposed_action.v1" },
  "recent_event_ids": [],
  "related_event_ids": [],
  "ruleset_id": "",
  "ruleset_hash": "",
  "policy_class": "PRODUCTIVITY",
  "window": {
    "event_count": 0,
    "byte_count": 0,
    "truncated": false,
    "overflow_marker": ""
  }
}
```

### RawAssessment — `reinframe.raw_assessment.v1`

```json
{
  "schema_version": "reinframe.raw_assessment.v1",
  "severity": 0,
  "reason_code": "NORMAL_PROGRESS",
  "evidence_event_ids": [],
  "model_id": "",
  "model_version": "",
  "prompt_hash": "",
  "ruleset_id": "",
  "ruleset_hash": "",
  "parse_status": "ok",
  "latency_ms": 0
}
```

- `severity` ∈ integers **0–100** inclusive (not probability)
- Extra fields / NaN / multi-object JSON → `parse_status=invalid`
- Unknown `reason_code` → invalid

### ResolvedDecision — `reinframe.resolved_decision.v1`

```json
{
  "schema_version": "reinframe.resolved_decision.v1",
  "decision": "ALLOW",
  "raw_severity": 0,
  "threshold": 50,
  "reason_code": "below_threshold",
  "resolver_reason": "stage1_applied",
  "feedback": "",
  "enforced": false,
  "profile_id": "provisional-default"
}
```

- `decision` ∈ {`ALLOW`,`BLOCK`} only  
- Shadow (#105): `enforced` always `false`

### Audit record — `reinframe.classifier_audit.v1`

```json
{
  "schema_version": "reinframe.classifier_audit.v1",
  "session_id": "",
  "action_id": "",
  "input_hash": "",
  "raw": {},
  "resolved": {},
  "hookgate_action": "",
  "disagreement": false,
  "created_at": ""
}
```

## 3. Reason-code closed allowlist (Stage 1 raw)

```
NORMAL_PROGRESS
SCOPE_DRIFT
VERIFICATION_CHURN
REPEATED_FAILURE
HYPOTHESIS_LOOP
TOOL_BUDGET
EVIDENCE_GAP
OVER_SOP
UNKNOWN
```

Stage 2 resolver reasons (separate allowlist):

```
stage0_block
stage0_skip
stage1_applied
below_threshold
above_threshold
user_exception
repo_policy_exception
flaky_investigation
fail_open_productivity
fail_closed_security
parse_invalid
provider_unavailable
```

## 4. EvidenceEventIDs validation

- Every ID must appear in ClassifierInput recent or related sets  
- Empty list allowed only for `NORMAL_PROGRESS` and Stage 0 deterministic assessments  
- Duplicates rejected  
- Max 32 IDs  

## 5. Event-window algorithm

1. Pin Task Anchor (objective + explicit user boundaries) — never drop  
2. Include current ProposedAction  
3. Recent relevant events since last contract revision, cap **N events** and **B bytes** (defaults N=40, B=48KiB)  
4. Same-fingerprint / same-action history (bounded)  
5. Deterministic order: ascending (session_id, sequence_num, event_id)  
6. On overflow: set `window.truncated=true`, `overflow_marker=events_or_bytes`  
7. Prefer canonical AgentEvent digests over raw transcripts; no unrestricted private CoT  

## 6. Provider abstraction decision (ADR)

**Decision:** introduce a narrow **`ClassifierProvider`** interface in `pkg/classifier` (new package), **not** a silent reuse of `ReviewerProvider` without an adapter.

Rationale:

- Reviewer returns intervention-oriented `SuggestedAdvice` / ZOOM_OUT for uncertain slow path  
- Classifier returns closed `RawAssessment` with severity + reason_code  
- An optional adapter may wrap ReviewerProvider for experiments, but #105 default fake is `FakeClassifierProvider`  

ADR file: `docs/adr/005-classifier-provider.md`

## 7. Prompt / model / version / ruleset / hash audit

RawAssessment and Audit must record:

- `model_id`, `model_version`  
- `prompt_hash` (hash of rendered prompt template + ruleset)  
- `ruleset_id`, `ruleset_hash`  
- `latency_ms`  

## 8. Failure matrix

| Failure | PRODUCTIVITY | SECURITY |
|---------|--------------|----------|
| timeout | fail-open → ALLOW (log) | fail-closed → BLOCK or existing hard deny |
| provider unavailable | fail-open → ALLOW (log) | fail-closed |
| parse invalid | fail-open → ALLOW + parse_status | fail-closed |
| oversize input | truncate window + marker; no provider call if still oversize → fail-open productivity | fail-closed |
| Stage 0 hard deny | N/A (Stage 0 wins) | Stage 0 wins |

Security class is a future track; productivity classifier must not claim security authority.

## 9. Golden fixtures (paths for #105)

Under `testdata/classifier/` (to be added with #105 runtime; names locked here):

1. `clear_allow.json`  
2. `clear_block.json`  
3. `user_exception.json`  
4. `repo_policy_exception.json`  
5. `flaky_investigation.json`  
6. `healthy_deep_security_work.json`  
7. `objective_outside_recent_tail.json`  
8. `contradictory_related_evidence.json`  
9. `malformed_output.json`  

Fake deterministic provider maps fixture → fixed RawAssessment.

## 10. Package location

```text
pkg/classifier/          # types, window builder, stage0/1/2, fake provider (#105)
docs/specs/              # this contract + action_alignment_classifier.md
docs/adr/005-...md       # provider decision
```

## 11. Non-claims

- Threshold 50 is **provisional**, not calibrated  
- Stage 2 is **not** necessarily a second LLM call (UNKNOWN for Claude; Reinframe Stage 2 is deterministic)  
- #105 is shadow only (`enforced=false`)  
- No hard-gate without #100 + separate promotion issue  
