# Appealable BLOCK challenges (#131) — host-neutral core

**Status:** implemented (library)
**Issue:** #131
**Parent:** Epic #80

## Decision invariant

```text
Classifier / resolver Stage 2:  ALLOW | BLOCK
Intervention metadata:         none | APPEALABLE_CHALLENGE | HUMAN_REVIEW
```

`CHALLENGE` is **not** a Stage 2 decision. It is workflow metadata attached to an
appealable productivity `BLOCK`.

## Package

`pkg/challenge` provides:

- closed versioned `ChallengeRecord` / `Justification` / event / audit schemas
- durable one-shot `RetryBudget` (initial **1**)
- append-only events + replay
- semantic action fingerprint from canonical `ProposedAction`
- justification validation (bounded fields, evidence IDs, injection resistance)
- re-evaluation: hard rules → contract/evidence context → optional classifier → Stage 2
- cache **identity inputs** only (exact-assessment cache is **#138**, not here)

## Justification is not permission

A valid justification is a **bounded external decision summary**. It is new
evidence for re-evaluation. It **never** automatically flips `BLOCK` → `ALLOW`.
Reinframe does **not** request, parse, or store private chain-of-thought.

Closed fields only:

```text
challenge_id, concrete_value, prevented_failure_or_threat, estimated_cost,
alternatives_considered, scope_limit, verification_plan, rollback_plan,
supporting_evidence_event_ids
```

## Appealability (default)

| Block class | Behavior |
|-------------|----------|
| Scope drift, over-SOP, expensive hardening, exploration | One bounded appeal |
| Evidence gap | Appeal with evidence IDs + verification plan |
| Secret exfiltration, explicit deny, cross-workspace | Non-appealable hard block |
| Production deploy, payment, remote deletion, permission change | `HUMAN_REVIEW` |
| Unknown security | Fail closed / human review; no self-appeal |

## Semantic fingerprint

Built from tool class, side-effect class, normalized targets, session/branch,
workspace and contract revisions — **not** raw command text alone.

- Syntax-only rewrites with the same effect remain bound to the challenge
  (e.g. `rm -rf build` ≈ `find build -delete`).
- Genuine reduced scope is a separate evaluable relationship (not a free bypass).

## Non-claims

- **No** live Claude appeal delivery (`additionalContext`) — that is **#139**
- **No** live smoke (#120) or advice consumer (#108)
- **No** provider runtime (#132) or native adapters (#134–#137)
- **No** exact-assessment cache implementation (#138)
- **No** hard-gate promotion
- **No** model self-permission / third Stage 2 decision

## State machine (summary)

```text
OPEN → JUSTIFIED → RETRY_PENDING → ALLOWED_ONCE | REJECTED | HUMAN_REVIEW
OPEN → REJECTED (retry without justification)
OPEN → ABANDONED | EXPIRED
```

Expired challenges cannot be revived by replay.
