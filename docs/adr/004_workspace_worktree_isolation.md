# ADR 004: Workspace Worktree Isolation & Checkpoint Rollback

- **Status**: Decided & Approved
- **Date**: 2026-08-02
- **Deciders**: Architecture Team
- **Technical Story**: Issue #51

---

## Context and Problem Statement
Supervised coding agents write files, run tools, and may attempt to escape the assigned workspace. Reinframe also needs a reliable rollback path when intervention level B3 (full-control) or checkpoint restore is required.

Without a single owner for worktree lifecycle, agents and the supervisor can race on git state, leave half-applied patches, or write outside the intended project tree.

---

## Decision Drivers
- Clear ownership of managed git worktrees.
- Contain agent filesystem writes to a known root (scope drift detection, #26-class detectors).
- Rollback must be deterministic and operator-understandable (no opaque snapshot formats for MVP).
- Align with ADR 001 (external OS process supervisor) and non-goals (no remote/cloud workspace state).

---

## Considered Options

### Option A: Agent owns its worktree; supervisor only observes
- **Pros**: Minimal supervisor git surface.
- **Cons**: Cannot guarantee isolation; rollback competes with agent git operations.

### Option B: Supervisor owns managed worktree; agent writes only inside; rollback = git reset to checkpoint (Selected)
- **Pros**: Single owner; rollback maps to standard git; isolation is enforceable via path policy / hook gate.
- **Cons**: Supervisor must create/bind worktrees and record checkpoint refs.

### Option C: Full VM/container filesystem snapshots per session
- **Pros**: Strong isolation and restore.
- **Cons**: Heavy for a local supervisor; out of MVP scope.

---

## Decision Outcome
**Selected Option**: **Option B**.

### Normative rules
1. **Supervisor owns the managed worktree**: create, bind, and tear down of the session worktree (or bound project directory) is the supervisor's responsibility. The target agent is a guest writer inside that tree.
2. **Agent writes only inside the managed root**: tool and path policies (hook gate scope whitelist, future path sandbox) MUST treat paths outside the managed worktree as denied / out-of-scope. Config flag `workspace.enforce_isolation` defaults toward enforcement when the control plane is active.
3. **Checkpoint model**: a checkpoint records a git ref (commit SHA and/or lightweight ref) plus metadata (`Checkpoint` protocol type). The supervisor creates checkpoints at safe points (e.g. before risky multi-file edits or on SUSPECT entry).
4. **Rollback = git reset to checkpoint**: default restore is `git reset --hard <checkpoint-sha>` (and clean untracked when policy requires) inside the managed worktree. No remote push/force as part of rollback. Result surfaces as `RollbackResult`.
5. **No agent authority over supervisor git bookkeeping**: agents MUST NOT move supervisor-managed checkpoint refs or delete the worktree directory. Isolation failures are audit events, not silent success.

### Consequences
- **Positive**: Simple mental model; leverages existing git; fits B3 rollback and #10 checkpoint manager design.
- **Negative**: Dirty worktrees with uncommitted intentional WIP need explicit checkpoint policy; submodule/LFS edge cases deferred.
- **Mitigation**: Document checkpoint cadence; fail closed on rollback when worktree is not a git repo or ref is missing.
