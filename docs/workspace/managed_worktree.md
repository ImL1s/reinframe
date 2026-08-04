# Managed worktree checkpoint / rollback (#99)

**Policy:** clean-only checkpoint; hard-reset restore; never primary checkout by default.

## Ownership

- Worktrees live only under configured managed root.
- `.reinframe-managed-worktree` marker required.
- Git common-dir identity must match checkpoint.

## Checkpoint

Requires clean worktree (`git status --porcelain` empty).  
Records HEAD, branch, digests, session/worktree IDs.

## Rollback

1. Revalidate ownership + marker + common-dir + session  
2. Reject dirty/untracked drift (clean-only)  
3. `git reset --hard <checkpoint HEAD>`  
4. Verify HEAD  

## Non-claims

- No remote push / force-push / history rewrite  
- No rollback of external side effects (DB, deploy, email)  
- No silent discard of untracked agent files under dirty trees  

## API

```go
reg, _ := workspace.NewRegistry(managedRoot)
wt, _ := reg.CreateWorktree(sessionID, baseRepo, "HEAD")
rec, protoChk, _ := reg.Checkpoint(wt, "before risk")
rb, _ := reg.Rollback(wt, rec)
```
