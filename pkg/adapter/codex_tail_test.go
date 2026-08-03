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

func TestCodexTailSource_FollowsAppendedLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	// Seed session meta + one tool call, then append another while tailing.
	seed := `{"timestamp":"2026-08-03T01:00:00Z","type":"session_meta","payload":{"session_id":"tail-sess","cwd":"/tmp/t","originator":"codex_exec"}}
{"timestamp":"2026-08-03T01:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"echo one","status":"completed"}}
`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	src := &adapter.CodexTailSource{
		Path:         path,
		PollInterval: 20 * time.Millisecond,
		MaxEvents:    2,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Append second tool call after source starts.
	go func() {
		time.Sleep(80 * time.Millisecond)
		f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		_, _ = f.WriteString(`{"timestamp":"2026-08-03T01:00:02Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"echo two","status":"completed"}}` + "\n")
		_ = f.Close()
	}()

	var n int
	var session string
	for ev := range ch {
		n++
		session = ev.SessionID
		if ev.EventType != "tool_call" {
			t.Fatalf("event=%+v", ev)
		}
	}
	if n != 2 {
		t.Fatalf("events=%d want 2 (got session=%s tools=%d)", n, session, src.ToolCalls)
	}
	if session != "tail-sess" {
		t.Fatalf("session=%s", session)
	}
	if src.ExecCalls < 2 {
		t.Fatalf("ExecCalls=%d", src.ExecCalls)
	}
}

func TestCodexTailSource_RequiresPath(t *testing.T) {
	t.Parallel()
	src := &adapter.CodexTailSource{}
	_, err := src.Events(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCodexTailSource_CursorResumeAndTruncate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	cursor := filepath.Join(dir, "cursor.json")
	line1 := `{"timestamp":"2026-08-03T01:00:00Z","type":"session_meta","payload":{"session_id":"cur-sess"}}` + "\n"
	line2 := `{"timestamp":"2026-08-03T01:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"echo a"}}` + "\n"
	if err := os.WriteFile(path, []byte(line1+line2), 0o644); err != nil {
		t.Fatal(err)
	}
	// First pass: read both tool events, persist cursor
	src := &adapter.CodexTailSource{
		Path: path, CursorPath: cursor, MaxEvents: 1, PollInterval: 10 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	ch, err := src.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for range ch {
		n++
	}
	cancel()
	if n != 1 {
		t.Fatalf("events=%d", n)
	}
	cur, err := adapter.LoadCodexTailCursor(cursor)
	if err != nil || cur.Offset <= 0 {
		t.Fatalf("cursor=%+v err=%v", cur, err)
	}
	// Truncate file → next tail should reset generation and re-read from 0
	if err := os.WriteFile(path, []byte(line1+line2), 0o644); err != nil {
		t.Fatal(err)
	}
	// Force stale large offset as if truncated content
	_ = adapter.SaveCodexTailCursor(cursor, adapter.CodexTailCursor{Path: path, Offset: 99999, Generation: 1})
	src2 := &adapter.CodexTailSource{
		Path: path, CursorPath: cursor, MaxEvents: 1, PollInterval: 10 * time.Millisecond,
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ch2, err := src2.Events(ctx2)
	if err != nil {
		t.Fatal(err)
	}
	n2 := 0
	for range ch2 {
		n2++
	}
	if n2 != 1 {
		t.Fatalf("after truncate events=%d", n2)
	}
	cur2, _ := adapter.LoadCodexTailCursor(cursor)
	if cur2.Generation < 2 {
		t.Fatalf("generation=%d want >=2 after truncate", cur2.Generation)
	}
}

func TestCodexTailSource_MaxEventsDoesNotSkipUnreadLines(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	cursor := filepath.Join(dir, "cursor.json")
	// two tool calls
	content := strings.Join([]string{
		`{"timestamp":"2026-08-03T01:00:00Z","type":"session_meta","payload":{"session_id":"s"}}`,
		`{"timestamp":"2026-08-03T01:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"one"}}`,
		`{"timestamp":"2026-08-03T01:00:02Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"two"}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pass 1: only first tool event
	src := &adapter.CodexTailSource{Path: path, CursorPath: cursor, MaxEvents: 1, PollInterval: 5 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	ch, _ := src.Events(ctx)
	for range ch {
	}
	cancel()
	// Pass 2: must still get the second tool event
	src2 := &adapter.CodexTailSource{Path: path, CursorPath: cursor, MaxEvents: 1, PollInterval: 5 * time.Millisecond}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	ch2, _ := src2.Events(ctx2)
	n := 0
	for range ch2 {
		n++
	}
	if n != 1 {
		t.Fatalf("second pass events=%d want 1 (second tool call)", n)
	}
}
