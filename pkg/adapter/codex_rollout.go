package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// CodexRolloutSource is an offline EventSource that reads a Codex CLI
// rollout JSONL file (sessions/**/rollout-*.jsonl) into AgentEvent records.
//
// This is the street-test / fixture path toward issue #95 (live EventSource).
// It does not attach to a running codex process and does not claim product supervision.
type CodexRolloutSource struct {
	// Path to rollout JSONL. Required unless R is set.
	Path string
	// R optional reader (tests); when set, Path is ignored.
	R io.Reader
	// SessionIDOverride when non-empty replaces payload session_id.
	SessionIDOverride string

	// Stats filled after Events completes (best-effort).
	ToolCalls   int
	ExecCalls   int
	SpawnCalls  int
	LinesRead   int
	SessionMeta map[string]string
}

// Events implements EventSource: scans the rollout once and closes the channel.
func (c *CodexRolloutSource) Events(ctx context.Context) (<-chan protocol.AgentEvent, error) {
	if c.R != nil {
		return c.eventsFromReader(ctx, c.R, nil)
	}
	if c.Path == "" {
		return nil, fmt.Errorf("codex rollout: Path or R required")
	}
	return c.eventsFromPath(ctx)
}

func (c *CodexRolloutSource) eventsFromPath(ctx context.Context) (<-chan protocol.AgentEvent, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return nil, err
	}
	return c.eventsFromReader(ctx, f, f)
}

func (c *CodexRolloutSource) eventsFromReader(ctx context.Context, r io.Reader, closer io.Closer) (<-chan protocol.AgentEvent, error) {
	ch := make(chan protocol.AgentEvent, 128)
	go func() {
		if closer != nil {
			defer closer.Close()
		}
		defer close(ch)
		sc := bufio.NewScanner(r)
		buf := make([]byte, 0, 256*1024)
		sc.Buffer(buf, 4*1024*1024)
		parser := &rolloutParser{
			sessionID: c.SessionIDOverride,
			meta:      map[string]string{},
		}
		for sc.Scan() {
			if ctx.Err() != nil {
				return
			}
			parser.linesRead++
			ev, ok := parseCodexRolloutLine(parser, sc.Bytes())
			if !ok {
				continue
			}
			select {
			case <-ctx.Done():
				return
			case ch <- ev:
			}
		}
		c.LinesRead = parser.linesRead
		c.ToolCalls = parser.toolCalls
		c.ExecCalls = parser.execCalls
		c.SpawnCalls = parser.spawnCalls
		c.SessionMeta = parser.meta
	}()
	return ch, nil
}

func makeToolEvent(sessionID string, seq int64, ts time.Time, name string, payload map[string]any) protocol.AgentEvent {
	if sessionID == "" {
		sessionID = "codex-unknown"
	}
	input, _ := payload["input"].(string)
	if input == "" {
		if args, ok := payload["arguments"].(string); ok {
			input = args
		}
	}
	// keep payload small
	if len(input) > 500 {
		input = input[:500] + "…"
	}
	body, _ := json.Marshal(map[string]any{
		"tool_name": name,
		"source":    "codex_rollout",
		"input":     input,
	})
	return protocol.AgentEvent{
		EventID:     fmt.Sprintf("codex-tool-%d", seq),
		SessionID:   sessionID,
		SequenceNum: seq,
		EventType:   "tool_call",
		Timestamp:   ts,
		Payload:     body,
	}
}

func makeErrorEvent(sessionID string, seq int64, ts time.Time, out string) protocol.AgentEvent {
	if sessionID == "" {
		sessionID = "codex-unknown"
	}
	snippet := out
	if len(snippet) > 400 {
		snippet = snippet[:400]
	}
	body, _ := json.Marshal(map[string]string{"error": snippet, "source": "codex_rollout"})
	return protocol.AgentEvent{
		EventID:     fmt.Sprintf("codex-err-%d", seq),
		SessionID:   sessionID,
		SequenceNum: seq,
		EventType:   "error",
		Timestamp:   ts,
		Payload:     body,
	}
}

func looksLikeFailure(s string) bool {
	l := strings.ToLower(s)
	for _, k := range []string{"error:", "failed", "panic:", "exit status 1", "traceback"} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

// payloadOutputString normalizes Codex output fields (plain string or content array).
func payloadOutputString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		var b strings.Builder
		for _, item := range t {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := m["text"].(string); ok {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(text)
			}
		}
		return b.String()
	default:
		return ""
	}
}
