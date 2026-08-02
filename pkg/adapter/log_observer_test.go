package adapter

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLogObserverAdapter_EmitsLinesAsEvents(t *testing.T) {
	r := strings.NewReader("error: boom\nerror: boom\n")
	obs := &LogObserverAdapter{SessionID: "sess-log", R: r}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ch, err := obs.Events(ctx)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	var n int
	for ev := range ch {
		n++
		if ev.SessionID != "sess-log" {
			t.Fatalf("session %q", ev.SessionID)
		}
		if ev.SequenceNum != int64(n) {
			t.Fatalf("seq got %d want %d", ev.SequenceNum, n)
		}
		if ev.EventType != "log_line" {
			t.Fatalf("type %s", ev.EventType)
		}
		if !strings.Contains(string(ev.Payload), "boom") {
			t.Fatalf("payload %s", ev.Payload)
		}
	}
	if n != 2 {
		t.Fatalf("expected 2 events, got %d", n)
	}
}
