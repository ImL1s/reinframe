package adapter_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestCodexRolloutSource_ParsesToolCalls(t *testing.T) {
	t.Parallel()
	// Minimal synthetic rollout lines (same shape as real Codex JSONL).
	raw := strings.Join([]string{
		`{"timestamp":"2026-08-03T01:00:00Z","type":"session_meta","payload":{"session_id":"sess-demo","cwd":"/tmp/x","originator":"codex_exec"}}`,
		`{"timestamp":"2026-08-03T01:00:01Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"rg -n foo","status":"completed"}}`,
		`{"timestamp":"2026-08-03T01:00:02Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec","input":"rg -n bar","status":"completed"}}`,
		`{"timestamp":"2026-08-03T01:00:03Z","type":"response_item","payload":{"type":"function_call","name":"spawn_agent","arguments":"{}"}}`,
		`{"timestamp":"2026-08-03T01:00:04Z","type":"response_item","payload":{"type":"function_call_output","output":"exit status 1: build failed"}}`,
		// Real Codex often emits custom_tool_call_output with content-array output.
		`{"timestamp":"2026-08-03T01:00:05Z","type":"response_item","payload":{"type":"custom_tool_call_output","call_id":"c1","output":[{"type":"input_text","text":"panic: boom\nexit status 1"}]}}`,
	}, "\n")
	src := &adapter.CodexRolloutSource{R: strings.NewReader(raw)}
	ch, err := src.Events(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var toolEvents, errEvents int
	for ev := range ch {
		n++
		switch ev.EventType {
		case "tool_call":
			toolEvents++
			var m map[string]any
			if err := json.Unmarshal(ev.Payload, &m); err != nil {
				t.Fatalf("payload: %v", err)
			}
			if m["tool_name"] == "" {
				t.Fatalf("missing tool_name in %s", ev.Payload)
			}
			if m["source"] != "codex_rollout" {
				t.Fatalf("source=%v", m["source"])
			}
		case "error":
			errEvents++
		}
		if ev.SessionID != "sess-demo" {
			t.Fatalf("session=%s", ev.SessionID)
		}
	}
	if src.ExecCalls != 2 {
		t.Fatalf("ExecCalls=%d", src.ExecCalls)
	}
	if src.SpawnCalls != 1 {
		t.Fatalf("SpawnCalls=%d", src.SpawnCalls)
	}
	if src.ToolCalls != 3 {
		t.Fatalf("ToolCalls=%d", src.ToolCalls)
	}
	// 3 tools + 2 error-like outputs (function_call_output + custom_tool_call_output)
	if n != 5 {
		t.Fatalf("events=%d want 5", n)
	}
	if toolEvents != 3 || errEvents != 2 {
		t.Fatalf("toolEvents=%d errEvents=%d", toolEvents, errEvents)
	}
	if src.SessionMeta["cwd"] != "/tmp/x" {
		t.Fatalf("cwd=%v", src.SessionMeta)
	}
	if src.LinesRead != 6 {
		t.Fatalf("LinesRead=%d", src.LinesRead)
	}
}

func TestCodexRolloutSource_RequiresPathOrReader(t *testing.T) {
	t.Parallel()
	src := &adapter.CodexRolloutSource{}
	_, err := src.Events(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
