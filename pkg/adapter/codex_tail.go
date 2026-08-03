package adapter

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// CodexTailSource is a near-live EventSource that follows a Codex rollout JSONL
// file as it grows (poll-based tail). It reuses the offline line parser from
// CodexRolloutSource.
//
// #95 follow-up / near-live path: not a process attach, not a product daemon.
// Suitable for local observation while `codex exec` is writing the same file.
type CodexTailSource struct {
	// Path to rollout JSONL (required).
	Path string
	// SessionIDOverride when non-empty replaces payload session_id.
	SessionIDOverride string
	// PollInterval between file size checks (default 50ms).
	PollInterval time.Duration
	// StartAtEnd when true begins tailing from current EOF (skip historical lines).
	// When false (default), reads existing content then follows.
	// Ignored when CursorPath loads a non-zero offset (resume wins).
	StartAtEnd bool
	// MaxEvents when >0 stops after emitting this many events (tests).
	MaxEvents int
	// CursorPath when non-empty loads/saves durable byte offset (#107).
	// Truncation (file size < offset) resets offset and bumps generation.
	CursorPath string

	// Stats (best-effort after/while running).
	ToolCalls  int
	ExecCalls  int
	SpawnCalls int
	LinesRead  int
}

// Events implements EventSource: tails Path until ctx cancel, MaxEvents, or
// unrecoverable read error. Channel closes on exit.
func (c *CodexTailSource) Events(ctx context.Context) (<-chan protocol.AgentEvent, error) {
	if c.Path == "" {
		return nil, fmt.Errorf("codex tail: Path required")
	}
	poll := c.PollInterval
	if poll <= 0 {
		poll = 50 * time.Millisecond
	}
	ch := make(chan protocol.AgentEvent, 128)
	go c.follow(ctx, ch, poll)
	return ch, nil
}

func (c *CodexTailSource) follow(ctx context.Context, ch chan<- protocol.AgentEvent, poll time.Duration) {
	defer close(ch)

	// Shared parser state via a one-shot CodexRolloutSource-style scanner state.
	parser := &rolloutParser{
		sessionID: c.SessionIDOverride,
		meta:      map[string]string{},
	}

	var offset int64
	var gen int
	if c.CursorPath != "" {
		if cur, err := LoadCodexTailCursor(c.CursorPath); err == nil && cur.Path == c.Path {
			offset = cur.Offset
			gen = cur.Generation
		}
	}
	if offset == 0 && c.StartAtEnd {
		if fi, err := os.Stat(c.Path); err == nil {
			offset = fi.Size()
		}
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			_ = c.persistCursor(offset, gen)
			return
		}
		// Detect truncation before seek.
		if fi, err := os.Stat(c.Path); err == nil {
			if fi.Size() < offset {
				offset = 0
				gen++
			}
		}
		n, err := c.readNew(ctx, ch, parser, &offset)
		if err != nil && !os.IsNotExist(err) {
			// Transient: wait and retry unless canceled.
			select {
			case <-ctx.Done():
				_ = c.persistCursor(offset, gen)
				return
			case <-ticker.C:
				continue
			}
		}
		_ = n
		if c.CursorPath != "" && n > 0 {
			_ = c.persistCursor(offset, gen)
		}
		if c.MaxEvents > 0 && parser.emitted >= c.MaxEvents {
			_ = c.persistCursor(offset, gen)
			return
		}
		// Publish stats snapshot
		c.ToolCalls = parser.toolCalls
		c.ExecCalls = parser.execCalls
		c.SpawnCalls = parser.spawnCalls
		c.LinesRead = parser.linesRead

		select {
		case <-ctx.Done():
			_ = c.persistCursor(offset, gen)
			return
		case <-ticker.C:
		}
	}
}

func (c *CodexTailSource) persistCursor(offset int64, gen int) error {
	if c.CursorPath == "" {
		return nil
	}
	return SaveCodexTailCursor(c.CursorPath, CodexTailCursor{
		Path: c.Path, Offset: offset, Generation: gen,
	})
}

func (c *CodexTailSource) readNew(ctx context.Context, ch chan<- protocol.AgentEvent, parser *rolloutParser, offset *int64) (int, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return 0, err
	}
	// Read all available new bytes as lines.
	buf, err := io.ReadAll(f)
	if err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, nil
	}
	// Only consume complete lines; keep partial trailing data for next read.
	complete := buf
	partial := 0
	if buf[len(buf)-1] != '\n' {
		// find last newline
		last := -1
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				last = i
				break
			}
		}
		if last < 0 {
			// no complete line yet
			return 0, nil
		}
		complete = buf[:last+1]
		partial = len(buf) - (last + 1)
	}
	_ = partial

	// Advance offset only after each line is fully handled so MaxEvents/cancel
	// cannot persist a cursor past unemitted events (Opus #114 C1).
	start := 0
	emitted := 0
	for i := 0; i < len(complete); i++ {
		if complete[i] != '\n' {
			continue
		}
		line := complete[start:i]
		lineBytes := int64(i + 1 - start) // include trailing newline
		start = i + 1
		if len(line) == 0 {
			*offset += lineBytes
			continue
		}
		parser.linesRead++
		ev, ok := parser.parseLine(line)
		if !ok {
			*offset += lineBytes
			continue
		}
		select {
		case <-ctx.Done():
			// Do not advance offset for this unsent event.
			return emitted, ctx.Err()
		case ch <- ev:
			*offset += lineBytes
			emitted++
			parser.emitted++
			if c.MaxEvents > 0 && parser.emitted >= c.MaxEvents {
				c.ToolCalls = parser.toolCalls
				c.ExecCalls = parser.execCalls
				c.SpawnCalls = parser.spawnCalls
				c.LinesRead = parser.linesRead
				return emitted, nil
			}
		}
	}
	c.ToolCalls = parser.toolCalls
	c.ExecCalls = parser.execCalls
	c.SpawnCalls = parser.spawnCalls
	c.LinesRead = parser.linesRead
	return emitted, nil
}

// rolloutParser is the shared JSONL line decoder for offline + tail sources.
type rolloutParser struct {
	sessionID  string
	meta       map[string]string
	seq        int64
	toolCalls  int
	execCalls  int
	spawnCalls int
	linesRead  int
	emitted    int
}

func (p *rolloutParser) parseLine(line []byte) (protocol.AgentEvent, bool) {
	// Delegate to the same logic as CodexRolloutSource by reusing package helpers.
	return parseCodexRolloutLine(p, line)
}
