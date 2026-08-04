package adapter_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestSourceRecordIdentity_TwoSessionsSameOffset(t *testing.T) {
	t.Parallel()
	a := adapter.SourceRecordIdentity{
		Source: adapter.CodexEventIDSource, SessionID: "s1", Generation: 0,
		FileIdentity: "f", RecordOffset: 100, EventKind: "tool_call",
	}
	b := a
	b.SessionID = "s2"
	if a.FormatEventID() == b.FormatEventID() {
		t.Fatal("session collision")
	}
}

func TestSourceRecordIdentity_OfflineAndTailSameShape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	line := `{"type":"session_meta","payload":{"session_id":"11111111-1111-1111-1111-111111111111"}}` + "\n" +
		`{"type":"response_item","payload":{"type":"function_call","name":"exec","arguments":"ls"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	off := &adapter.CodexRolloutSource{Path: path}
	ctx1, cancel1 := context.WithTimeout(context.Background(), 2*time.Second)
	ch, err := off.Events(ctx1)
	if err != nil {
		cancel1()
		t.Fatal(err)
	}
	var offlineID string
	for ev := range ch {
		if ev.EventType == "tool_call" {
			offlineID = ev.EventID
		}
	}
	cancel1()
	if offlineID == "" || !strings.HasPrefix(offlineID, "codex_jsonl|") {
		t.Fatalf("offline id=%q", offlineID)
	}
	cursor := filepath.Join(dir, "cur.json")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	tail := &adapter.CodexTailSource{Path: path, CursorPath: cursor, MaxEvents: 1, PollInterval: 5 * time.Millisecond}
	ch2, err := tail.Events(ctx2)
	if err != nil {
		cancel2()
		t.Fatal(err)
	}
	var tailID string
	for ev := range ch2 {
		if ev.EventType == "tool_call" {
			tailID = ev.EventID
		}
	}
	cancel2()
	time.Sleep(20 * time.Millisecond)
	if offlineID != tailID {
		t.Fatalf("offline/tail mismatch:\n  off=%s\n  tail=%s", offlineID, tailID)
	}
}

func TestCodexTail_CorruptCursorSurfaced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "r.jsonl")
	_ = os.WriteFile(path, []byte("{}\n"), 0o644)
	cur := filepath.Join(dir, "c.json")
	_ = os.WriteFile(cur, []byte("not-json"), 0o600)
	src := &adapter.CodexTailSource{Path: path, CursorPath: cur, MaxEvents: 1, PollInterval: time.Millisecond, FailClosedCursor: true}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
	if src.LastCursorError == nil {
		t.Fatal("expected LastCursorError on corrupt cursor")
	}
}

func TestRotationDetected_SizeAndInode(t *testing.T) {
	t.Parallel()
	if !adapter.RotationDetected(100, adapter.FileFingerprint{Size: 200}, adapter.FileFingerprint{Size: 50}) {
		t.Fatal("size < offset")
	}
	if !adapter.RotationDetected(10, adapter.FileFingerprint{Inode: 1}, adapter.FileFingerprint{Size: 100, Inode: 2}) {
		t.Fatal("inode change")
	}
}
