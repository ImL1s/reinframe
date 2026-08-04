package workspace_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/workspace"
)

func initRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "test@example.com")
	run("git", "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", "README")
	run("git", "commit", "-m", "init")
}

func TestManagedWorktree_CheckpointRollback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, err := workspace.NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := reg.CreateWorktree("sess-1", base, "HEAD")
	if err != nil {
		// some git versions need branch; try without -b
		t.Fatalf("create: %v", err)
	}
	// Ensure clean
	rec, pc, err := reg.Checkpoint(wt, "before edit")
	if err != nil {
		t.Fatal(err)
	}
	if pc.GitCommitHash == "" || rec.HEAD == "" {
		t.Fatal("empty head")
	}
	// Mutate then make clean commit so we can reset
	if err := os.WriteFile(filepath.Join(wt.Path, "README"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "README")
	cmd.Dir = wt.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	cmd = exec.Command("git", "commit", "-m", "v2")
	cmd.Dir = wt.Path
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(out), err)
	}
	// Rollback to checkpoint
	rb, err := reg.Rollback(wt, rec)
	if err != nil || !rb.Success {
		t.Fatalf("rollback: %+v err=%v", rb, err)
	}
	if rb.RestoredCommitHash != rec.HEAD {
		t.Fatalf("restored=%s want %s", rb.RestoredCommitHash, rec.HEAD)
	}
	b, _ := os.ReadFile(filepath.Join(wt.Path, "README"))
	// Normalize CRLF (Windows git autocrlf) before compare.
	if strings.TrimSpace(string(b)) != "v1" {
		t.Fatalf("content=%q", b)
	}
}

func TestManagedWorktree_RejectOutsideRoot(t *testing.T) {
	root := t.TempDir()
	reg, err := workspace.NewRegistry(root)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	_, err = reg.AdoptExisting("s", outside)
	if err == nil {
		t.Fatal("expected outside root reject")
	}
}

func TestManagedWorktree_DirtyCheckpointFailClosed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("s", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt.Path, "dirty.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := reg.Checkpoint(wt, "dirty"); err == nil {
		t.Fatal("expected dirty fail closed")
	}
}

func TestManagedWorktree_SessionMismatch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("sess-a", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := reg.Checkpoint(wt, "ok")
	if err != nil {
		t.Fatal(err)
	}
	wt.SessionID = "sess-b"
	if _, err := reg.Rollback(wt, rec); err == nil {
		t.Fatal("expected session mismatch")
	}
}

func TestManagedWorktree_SymlinkRootFailClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary")
	}
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip(err)
	}
	if _, err := workspace.NewRegistry(link); err == nil {
		t.Fatal("expected symlink root fail closed")
	}
}

func TestManagedWorktree_UntrackedAfterCheckpointFailsRollback(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("s", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := reg.Checkpoint(wt, "ok")
	if err != nil {
		t.Fatal(err)
	}
	// New untracked file after checkpoint — clean-only policy fails closed on rollback.
	if err := os.WriteFile(filepath.Join(wt.Path, "ghost.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Rollback(wt, rec); err == nil {
		t.Fatal("expected untracked drift fail closed")
	}
}

func TestManagedWorktree_PrimaryCheckoutRejectedAsManaged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	// Primary checkout = base repo outside managed root cannot be Adopted.
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	if _, err := reg.AdoptExisting("s", base); err == nil {
		t.Fatal("primary/outside must be rejected")
	}
}

func TestLoadCheckpoint(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("s", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := reg.Checkpoint(wt, "load")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := reg.LoadCheckpoint(wt.ID, rec.CheckpointID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.HEAD != rec.HEAD {
		t.Fatal(loaded.HEAD)
	}
}

func TestManagedWorktree_MarkerSessionMismatchFailClosed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("sess-ok", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	// Tamper session on in-memory record while marker still says sess-ok.
	wt.SessionID = "sess-evil"
	if _, _, err := reg.Checkpoint(wt, "should fail"); err == nil {
		t.Fatal("expected marker session mismatch")
	}
}

func TestManagedWorktree_CheckpointNotInAgentTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git required")
	}
	base := t.TempDir()
	initRepo(t, base)
	root := t.TempDir()
	reg, _ := workspace.NewRegistry(root)
	wt, err := reg.CreateWorktree("s", base, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	rec, _, err := reg.Checkpoint(wt, "private")
	if err != nil {
		t.Fatal(err)
	}
	// Must not live under agent worktree path.
	agentPath := filepath.Join(wt.Path, ".reinframe-checkpoints", rec.CheckpointID+".json")
	if _, err := os.Stat(agentPath); err == nil {
		t.Fatal("checkpoint must not be inside agent-writable worktree")
	}
	// Must load from private store.
	if _, err := reg.LoadCheckpoint(wt.ID, rec.CheckpointID); err != nil {
		t.Fatal(err)
	}
}
