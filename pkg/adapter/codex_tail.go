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
// #95 / #118: not a process attach, not a product daemon. Observe-only.
type CodexTailSource struct {
	Path              string
	SessionIDOverride string
	PollInterval      time.Duration
	StartAtEnd        bool
	MaxEvents         int
	CursorPath        string
	// FailClosedCursor aborts follow on cursor load error when true.
	// Default false = fail-open (LastCursorError set, start offset 0).
	FailClosedCursor bool
	// MaxBytesPerPoll caps bytes read per poll (default 1MiB). Never unbounded ReadAll.
	MaxBytesPerPoll int
	// MaxRecordBytes caps a single line (default 1MiB).
	MaxRecordBytes int
	// MaxRecordsPerBatch caps events per poll (0 = unlimited aside from MaxEvents).
	MaxRecordsPerBatch int

	ToolCalls       int
	ExecCalls       int
	SpawnCalls      int
	LinesRead       int
	LastCursorError error
	AuditUnknown    int
	AuditMalformed  int
}

// Events implements EventSource.
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

	parser := &rolloutParser{
		sessionID:    c.SessionIDOverride,
		meta:         map[string]string{},
		fileIdentity: FileIdentityFromPath(c.Path),
	}

	var offset int64
	var gen int
	if c.CursorPath != "" {
		cur, err := LoadCodexTailCursor(c.CursorPath)
		if err != nil {
			c.LastCursorError = err
			if c.FailClosedCursor {
				return
			}
			gen = 1
		} else if cur.Path == c.Path || cur.Path == "" {
			offset = cur.Offset
			gen = cur.Generation
			if cur.SessionID != "" && c.SessionIDOverride == "" {
				parser.sessionID = cur.SessionID
			}
			if cur.FileIdentity != "" {
				parser.fileIdentity = cur.FileIdentity
			}
		}
	}
	parser.generation = gen
	if offset == 0 && c.StartAtEnd {
		if fi, err := os.Stat(c.Path); err == nil {
			offset = fi.Size()
		}
	}

	ticker := time.NewTicker(poll)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			_ = c.persistCursor(offset, gen, parser.sessionID)
			return
		}
		if fi, err := os.Stat(c.Path); err == nil {
			fp := FileFingerprint{Size: fi.Size(), ModNano: fi.ModTime().UnixNano()}
			prev := FileFingerprint{Size: offset}
			if RotationDetected(offset, prev, fp) {
				offset = 0
				gen++
				parser.generation = gen
			}
		}
		n, err := c.readNew(ctx, ch, parser, &offset, gen)
		if err != nil && !os.IsNotExist(err) {
			select {
			case <-ctx.Done():
				_ = c.persistCursor(offset, gen, parser.sessionID)
				return
			case <-ticker.C:
				continue
			}
		}
		if c.CursorPath != "" && n > 0 {
			if err := c.persistCursor(offset, gen, parser.sessionID); err != nil && c.FailClosedCursor {
				return
			}
		}
		if c.MaxEvents > 0 && parser.emitted >= c.MaxEvents {
			_ = c.persistCursor(offset, gen, parser.sessionID)
			return
		}
		c.ToolCalls = parser.toolCalls
		c.ExecCalls = parser.execCalls
		c.SpawnCalls = parser.spawnCalls
		c.LinesRead = parser.linesRead

		select {
		case <-ctx.Done():
			_ = c.persistCursor(offset, gen, parser.sessionID)
			return
		case <-ticker.C:
		}
	}
}

func (c *CodexTailSource) persistCursor(offset int64, gen int, sessionID string) error {
	if c.CursorPath == "" {
		return nil
	}
	err := SaveCodexTailCursor(c.CursorPath, CodexTailCursor{
		Path: c.Path, Offset: offset, Generation: gen, SessionID: sessionID,
		FileIdentity: FileIdentityFromPath(c.Path), SchemaVersion: codexCursorSchemaVersion,
	})
	if err != nil {
		c.LastCursorError = err
	}
	return err
}

func (c *CodexTailSource) readNew(ctx context.Context, ch chan<- protocol.AgentEvent, parser *rolloutParser, offset *int64, gen int) (int, error) {
	f, err := os.Open(c.Path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Seek(*offset, io.SeekStart); err != nil {
		return 0, err
	}
	maxPoll := c.MaxBytesPerPoll
	if maxPoll <= 0 {
		maxPoll = 1 << 20
	}
	maxRec := c.MaxRecordBytes
	if maxRec <= 0 {
		maxRec = 1 << 20
	}
	limited := io.LimitReader(f, int64(maxPoll)+1)
	buf, err := io.ReadAll(limited)
	if err != nil {
		return 0, err
	}
	if len(buf) > maxPoll {
		buf = buf[:maxPoll]
	}
	if len(buf) == 0 {
		return 0, nil
	}
	complete := buf
	if buf[len(buf)-1] != '\n' {
		last := -1
		for i := len(buf) - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				last = i
				break
			}
		}
		if last < 0 {
			return 0, nil
		}
		complete = buf[:last+1]
	}

	start := 0
	emitted := 0
	for i := 0; i < len(complete); i++ {
		if complete[i] != '\n' {
			continue
		}
		line := complete[start:i]
		lineBytes := int64(i + 1 - start)
		start = i + 1
		if len(line) == 0 {
			*offset += lineBytes
			continue
		}
		if len(line) > maxRec {
			c.AuditMalformed++
			*offset += lineBytes
			continue
		}
		parser.linesRead++
		parser.generation = gen
		parser.lineEndOffset = *offset + lineBytes
		before := parser.unknown + parser.malformed
		ev, ok := parser.parseLine(line)
		if !ok {
			if parser.unknown+parser.malformed > before {
				c.AuditUnknown += (parser.unknown + parser.malformed) - before
			}
			*offset += lineBytes
			continue
		}
		lineEnd := *offset + lineBytes
		ev.SequenceNum = lineEnd
		select {
		case <-ctx.Done():
			return emitted, ctx.Err()
		case ch <- ev:
			*offset += lineBytes
			emitted++
			parser.emitted++
			if c.MaxRecordsPerBatch > 0 && emitted >= c.MaxRecordsPerBatch {
				c.ToolCalls = parser.toolCalls
				c.ExecCalls = parser.execCalls
				c.SpawnCalls = parser.spawnCalls
				c.LinesRead = parser.linesRead
				return emitted, nil
			}
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
	sessionID     string
	meta          map[string]string
	seq           int64
	toolCalls     int
	execCalls     int
	spawnCalls    int
	linesRead     int
	emitted       int
	malformed     int
	unknown       int
	generation    int
	fileIdentity  string
	lineEndOffset int64
}

func (p *rolloutParser) parseLine(line []byte) (protocol.AgentEvent, bool) {
	return parseCodexRolloutLine(p, line)
}

func (p *rolloutParser) identityAtLine() *SourceRecordIdentity {
	return &SourceRecordIdentity{
		SchemaVersion: SourceRecordIdentitySchemaVersion,
		Source:        CodexEventIDSource,
		SessionID:     p.sessionID,
		Generation:    p.generation,
		FileIdentity:  p.fileIdentity,
		RecordOffset:  p.lineEndOffset,
	}
}
