package adapter_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestDiscoverAndSelectCodexRollouts(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// nested path like real layout
	dir := filepath.Join(root, "2026", "08", "03")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := filepath.Join(dir, "rollout-2026-08-03T10-00-00-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl")
	p2 := filepath.Join(dir, "rollout-2026-08-03T11-00-00-11111111-2222-3333-4444-555555555555.jsonl")
	if err := os.WriteFile(p1, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(p2, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cands, err := adapter.DiscoverCodexRollouts(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cands) != 2 {
		t.Fatalf("cands=%d", len(cands))
	}
	// refuse auto-pick
	if _, err := adapter.SelectCodexRollout(cands, ""); err == nil {
		t.Fatal("expected multi-candidate error")
	}
	sel, err := adapter.SelectCodexRollout(cands, p1)
	if err != nil || sel.Path != p1 {
		t.Fatalf("sel=%+v err=%v", sel, err)
	}
	// Full UUID, not trailing fragment after last hyphen.
	wantID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var found bool
	for _, c := range cands {
		if c.Path == p1 {
			if c.SessionID != wantID {
				t.Fatalf("SessionID=%q want %q", c.SessionID, wantID)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("p1 missing")
	}
	// single candidate auto-ok
	one, err := adapter.SelectCodexRollout(cands[:1], "")
	if err != nil || one.Path != cands[0].Path {
		t.Fatalf("%+v %v", one, err)
	}
}

func TestCodexTailCursor_TruncateAndSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cp := filepath.Join(dir, "cursor.json")
	c := adapter.CodexTailCursor{Path: "/x.jsonl", Offset: 100, Generation: 1}
	if err := adapter.SaveCodexTailCursor(cp, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := adapter.LoadCodexTailCursor(cp)
	if err != nil || loaded.Offset != 100 {
		t.Fatalf("%+v %v", loaded, err)
	}
	fixed := adapter.ReconcileCursorAgainstFile(loaded, 40)
	if fixed.Offset != 0 || fixed.Generation != 2 {
		t.Fatalf("%+v", fixed)
	}
}

func TestDefaultCodexCapability_ObserveOnly(t *testing.T) {
	t.Parallel()
	m := adapter.DefaultCodexCapabilityManifest()
	if !m.ObserveEvents || m.InjectMessage || m.PreToolGate || m.NegotiatedLevel != 0 {
		t.Fatalf("%+v", m)
	}
}
