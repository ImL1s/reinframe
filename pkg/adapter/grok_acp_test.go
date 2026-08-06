package adapter_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// fakeACPServer answers initialize/session/new/session/prompt and emits session/update.
// authenticate records the request params for assertion (no credential fields allowed).
func fakeACPServer(t *testing.T, clientWrites io.Reader, clientReads io.Writer, authSink *[]map[string]any) {
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
				"capabilities": map[string]any{
					"loadSession":  true,
					"promptCancel": true,
				},
				"authMethods": []any{
					map[string]any{"id": "env_token"},
				},
			})
		case "authenticate":
			params, _ := req["params"].(map[string]any)
			if authSink != nil {
				*authSink = append(*authSink, params)
			}
			writeRPC(clientReads, idInt, map[string]any{"ok": true})
		case "session/new":
			writeRPC(clientReads, idInt, map[string]any{"sessionId": "sess-1"})
		case "session/load":
			writeRPC(clientReads, idInt, map[string]any{"ok": true})
		case "session/cancel":
			writeRPC(clientReads, idInt, map[string]any{"ok": true})
		case "session/prompt":
			params, _ := req["params"].(map[string]any)
			if params != nil {
				pr, ok := params["prompt"].([]any)
				if !ok || len(pr) == 0 {
					writeRPCErr(clientReads, idInt, "prompt must be ContentBlock[]")
					continue
				}
				block, _ := pr[0].(map[string]any)
				if block["type"] != "text" {
					writeRPCErr(clientReads, idInt, "prompt block type must be text")
					continue
				}
			}
			writeRPC(clientReads, idInt, map[string]any{"ok": true})
			writeNotif(clientReads, "session/update", map[string]any{
				"sessionId": "sess-1",
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"status":        "ok",
				},
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
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()

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
	if c.LastACKLayer() == adapter.ACKLayerExplicit {
		t.Fatal("must not claim explicit")
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACP_DelegatedAuthEnvelope_NoToken(t *testing.T) {
	t.Parallel()
	// Pure builder fixture.
	params, err := adapter.BuildGrokACPAuthenticateParams("env_token")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(params)
	if strings.Contains(string(raw), `"token"`) || strings.Contains(strings.ToLower(string(raw)), "secret") {
		t.Fatalf("credential field leaked: %s", raw)
	}
	if params["methodId"] != "env_token" {
		t.Fatalf("%v", params)
	}
	meta, ok := params["_meta"].(map[string]any)
	if !ok || meta["headless"] != true {
		t.Fatalf("meta=%v", params["_meta"])
	}

	// End-to-end through client: capture wire params.
	var authReqs []map[string]any
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, &authReqs)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Authenticate(ctx, "nope"); err == nil {
		t.Fatal("bad method")
	}
	if err := c.Authenticate(ctx, "env_token"); err != nil {
		t.Fatal(err)
	}
	if len(authReqs) != 1 {
		t.Fatalf("auth calls=%d", len(authReqs))
	}
	got := authReqs[0]
	if _, hasToken := got["token"]; hasToken {
		t.Fatalf("token field must not be sent: %v", got)
	}
	if got["methodId"] != "env_token" {
		t.Fatalf("%v", got)
	}
	meta2, _ := got["_meta"].(map[string]any)
	if meta2["headless"] != true {
		t.Fatalf("%v", got)
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACP_RejectCredentialShapedMethodID(t *testing.T) {
	t.Parallel()
	_, err := adapter.BuildGrokACPAuthenticateParams("token=sk-secret")
	if err == nil {
		t.Fatal("expected reject")
	}
	if !errors.Is(err, adapter.ErrGrokACPAuth) {
		t.Fatalf("%v", err)
	}
}

func TestGrokACP_UnsupportedProtocolVersionFailClosed(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		sc := bufio.NewScanner(serverR)
		for sc.Scan() {
			var req map[string]any
			_ = json.Unmarshal(sc.Bytes(), &req)
			id, _ := req["id"].(float64)
			writeRPC(serverW, int64(id), map[string]any{
				"protocolVersion": 99,
				"capabilities":    map[string]any{},
			})
		}
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err == nil {
		t.Fatal("expected protocolVersion fail-closed")
	}
	// Session ops must not proceed without successful initialize.
	if _, err := c.SessionNew(ctx, nil); err == nil {
		t.Fatal("session before successful init")
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACP_AuthLoadCancelAndNegotiatedManifest(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		fakeACPServer(t, serverR, serverW, nil)
	}()
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := c.Cancel(ctx, "s"); err == nil {
		t.Fatal("cancel before negotiate")
	}
	if err := c.SessionLoad(ctx, "s", nil); err == nil {
		t.Fatal("load before negotiate")
	}
	if _, err := c.Initialize(ctx, map[string]any{"name": "t"}); err != nil {
		t.Fatal(err)
	}
	neg := c.Negotiated()
	if !neg.LoadSession || !neg.Cancel || len(neg.AuthMethods) != 1 || neg.AuthMethods[0] != "env_token" {
		t.Fatalf("%+v", neg)
	}
	if neg.CapsDigest == "" {
		t.Fatalf("expected caps digest, got %+v", neg)
	}
	man := adapter.ManifestFromNegotiated(neg)
	if !man.LoadSession || !man.CapCancel || man.CapPause {
		t.Fatalf("manifest %+v", man)
	}
	// Without CapToolInspection, Level 2 impossible; Level 1 also incomplete → 0.
	if man.NegotiatedLevel >= 2 {
		t.Fatalf("overclaim level %d", man.NegotiatedLevel)
	}
	if err := c.Authenticate(ctx, "env_token"); err != nil {
		t.Fatal(err)
	}
	if err := c.SessionLoad(ctx, "sess-1", nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Cancel(ctx, "sess-1"); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	_ = clientW.Close()
	_ = serverW.Close()
	wg.Wait()
}

func TestGrokACP_ManifestLevelTableDriven(t *testing.T) {
	t.Parallel()
	// Pre-handshake: no achieved level.
	pre := adapter.NewGrokACPFoundationManifest()
	if pre.NegotiatedLevel != -1 || pre.CapEventStream || pre.CapAdviceDelivery || pre.CapPause {
		t.Fatalf("pre-handshake must not claim caps/level: %+v", pre)
	}

	cases := []struct {
		name      string
		init      map[string]any
		wantLevel int
		wantPause bool
	}{
		{
			name: "partial_pause_only",
			init: map[string]any{
				"protocolVersion": 1,
				"capabilities":    map[string]any{"pause": true, "cancel": false, "resume": false},
			},
			wantLevel: 0, // event+advice from ACP path; no tool inspection → L0
			wantPause: true,
		},
		{
			name: "pause_cancel_resume_without_level1",
			init: map[string]any{
				"protocolVersion": 1,
				"capabilities": map[string]any{
					"pause": true, "cancel": true, "resume": true,
				},
			},
			// Still missing CapToolInspection + CapDiffInspection → not L2
			wantLevel: 0,
			wantPause: true,
		},
		{
			name: "full_level2_mask",
			init: map[string]any{
				"protocolVersion": 1,
				"capabilities": map[string]any{
					"pause": true, "cancel": true, "resume": true,
					"toolInspection": true, "diffInspection": true,
				},
			},
			wantLevel: 2,
			wantPause: true,
		},
		{
			name: "tool_only_level1",
			init: map[string]any{
				"protocolVersion": 1,
				"capabilities": map[string]any{
					"toolInspection": true,
				},
			},
			wantLevel: 1,
			wantPause: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps, err := adapter.ParseGrokACPNegotiatedCapsMap(tc.init)
			if err != nil {
				t.Fatal(err)
			}
			man := adapter.ManifestFromNegotiated(caps)
			if man.NegotiatedLevel != tc.wantLevel {
				t.Fatalf("level=%d want %d man=%+v pm=%+v", man.NegotiatedLevel, tc.wantLevel, man, adapter.ProtocolCapabilityManifest(caps))
			}
			if man.CapPause != tc.wantPause {
				t.Fatalf("pause=%v want %v", man.CapPause, tc.wantPause)
			}
			// Double-check against canonical evaluator directly.
			pm := adapter.ProtocolCapabilityManifest(caps)
			if got := protocol.EvaluateAchievableLevel(&pm); got != tc.wantLevel {
				t.Fatalf("evaluator=%d want %d", got, tc.wantLevel)
			}
		})
	}
}

func TestGrokACP_DuplicateAuthMethodsFailClosed(t *testing.T) {
	t.Parallel()
	_, err := adapter.ParseGrokACPNegotiatedCapsMap(map[string]any{
		"protocolVersion": 1,
		"authMethods":     []any{"env_token", "env_token"},
	})
	if err == nil {
		t.Fatal("expected duplicate reject")
	}
}

func TestGrokACP_ReaderEOFFailsPending(t *testing.T) {
	t.Parallel()
	serverR, clientW := io.Pipe()
	clientR, serverW := io.Pipe()
	// Drain client→server writes so the request is not blocked on an unread pipe.
	go func() {
		_, _ = io.Copy(io.Discard, serverR)
	}()
	// No server responses — hang the first call then close the client's stdout (reader EOF).
	c := adapter.NewGrokACPClientForTest(clientW, clientR, adapter.GrokACPConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Initialize(ctx, map[string]any{"name": "t"})
		errCh <- err
	}()
	// Let the request be written and land in pending.
	time.Sleep(80 * time.Millisecond)
	_ = serverW.Close() // EOF on client stdout → failAllPending

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected terminal error")
		}
		if !errors.Is(err, adapter.ErrGrokACPReaderClosed) &&
			!strings.Contains(err.Error(), "reader closed") &&
			!strings.Contains(err.Error(), "reader") {
			t.Fatalf("unexpected err: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending RPC hung after reader EOF — must fail immediately")
	}
	_ = c.Close()
	_ = clientW.Close()
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
	_, err := adapter.ResolveGrokExecutable("/no/such/grok-binary-reinframe-test")
	if err == nil {
		t.Fatal("expected missing")
	}
}

func TestGrokACP_ManifestHonest(t *testing.T) {
	t.Parallel()
	m := adapter.NewGrokACPFoundationManifest()
	if m.ExplicitAck || m.CapPause || m.CapInterventionAck || m.CapEventStream || m.CapAdviceDelivery {
		t.Fatalf("%+v", m)
	}
	if m.ProtocolVersion != 1 || m.NegotiatedLevel != -1 {
		t.Fatalf("%+v", m)
	}
}

func TestGrokHeadless_DefaultArgsOfficialShape(t *testing.T) {
	t.Parallel()
	args := adapter.DefaultGrokHeadlessArgs("hello world")
	// Exact official shape: --no-auto-update -p <PROMPT> --output-format streaming-json
	want := []string{"--no-auto-update", "-p", "hello world", "--output-format", "streaming-json"}
	if len(args) != len(want) {
		t.Fatalf("%v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("i=%d got %q want %q full=%v", i, args[i], want[i], args)
		}
	}
	if err := adapter.ValidateGrokHeadlessArgs(args); err != nil {
		t.Fatal(err)
	}
	// Old wrong shape must fail validation.
	if err := adapter.ValidateGrokHeadlessArgs([]string{"-p", "--output-format", "streaming-json", "--", "x"}); err == nil {
		t.Fatal("old shape should fail")
	}
}

func TestGrokHeadless_FakeExecArgvAndStream(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.json")
	// Build a tiny Go program that records os.Args and emits streaming-json.
	// Argv log path comes from REINFRAME_ARGV_LOG (portable across OS path separators).
	src := filepath.Join(dir, "fake_grok.go")
	srcBody := `package main
import (
  "encoding/json"
  "fmt"
  "os"
)
func main() {
  path := os.Getenv("REINFRAME_ARGV_LOG")
  b, _ := json.Marshal(os.Args)
  _ = os.WriteFile(path, b, 0o644)
  fmt.Println("{\"type\":\"init\"}")
  fmt.Println("{\"type\":\"agent_thought_chunk\",\"text\":\"secret\"}")
  fmt.Println("{\"type\":\"result\",\"status\":\"ok\"}")
}
`
	if err := os.WriteFile(src, []byte(srcBody), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "fake-grok")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, src)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake: %v\n%s", err, out)
	}
	prompt := "observe me please"
	ev, err := adapter.RunGrokHeadlessObserve(context.Background(), adapter.GrokHeadlessObserveConfig{
		Executable: bin,
		Prompt:     prompt,
		Timeout:    15 * time.Second,
		Env:        []string{"REINFRAME_ARGV_LOG=" + argvLog},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	var gotArgs []string
	if err := json.Unmarshal(raw, &gotArgs); err != nil {
		t.Fatal(err)
	}
	// gotArgs[0] is the executable path; rest must match official shape.
	if len(gotArgs) < 6 {
		t.Fatalf("argv too short: %v", gotArgs)
	}
	tail := gotArgs[1:]
	want := adapter.DefaultGrokHeadlessArgs(prompt)
	if len(tail) != len(want) {
		t.Fatalf("argv=%v want suffix %v", gotArgs, want)
	}
	for i := range want {
		if tail[i] != want[i] {
			t.Fatalf("argv[%d]=%q want %q full=%v", i+1, tail[i], want[i], gotArgs)
		}
	}
	// Stream parse: thoughts omitted, result present.
	foundThought, foundResult := false, false
	for _, e := range ev {
		if e.Type == "agent_thought_chunk" {
			foundThought = true
			if e.Summary != "thought_omitted" {
				t.Fatalf("%+v", e)
			}
		}
		if e.Type == "result" {
			foundResult = true
		}
	}
	if !foundThought || !foundResult {
		t.Fatalf("events=%+v", ev)
	}
}

func TestGrokHeadlessObserve_ParseStream(t *testing.T) {
	t.Parallel()
	r := strings.NewReader(`{"type":"init"}
{"type":"agent_thought_chunk","text":"secret-reasoning"}
{"type":"tool_call","toolName":"run_terminal_command"}
not-json
{"type":"result","status":"ok"}
`)
	ev, err := adapter.ParseGrokHeadlessStream(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) < 4 {
		t.Fatalf("%+v", ev)
	}
	foundThought := false
	for _, e := range ev {
		if e.Type == "agent_thought_chunk" {
			foundThought = true
			if e.Summary != "thought_omitted" {
				t.Fatalf("%+v", e)
			}
		}
	}
	if !foundThought {
		t.Fatal("missing thought omit")
	}
	m := adapter.NewGrokHeadlessObserveManifest()
	if m.CapToolGate || m.CapAdviceDelivery || m.ExplicitAck {
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
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
