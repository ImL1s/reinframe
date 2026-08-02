package adapter

import (
	"bufio"
	"context"
	"io"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// LogObserverAdapter is an observe-only EventSource that tails line-oriented logs
// (stdout/stderr/file) and emits canonical AgentEvent records (Level 0 path).
type LogObserverAdapter struct {
	SessionID string
	R         io.Reader
	EventType string // default tool_call_event-like generic "log_line"

	once   sync.Once
	ch     chan protocol.AgentEvent
	seq    int64
	mu     sync.Mutex
	closed bool
}

// Events starts a single background reader; subsequent calls return the same channel.
func (l *LogObserverAdapter) Events(ctx context.Context) (<-chan protocol.AgentEvent, error) {
	if l.R == nil {
		return nil, io.ErrUnexpectedEOF
	}
	l.once.Do(func() {
		l.ch = make(chan protocol.AgentEvent, 64)
		et := l.EventType
		if et == "" {
			et = "log_line"
		}
		go func() {
			defer close(l.ch)
			sc := bufio.NewScanner(l.R)
			// allow long log lines
			buf := make([]byte, 0, 64*1024)
			sc.Buffer(buf, 1024*1024)
			for {
				if ctx.Err() != nil {
					return
				}
				if !sc.Scan() {
					return
				}
				l.mu.Lock()
				if l.closed {
					l.mu.Unlock()
					return
				}
				l.seq++
				seq := l.seq
				l.mu.Unlock()
				line := sc.Text()
				ev := protocol.AgentEvent{
					EventID:     time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + itoa(seq),
					SessionID:   l.SessionID,
					SequenceNum: seq,
					EventType:   et,
					Timestamp:   time.Now().UTC(),
					Payload:     []byte(`{"line":` + jsonQuote(line) + `}`),
				}
				select {
				case <-ctx.Done():
					return
				case l.ch <- ev:
				}
			}
		}()
	})
	return l.ch, nil
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func jsonQuote(s string) string {
	// minimal JSON string escape
	out := make([]byte, 0, len(s)+2)
	out = append(out, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '"':
			out = append(out, '\\', c)
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		default:
			if c < 0x20 {
				out = append(out, ' ')
			} else {
				out = append(out, c)
			}
		}
	}
	out = append(out, '"')
	return string(out)
}
