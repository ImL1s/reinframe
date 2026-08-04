# Managed worktree checkpoint / rollback (#99)

**Policy:** clean-only checkpoint; hard-reset restore; never primary checkout by default.

## Ownership

- Worktrees live only under configured managed root.
- `.reinframe-managed-worktree` marker required.
- Git common-dir identity must match checkpoint.

## Checkpoint

Requires clean worktree:

```bash
git status --porcelain --untracked-files=all
```

(must be empty). Nested untracked directories are not summarized away.

Records HEAD, branch, digests, session/worktree IDs, generation.

**Storage:** checkpoint JSON lives under the **managed root private store**, not the agent worktree:

```text
{managedRoot}/.reinframe-private/checkpoints/{worktreeID}/{checkpointID}.json
```

## Ownership checks

`validateOwned` requires:

- path under managed root after `EvalSymlinks` canonicalize
- path itself not a symlink
- marker file present with **matching** `id` and `session_id`

## Rollback

1. Revalidate ownership + marker id/session + common-dir + generation  
2. Reject dirty/untracked drift (clean-only, untracked-files=all)  
3. Verify checkpoint HEAD is a commit object  
4. `git reset --hard <commit>`  
5. Verify HEAD  

## Non-claims

- No remote push / force-push / history rewrite  
- No rollback of external side effects (DB, deploy, email)  
- No silent discard of untracked agent files under dirty trees  
- No primary-checkout mutation by default  

## API

```go
reg, _ := workspace.NewRegistry(managedRoot)
wt, _ := reg.CreateWorktree(sessionID, baseRepo, "HEAD")
rec, protoChk, _ := reg.Checkpoint(wt, "before risk")
loaded, _ := reg.LoadCheckpoint(wt.ID, rec.CheckpointID)
rb, _ := reg.Rollback(wt, rec)
```
