package adapter_test

import (
	"context"
	"os"
	"path/filepath"
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
