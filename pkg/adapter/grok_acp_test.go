package adapter_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// fakeACPServer answers initialize/session/new/session/prompt and emits session/update.
func fakeACPServer(t *testing.T, clientWrites io.Reader, clientReads io.Writer) {
	t.Helper()
	sc := bufio.NewScanner(clientWrites)
	for sc.Scan() {
		var req map[string]any
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id, _ := req["id"].(float64)
		idInt := int64(id)
		switch method {
		case "initialize":
			writeRPC(clientReads, idInt, map[string]any{
				"protocolVersion": 1,
				"serverInfo":      map[string]any{"name": "fake-grok"},
				"capabilities":    map[string]any{"loadSession": false},
			})
		case "session/new":
			writeRPC(clientReads, idInt, map[string]any{"sessionId": "sess-1"})
		case "session/prompt":
			writeRPC(clientReads, idInt, map[string]any{"ok": true})
			// notification (no id)
			writeNotif(clientReads, "session/update", map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"status":        "ok",
			})
		default:
			writeRPCErr(clientReads, idInt, "unknown method")
		}
	}
}

func writeRPC(w io.Writer, id int64, result any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = w.Write(append(raw, '\n'))
}

func writeRPCErr(w io.Writer, id int64, msg string) {
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id,
		"error": map[string]any{"code": -32601, "message": msg},
	})
	_, _ = w.Write(append(raw, '\n'))
}

func writeNotif(w io.Writer, method string, params any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, _ = w.Write(append(raw, '\n'))
}

func TestGrokACP_InitializeSessionPromptACK(t *testing.T) {
	t.Parallel()
	// client writes to serverR; server writes to clientR
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW)
	}()

	// Client: stdin=clientW (writes to server), stdout=clientR (reads from server)
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{
		StartupTimeout: 5 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cap, err := c.Initialize(ctx, map[string]any{"name": "reinframe-test"})
	if err != nil {
		t.Fatal(err)
	}
	if cap["protocolVersion"].(float64) != 1 {
		t.Fatalf("%v", cap)
	}
	if c.LastACKLayer() != adapter.ACKLayerTransport {
		t.Fatalf("ack=%s", c.LastACKLayer())
	}
	sid, err := c.SessionNew(ctx, map[string]any{"cwd": "/tmp"})
	if err != nil || sid != "sess-1" {
		t.Fatalf("sid=%s err=%v", sid, err)
	}
	prompt := adapter.BuildAdvicePrompt("ZOOM_OUT_PROMPT", "step back", "int-1", "ch-2")
	if err := c.SessionPrompt(ctx, sid, prompt, "int-1", "ch-2"); err != nil {
		t.Fatal(err)
	}
	// Wait for session/update
	select {
	case u := <-c.Updates():
		kind, sum := adapter.MapSessionUpdateToSummary(u)
		if kind == "" || sum == "" {
			t.Fatalf("%v", u)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting update")
	}
	if c.LastACKLayer() != adapter.ACKLayerSessionVisible {
		t.Fatalf("want session_visible got %s", c.LastACKLayer())
	}
	// Explicit ACK never claimed
	if c.LastACKLayer() == adapter.ACKLayerExplicit {
		t.Fatal("must not claim explicit")
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACP_RejectShellMetacharExecutable(t *testing.T) {
	t.Parallel()
	_, err := adapter.StartGrokACPClient(context.Background(), adapter.GrokACPConfig{
		Executable: "/bin/true;rm -rf /",
	})
	if err == nil {
		t.Fatal("expected reject")
	}
}

func TestGrokACP_ResolveExecutableEmpty(t *testing.T) {
	t.Parallel()
	// LookPath may succeed on this machine; only assert explicit bad path fails.
	_, err := adapter.ResolveGrokExecutable("/no/such/grok-binary-reinframe-test")
	if err == nil {
		t.Fatal("expected missing")
	}
}

func TestGrokACP_ManifestHonest(t *testing.T) {
	t.Parallel()
	m := adapter.NewGrokACPFoundationManifest()
	if m.ExplicitAck || m.CapPause || m.CapInterventionAck {
		t.Fatalf("%+v", m)
	}
	if m.ProtocolVersion != 1 || !m.CapAdviceDelivery {
		t.Fatalf("%+v", m)
	}
}

func TestBuildAdvicePrompt_Bounded(t *testing.T) {
	t.Parallel()
	p := adapter.BuildAdvicePrompt("REQUEST_REPLAN", "rethink scope", "i1", "c1")
	if !containsAll(p, "InterventionID=i1", "ChallengeID=c1", "REQUEST_REPLAN") {
		t.Fatalf("%q", p)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !containsStr(s, p) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
