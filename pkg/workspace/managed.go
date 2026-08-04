package workspace

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

const (
	// CheckpointRecordSchema is the durable checkpoint record version.
	CheckpointRecordSchema = "reinframe.managed_checkpoint.v1"
	// RegistrySchema is the in-memory/on-disk registry version.
	RegistrySchema = "reinframe.worktree_registry.v1"
	// MarkerFile is written inside managed worktrees for ownership proof.
	MarkerFile = ".reinframe-managed-worktree"
)

// Registry tracks Reinframe-owned managed worktrees under Root.
type Registry struct {
	// Root is the absolute managed-worktree root (required).
	Root string
	// Now for tests.
	Now func() time.Time

	mu    sync.Mutex
	trees map[string]*ManagedWorktree // id → meta
}

// ManagedWorktree is a supervisor-owned worktree record.
type ManagedWorktree struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	Path         string    `json:"path"` // canonical absolute path
	GitCommonDir string    `json:"git_common_dir"`
	Generation   int       `json:"generation"`
	CreatedAt    time.Time `json:"created_at"`
}

// CheckpointRecord is the durable checkpoint metadata (#99).
type CheckpointRecord struct {
	SchemaVersion      string    `json:"schema_version"`
	CheckpointID       string    `json:"checkpoint_id"`
	SessionID          string    `json:"session_id"`
	ManagedWorktreeID  string    `json:"managed_worktree_id"`
	Generation         int       `json:"generation"`
	GitCommonDir       string    `json:"git_common_dir"`
	WorktreePath       string    `json:"worktree_path"`
	HEAD               string    `json:"head"`
	Branch             string    `json:"branch,omitempty"`
	IndexDigest        string    `json:"index_digest,omitempty"`
	TrackedDigest      string    `json:"tracked_digest,omitempty"`
	UntrackedPolicy    string    `json:"untracked_policy"` // clean-only
	CreatedAt          time.Time `json:"created_at"`
	Reason             string    `json:"reason,omitempty"`
	RollbackCapability string    `json:"rollback_capability"` // hard-reset-clean
	// PrimaryCheckoutDenied is always true for default safety.
	PrimaryCheckoutDenied bool `json:"primary_checkout_denied"`
}

// NewRegistry builds a registry for managedRoot (must be absolute).
func NewRegistry(managedRoot string) (*Registry, error) {
	if managedRoot == "" {
		return nil, fmt.Errorf("workspace: ManagedWorktreeRoot required")
	}
	abs, err := filepath.Abs(managedRoot)
	if err != nil {
		return nil, err
	}
	// Refuse if root is symlink (fail closed).
	if fi, err := os.Lstat(abs); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("workspace: managed root is symlink (fail closed)")
	}
	return &Registry{
		Root:  abs,
		Now:   func() time.Time { return time.Now().UTC() },
		trees: make(map[string]*ManagedWorktree),
	}, nil
}

// CreateWorktree creates a new git worktree under Root for sessionID from basePath repo.
// basePath is the main repository (not mutated). Worktree path is under Root only.
func (r *Registry) CreateWorktree(sessionID, baseRepo, branch string) (*ManagedWorktree, error) {
	if sessionID == "" || baseRepo == "" {
		return nil, fmt.Errorf("workspace: sessionID and baseRepo required")
	}
	baseAbs, err := filepath.Abs(baseRepo)
	if err != nil {
		return nil, err
	}
	// baseAbs is source repository only (never the managed target).
	if err := requireGitRepo(baseAbs); err != nil {
		return nil, err
	}
	id := shortID(sessionID + r.now().String())
	dest := filepath.Join(r.Root, id)
	if err := os.MkdirAll(r.Root, 0o755); err != nil {
		return nil, err
	}
	// git worktree add (detached from base HEAD or start-point)
	start := branch
	if start == "" {
		start = "HEAD"
	}
	args := []string{"worktree", "add", "--detach", dest, start}
	if out, err := git(baseAbs, args...); err != nil {
		return nil, fmt.Errorf("git worktree add: %w (%s)", err, out)
	}
	common, err := gitCommonDir(dest)
	if err != nil {
		_ = removeWorktree(baseAbs, dest)
		return nil, err
	}
	if err := writeMarker(dest, id, sessionID); err != nil {
		_ = removeWorktree(baseAbs, dest)
		return nil, err
	}
	// Ensure checkpoint store is gitignored without wiping existing .gitignore.
	if err := ensureGitignoreLine(dest, ".reinframe-checkpoints/"); err != nil {
		_ = removeWorktree(baseAbs, dest)
		return nil, err
	}
	// Commit marker + gitignore so clean-only checkpoint policy is satisfiable.
	if out, err := git(dest, "add", MarkerFile, ".gitignore"); err != nil {
		_ = removeWorktree(baseAbs, dest)
		return nil, fmt.Errorf("git add marker: %w (%s)", err, out)
	}
	if out, err := git(dest, "-c", "user.email=reinframe@local", "-c", "user.name=reinframe",
		"commit", "-m", "reinframe: managed worktree marker"); err != nil {
		_ = removeWorktree(baseAbs, dest)
		return nil, fmt.Errorf("git commit marker: %w (%s)", err, out)
	}
	wt := &ManagedWorktree{
		ID: id, SessionID: sessionID, Path: dest, GitCommonDir: common,
		Generation: 1, CreatedAt: r.now(),
	}
	r.mu.Lock()
	r.trees[id] = wt
	r.mu.Unlock()
	return wt, nil
}

// AdoptExisting registers an existing path only if it is under Root and has a marker.
func (r *Registry) AdoptExisting(sessionID, path string) (*ManagedWorktree, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := r.underRoot(abs); err != nil {
		return nil, err
	}
	if err := r.rejectSymlink(abs); err != nil {
		return nil, err
	}
	meta, err := readMarker(abs)
	if err != nil {
		return nil, fmt.Errorf("workspace: not reinframe-owned (missing marker): %w", err)
	}
	if meta.SessionID != "" && sessionID != "" && meta.SessionID != sessionID {
		return nil, fmt.Errorf("workspace: session mismatch")
	}
	common, err := gitCommonDir(abs)
	if err != nil {
		return nil, err
	}
	wt := &ManagedWorktree{
		ID: meta.ID, SessionID: sessionID, Path: abs, GitCommonDir: common,
		Generation: 1, CreatedAt: r.now(),
	}
	if wt.ID == "" {
		wt.ID = shortID(abs)
	}
	r.mu.Lock()
	r.trees[wt.ID] = wt
	r.mu.Unlock()
	return wt, nil
}

// Checkpoint creates a clean-tree checkpoint (policy: clean-only).
// Fails closed if dirty/untracked present.
func (r *Registry) Checkpoint(wt *ManagedWorktree, reason string) (*CheckpointRecord, *protocol.Checkpoint, error) {
	if err := r.validateOwned(wt); err != nil {
		return nil, nil, err
	}
	if err := requireClean(wt.Path); err != nil {
		return nil, nil, err
	}
	head, err := gitOutput(wt.Path, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	branch, _ := gitOutput(wt.Path, "rev-parse", "--abbrev-ref", "HEAD")
	idx, _ := gitOutput(wt.Path, "write-tree")
	tracked, _ := gitOutput(wt.Path, "rev-parse", "HEAD")
	id := "chk-" + shortID(head+wt.ID+r.now().String())
	rec := &CheckpointRecord{
		SchemaVersion:         CheckpointRecordSchema,
		CheckpointID:          id,
		SessionID:             wt.SessionID,
		ManagedWorktreeID:     wt.ID,
		Generation:            wt.Generation,
		GitCommonDir:          wt.GitCommonDir,
		WorktreePath:          wt.Path,
		HEAD:                  head,
		Branch:                branch,
		IndexDigest:           hashStr(idx),
		TrackedDigest:         hashStr(tracked),
		UntrackedPolicy:       "clean-only",
		CreatedAt:             r.now(),
		Reason:                reason,
		RollbackCapability:    "hard-reset-clean",
		PrimaryCheckoutDenied: true,
	}
	// Persist next to worktree
	if err := writeJSON(filepath.Join(wt.Path, ".reinframe-checkpoints", id+".json"), rec); err != nil {
		return nil, nil, err
	}
	pc := &protocol.Checkpoint{
		CheckpointID:  id,
		SessionID:     wt.SessionID,
		GitCommitHash: head,
		BranchName:    branch,
		Description:   reason,
		CreatedAt:     rec.CreatedAt,
	}
	return rec, pc, nil
}

// RollbackResultStatus extends protocol with explicit outcomes.
const (
	RollbackOK               = "success"
	RollbackRejected         = "rejected"
	RollbackRecoveryRequired = "recovery_required"
	RollbackVerifyFailed     = "verification_failed"
)

// Rollback hard-resets the managed worktree to checkpoint HEAD if still clean-owned.
func (r *Registry) Rollback(wt *ManagedWorktree, rec *CheckpointRecord) (*protocol.RollbackResult, error) {
	res := &protocol.RollbackResult{
		RollbackID:         "rb-" + shortID(r.now().String()),
		SessionID:          wt.SessionID,
		TargetCheckpointID: rec.CheckpointID,
		CompletedAt:        r.now(),
	}
	if err := r.validateOwned(wt); err != nil {
		res.Success = false
		res.ErrorMessage = err.Error()
		return res, err
	}
	if rec.ManagedWorktreeID != wt.ID {
		res.Success = false
		res.ErrorMessage = "checkpoint worktree mismatch"
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	if rec.SessionID == "" || wt.SessionID == "" || rec.SessionID != wt.SessionID {
		res.Success = false
		res.ErrorMessage = "checkpoint session mismatch (fail closed)"
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	if rec.Generation != 0 && wt.Generation != 0 && rec.Generation != wt.Generation {
		res.Success = false
		res.ErrorMessage = "worktree generation mismatch"
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	common, err := gitCommonDir(wt.Path)
	if err != nil {
		res.Success = false
		res.ErrorMessage = err.Error()
		return res, err
	}
	if common != rec.GitCommonDir && rec.GitCommonDir != "" {
		res.Success = false
		res.ErrorMessage = "git common-dir identity mismatch"
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	// Fail closed on unknown dirty drift before mutate
	if err := requireClean(wt.Path); err != nil {
		// Allow dirty only if we will reset hard — but issue says unknown untracked fail closed under clean-only.
		// For clean-only policy, reject dirty/untracked.
		res.Success = false
		res.ErrorMessage = "dirty or untracked drift (fail closed): " + err.Error()
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	prev, _ := gitOutput(wt.Path, "rev-parse", "HEAD")
	res.PreviousCommitHash = prev
	// Require full commit object id before mutate.
	commit, err := gitOutput(wt.Path, "rev-parse", "--verify", rec.HEAD+"^{commit}")
	if err != nil || commit == "" {
		res.Success = false
		res.ErrorMessage = "checkpoint HEAD is not a commit object"
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	if out, err := git(wt.Path, "reset", "--hard", commit); err != nil {
		res.Success = false
		res.ErrorMessage = fmt.Sprintf("reset failed: %v (%s)", err, out)
		return res, fmt.Errorf("workspace: %s", res.ErrorMessage)
	}
	// Verify
	head, err := gitOutput(wt.Path, "rev-parse", "HEAD")
	if err != nil || head != rec.HEAD {
		res.Success = false
		res.ErrorMessage = "verification failed after reset"
		res.RestoredCommitHash = head
		return res, fmt.Errorf("workspace: verification failed")
	}
	res.RestoredCommitHash = head
	res.Success = true
	return res, nil
}

// LoadCheckpoint reads a checkpoint JSON from the worktree store.
func LoadCheckpoint(wtPath, checkpointID string) (*CheckpointRecord, error) {
	p := filepath.Join(wtPath, ".reinframe-checkpoints", checkpointID+".json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var rec CheckpointRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return nil, err
	}
	if rec.SchemaVersion != CheckpointRecordSchema {
		return nil, fmt.Errorf("workspace: checkpoint schema %q", rec.SchemaVersion)
	}
	return &rec, nil
}

func (r *Registry) validateOwned(wt *ManagedWorktree) error {
	if wt == nil {
		return fmt.Errorf("workspace: nil worktree")
	}
	if err := r.underRoot(wt.Path); err != nil {
		return err
	}
	if err := r.rejectSymlink(wt.Path); err != nil {
		return err
	}
	if _, err := readMarker(wt.Path); err != nil {
		return fmt.Errorf("workspace: ownership marker missing: %w", err)
	}
	return nil
}

func (r *Registry) underRoot(abs string) error {
	rel, err := filepath.Rel(r.Root, abs)
	if err != nil || strings.HasPrefix(rel, "..") || rel == "." {
		return fmt.Errorf("workspace: path outside managed root (fail closed)")
	}
	return nil
}

func (r *Registry) rejectPrimaryAsTarget(abs string) error {
	// Primary checkout = anything outside Root cannot be managed target.
	return r.underRoot(abs)
}

func (r *Registry) rejectSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("workspace: symlink path fail closed")
	}
	return nil
}

func (r *Registry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now().UTC()
}

type marker struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Schema    string `json:"schema"`
}

func writeMarker(dir, id, sessionID string) error {
	m := marker{ID: id, SessionID: sessionID, Schema: RegistrySchema}
	return writeJSON(filepath.Join(dir, MarkerFile), m)
}

func readMarker(dir string) (marker, error) {
	b, err := os.ReadFile(filepath.Join(dir, MarkerFile))
	if err != nil {
		return marker{}, err
	}
	var m marker
	if err := json.Unmarshal(b, &m); err != nil {
		return marker{}, err
	}
	if m.Schema != RegistrySchema {
		return marker{}, fmt.Errorf("bad marker schema")
	}
	return m, nil
}

func requireGitRepo(path string) error {
	if _, err := gitOutput(path, "rev-parse", "--git-dir"); err != nil {
		return fmt.Errorf("workspace: not a git repo: %w", err)
	}
	return nil
}

func requireClean(path string) error {
	out, err := gitOutput(path, "status", "--porcelain")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("worktree not clean")
	}
	return nil
}

func gitCommonDir(path string) (string, error) {
	out, err := gitOutput(path, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	abs, err := filepath.Abs(filepath.Join(path, out))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	b, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func removeWorktree(base, dest string) error {
	_, err := git(base, "worktree", "remove", "--force", dest)
	_ = os.RemoveAll(dest)
	return err
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

func ensureGitignoreLine(dir, line string) error {
	path := filepath.Join(dir, ".gitignore")
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(b)
	for _, l := range strings.Split(content, "\n") {
		if strings.TrimSpace(l) == line {
			return nil
		}
	}
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += line + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func shortID(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func hashStr(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
