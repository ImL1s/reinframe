// Package workspace implements supervisor-owned managed git worktrees (#99).
//
// Checkpoint/rollback operate only under a configured managed root and never
// target the user's primary checkout by default. Dirty/untracked drift fails
// closed. See docs/adr/004_workspace_worktree_isolation.md.
package workspace
