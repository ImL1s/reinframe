package adapter_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// fakeAppServer provides a mock JSON-RPC stdio server for testing CodexAppServerClient.
func fakeAppServer(
	t *testing.T,
	clientWrites io.Reader,
	clientReads io.Writer,
	customHandler func(method string, id int64, params map[string]any) (result any, rpcErr map[string]any, handled bool),
) {
	t.Helper()
	sc := bufio.NewScanner(clientWrites)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, adapter.MaxCodexAppServerMessageBytes+1)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		method, _ := req["method"].(string)
		idFloat, hasID := req["id"].(float64)
		idInt := int64(idFloat)
		params, _ := req["params"].(map[string]any)

		if customHandler != nil {
			result, rpcErr, handled := customHandler(method, idInt, params)
			if handled {
				if rpcErr != nil && hasID {
					writeAppRPCErr(clientReads, idInt, rpcErr)
				} else if hasID {
					writeAppRPC(clientReads, idInt, result)
				}
				continue
			}
		}

		if !hasID {
			// Notification received from client (e.g. "initialized")
			continue
		}

		switch method {
		case "initialize":
			writeAppRPC(clientReads, idInt, map[string]any{
				"serverInfo": map[string]any{
					"name":    "codex-app-server",
					"version": "1.0.0",
				},
				"protocolVersion": 1,
				"capabilities": map[string]any{
					"approval":     true,
					"streaming":    true,
					"modelCatalog": true,
				},
			})
		case "thread/start":
			model, _ := params["model"].(string)
			if model == "" {
				model = "gpt-5.3-codex-spark"
			}
			writeAppRPC(clientReads, idInt, map[string]any{
				"threadId":  "th_test_100",
				"model":     model,
				"status":    "active",
				"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
		case "thread/resume":
			writeAppRPC(clientReads, idInt, map[string]any{
				"threadId": params["threadId"],
				"model":    "gpt-5.3-codex-spark",
				"status":   "active",
			})
		case "turn/start":
			model, _ := params["model"].(string)
			if model == "" {
				model = "gpt-5.3-codex-spark"
			}
			turnID, _ := params["turnId"].(string)
			if turnID == "" {
				turnID = "turn_test_200"
			}
			threadID, _ := params["threadId"].(string)
			writeAppRPC(clientReads, idInt, map[string]any{
				"turnId":    turnID,
				"threadId":  threadID,
				"status":    "in_progress",
				"model":     model,
				"startedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})
		case "turn/interrupt":
			writeAppRPC(clientReads, idInt, map[string]any{"ok": true})
		case "approval/respond":
			writeAppRPC(clientReads, idInt, map[string]any{"ok": true})
		default:
			writeAppRPCErr(clientReads, idInt, map[string]any{"code": -32601, "message": "Method not found"})
		}
	}
}

func writeAppRPC(w io.Writer, id int64, result any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = w.Write(append(raw, '\n'))
}

func writeAppRPCErr(w io.Writer, id int64, errObj map[string]any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": errObj})
	_, _ = w.Write(append(raw, '\n'))
}

func writeAppNotif(w io.Writer, method string, params any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	_, _ = w.Write(append(raw, '\n'))
}

func writeAppServerRequest(w io.Writer, id int64, method string, params any) {
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	_, _ = w.Write(append(raw, '\n'))
}

// 1. Initialize Ordering Tests
func TestCodexAppServer_InitializeHandshake(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, nil)

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Starting thread succeeds after initialization
	th, err := client.StartThread(ctx, adapter.ThreadStartRequest{
		ModelID: "gpt-5.3-codex-spark",
	})
	if err != nil {
		t.Fatalf("StartThread failed: %v", err)
	}
	if th.ID != "th_test_100" {
		t.Errorf("got thread ID %q, want %q", th.ID, "th_test_100")
	}
	if th.ModelIdentity.SubstitutionState != adapter.ModelSubstitutionExact {
		t.Errorf("got substitution state %v, want exact_match", th.ModelIdentity.SubstitutionState)
	}

	_ = client.Close(ctx)
}

func TestCodexAppServer_CallBeforeInitialize_Fails(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	cfg := adapter.CodexAppServerConfig{}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	_, err := client.StartThread(ctx, adapter.ThreadStartRequest{
		ModelID: "gpt-5.3-codex-spark",
	})
	if err == nil {
		t.Fatal("expected StartThread to fail before Start/initialize")
	}
	var appErr *adapter.AppServerError
	if !errors.As(err, &appErr) || appErr.Code != adapter.ErrCodeRuntimeCrashed {
		t.Errorf("expected ErrCodeRuntimeCrashed, got %v", err)
	}
}

func TestCodexAppServer_InitializeTimeout_Fails(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	// Server does not answer initialize
	go func() {
		sc := bufio.NewScanner(c2sR)
		for sc.Scan() {
			// Blackhole requests
		}
	}()

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 50 * time.Millisecond,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	err := client.Start(ctx)
	if err == nil {
		t.Fatal("expected Start to fail on startup timeout")
	}
	var appErr *adapter.AppServerError
	if !errors.As(err, &appErr) || appErr.Code != adapter.ErrCodeStartupTimeout {
		t.Errorf("expected ErrCodeStartupTimeout, got %v", err)
	}
}

// 2. Request ID Correlation & Concurrency
func TestCodexAppServer_RequestIDCorrelationAndConcurrency(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, func(method string, id int64, params map[string]any) (any, map[string]any, bool) {
		if method == "turn/start" {
			prompt, _ := params["prompt"].(string)
			// Return prompt echoed in turnId to verify correlation
			return map[string]any{
				"turnId":    "turn_" + prompt,
				"threadId":  params["threadId"],
				"status":    "in_progress",
				"model":     "gpt-5.3-codex-spark",
				"startedAt": time.Now().UTC().Format(time.RFC3339Nano),
			}, nil, true
		}
		return nil, nil, false
	})

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
		RequestTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	const numWorkers = 20
	var wg sync.WaitGroup
	errCh := make(chan error, numWorkers)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			prompt := fmt.Sprintf("prompt_%d", idx)
			turn, err := client.StartTurn(ctx, adapter.TurnStartRequest{
				ThreadID: "th_concurrency",
				Prompt:   prompt,
				ModelID:  "gpt-5.3-codex-spark",
			})
			if err != nil {
				errCh <- fmt.Errorf("worker %d failed: %w", idx, err)
				return
			}
			expectedTurnID := "turn_" + prompt
			if turn.ID != expectedTurnID {
				errCh <- fmt.Errorf("worker %d got turn ID %q, want %q", idx, turn.ID, expectedTurnID)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}
}

// 3. Model Identity & Substitution Invariants
func TestCodexAppServer_ModelIdentity_Invariants(t *testing.T) {
	tests := []struct {
		name          string
		requested     string
		reported      string
		allowFallback bool
		wantErrCode   adapter.AppServerErrorCode
		wantState     adapter.ModelSubstitutionState
	}{
		{
			name:          "exact match",
			requested:     "gpt-5.3-codex-spark",
			reported:      "gpt-5.3-codex-spark",
			allowFallback: false,
			wantErrCode:   "",
			wantState:     adapter.ModelSubstitutionExact,
		},
		{
			name:          "default model exact match",
			requested:     "",
			reported:      "gpt-5.3-codex-spark",
			allowFallback: false,
			wantErrCode:   "",
			wantState:     adapter.ModelSubstitutionExact,
		},
		{
			name:          "absent reported model identity unproven",
			requested:     "gpt-5.3-codex-spark",
			reported:      "",
			allowFallback: false,
			wantErrCode:   adapter.ErrCodeModelIdentityUnproven,
			wantState:     adapter.ModelSubstitutionIdentityUnproven,
		},
		{
			name:          "substitution with fallback disabled fails",
			requested:     "gpt-5.3-codex-spark",
			reported:      "gpt-5-codex",
			allowFallback: false,
			wantErrCode:   adapter.ErrCodeModelUnavailable,
			wantState:     adapter.ModelSubstitutionViolated,
		},
		{
			name:          "substitution with fallback enabled succeeds",
			requested:     "gpt-5.3-codex-spark",
			reported:      "gpt-5-codex",
			allowFallback: true,
			wantErrCode:   "",
			wantState:     adapter.ModelSubstitutionFallbackAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, err := adapter.VerifyModelIdentity(tc.requested, tc.reported, tc.allowFallback)
			if tc.wantErrCode != "" {
				if err == nil {
					t.Fatalf("expected error code %s, got nil", tc.wantErrCode)
				}
				var appErr *adapter.AppServerError
				if !errors.As(err, &appErr) || appErr.Code != tc.wantErrCode {
					t.Fatalf("expected error code %s, got %v", tc.wantErrCode, err)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if st.SubstitutionState != tc.wantState {
				t.Errorf("got substitution state %v, want %v", st.SubstitutionState, tc.wantState)
			}
		})
	}
}

// 4. Approval Bridge & HookGate Routing Tests
func TestCodexAppServer_ApprovalBridge_Routing(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, nil)

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	// 1. Test Command approval
	cmdPolicy := adapter.HookPolicy{
		DeniedTools: map[string]struct{}{
			"rm -rf /": {},
		},
	}
	cmdReqDeny := adapter.ApprovalRequest{
		RequestID: "appr-cmd-1",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindCommand,
		Command:   "rm -rf /",
	}
	respDeny := adapter.RouteApprovalRequest(ctx, cmdReqDeny, cmdPolicy)
	if respDeny.Decision != adapter.ApprovalDecisionDeny {
		t.Errorf("expected deny for dangerous command, got %v", respDeny.Decision)
	}

	cmdReqAllow := adapter.ApprovalRequest{
		RequestID: "appr-cmd-2",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindCommand,
		Command:   "git status",
	}
	respAllow := adapter.RouteApprovalRequest(ctx, cmdReqAllow, cmdPolicy)
	if respAllow.Decision != adapter.ApprovalDecisionAllow {
		t.Errorf("expected allow for safe command, got %v", respAllow.Decision)
	}

	// 2. Test File approval with ScopeWhitelist
	scopePolicy := adapter.HookPolicy{
		ScopeWhitelist: []string{"/workspace/safe"},
	}
	fileReqOut := adapter.ApprovalRequest{
		RequestID: "appr-file-1",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindFile,
		FilePath:  "/etc/passwd",
	}
	respOut := adapter.RouteApprovalRequest(ctx, fileReqOut, scopePolicy)
	if respOut.Decision != adapter.ApprovalDecisionDeny {
		t.Errorf("expected deny for out of scope file, got %v", respOut.Decision)
	}

	fileReqIn := adapter.ApprovalRequest{
		RequestID: "appr-file-2",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindFile,
		FilePath:  "/workspace/safe/main.go",
	}
	respIn := adapter.RouteApprovalRequest(ctx, fileReqIn, scopePolicy)
	if respIn.Decision != adapter.ApprovalDecisionAllow {
		t.Errorf("expected allow for in-scope file, got %v", respIn.Decision)
	}

	// 3. Test Tool approval
	toolPolicy := adapter.HookPolicy{
		DeniedTools: map[string]struct{}{
			"DangerousTool": {},
		},
	}
	toolReqDeny := adapter.ApprovalRequest{
		RequestID: "appr-tool-1",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindTool,
		ToolName:  "DangerousTool",
	}
	respToolDeny := adapter.RouteApprovalRequest(ctx, toolReqDeny, toolPolicy)
	if respToolDeny.Decision != adapter.ApprovalDecisionDeny {
		t.Errorf("expected deny for dangerous tool, got %v", respToolDeny.Decision)
	}

	toolReqSafe := adapter.ApprovalRequest{
		RequestID: "appr-tool-2",
		ThreadID:  "th-1",
		TurnID:    "tu-1",
		Kind:      adapter.ApprovalKindTool,
		ToolName:  "SafeLinter",
	}
	respSafe := adapter.RouteApprovalRequest(ctx, toolReqSafe, toolPolicy)
	if respSafe.Decision != adapter.ApprovalDecisionAllow {
		t.Errorf("expected allow for safe tool, got %v", respSafe.Decision)
	}

	// 4. Test Cancelled Context
	cancCtx, cancel := context.WithCancel(context.Background())
	cancel()
	respCancel := adapter.RouteApprovalRequest(cancCtx, toolReqSafe, toolPolicy)
	if respCancel.Decision != adapter.ApprovalDecisionCancel {
		t.Errorf("expected cancel on cancelled context, got %v", respCancel.Decision)
	}

	// 5. Respond approval call
	if err := client.RespondApproval(ctx, respSafe); err != nil {
		t.Fatalf("RespondApproval failed: %v", err)
	}
}

func TestCodexAppServer_ServerInitiatedApprovalRPC(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, func(method string, id int64, params map[string]any) (any, map[string]any, bool) {
		return nil, nil, false
	})

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	// Send server-initiated request with id 777
	writeAppServerRequest(s2cW, 777, "approval/request", map[string]any{
		"requestId": "server_appr_99",
		"threadId":  "th-1",
		"turnId":    "tu-1",
		"kind":      "tool",
		"toolName":  "Bash",
	})

	select {
	case req := <-client.ApprovalRequests():
		if req.RequestID != "server_appr_99" {
			t.Fatalf("got request ID %q, want %q", req.RequestID, "server_appr_99")
		}
		// Respond
		err := client.RespondApproval(ctx, adapter.ApprovalResponse{
			RequestID:  req.RequestID,
			Decision:   adapter.ApprovalDecisionAllow,
			ReasonCode: "allow",
		})
		if err != nil {
			t.Fatalf("RespondApproval: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for approval request")
	}
}

// 5. Event Mapping & Sequence Numbers Tests
func TestCodexAppServer_EventMappingAndDeterministicSequence(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, nil)

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	events := []struct {
		method   string
		evType   string
		expected string
	}{
		{"event", "turn.started", "turn_start"},
		{"event", "item.tool_call", "tool_call"},
		{"event", "item.file_change", "file_change"},
		{"event", "test.result", "test_result"},
		{"event", "turn.finished", "turn_end"},
		{"event", "error", "error"},
	}

	for _, e := range events {
		writeAppNotif(s2cW, e.method, map[string]any{
			"type":     e.evType,
			"threadId": "th_seq_test",
			"turnId":   "tu_seq_test",
		})
	}

	var prevSeq int64 = 0
	for i, e := range events {
		select {
		case rtEv := <-client.Events():
			if rtEv.SequenceNum <= prevSeq {
				t.Fatalf("sequence number %d not monotonically increasing from %d", rtEv.SequenceNum, prevSeq)
			}
			prevSeq = rtEv.SequenceNum

			agentEv := rtEv.ToAgentEvent("th_seq_test", int64(i+1))
			var _ protocol.AgentEvent = agentEv
			if agentEv.EventType != e.expected {
				t.Errorf("event %d: got type %q, want %q", i, agentEv.EventType, e.expected)
			}
			if agentEv.SequenceNum != int64(i+1) {
				t.Errorf("event %d: got sequence num %d, want %d", i, agentEv.SequenceNum, i+1)
			}
			if agentEv.SessionID != "th_seq_test" {
				t.Errorf("event %d: got session ID %q, want %q", i, agentEv.SessionID, "th_seq_test")
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

// 6. Malformed and Oversized Handling Tests
func TestCodexAppServer_MalformedAndOversizedMessages(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, nil)

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout:  5 * time.Second,
		MaxMessageBytes: 1048576, // 1MB
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	// Send malformed JSON line
	_, _ = s2cW.Write([]byte("{invalid json corrupt\n"))

	// Send oversized line (> 1MB) asynchronously so unbuffered pipe does not deadlock
	oversizedLine := strings.Repeat("A", 1048576+50) + "\n"
	go func() {
		_, _ = s2cW.Write([]byte(oversizedLine))
		writeAppNotif(s2cW, "event", map[string]any{
			"type":     "turn.started",
			"threadId": "th-alive",
		})
	}()

	select {
	case ev := <-client.Events():
		if ev.ThreadID != "th-alive" {
			t.Errorf("got event threadId %q, want %q", ev.ThreadID, "th-alive")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reader loop died or blocked after malformed/oversized messages")
	}

	// Client sending oversized request (> 1MB) fails immediately with ErrCodeProtocolOversized
	hugePrompt := strings.Repeat("x", 1048576+100)
	_, err := client.StartTurn(ctx, adapter.TurnStartRequest{
		ThreadID: "th-1",
		Prompt:   hugePrompt,
	})
	if err == nil {
		t.Fatal("expected StartTurn with huge payload to fail with ErrCodeProtocolOversized")
	}
	var appErr *adapter.AppServerError
	if !errors.As(err, &appErr) || appErr.Code != adapter.ErrCodeProtocolOversized {
		t.Errorf("expected ErrCodeProtocolOversized, got %v", err)
	}
}

// 7. Cancellation & Backpressure Safety Tests
func TestCodexAppServer_CancellationAndBackpressure(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	go fakeAppServer(t, c2sR, s2cW, func(method string, id int64, params map[string]any) (any, map[string]any, bool) {
		if method == "turn/start" {
			// Intentionally delay response
			time.Sleep(500 * time.Millisecond)
			return map[string]any{"turnId": "delayed_turn"}, nil, true
		}
		return nil, nil, false
	})

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout:   5 * time.Second,
		EventsQueueDepth: 2, // small queue to test backpressure
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	// Context cancellation
	shortCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	_, err := client.StartTurn(shortCtx, adapter.TurnStartRequest{
		ThreadID: "th-cancel",
		Prompt:   "slow prompt",
	})
	if err == nil {
		t.Fatal("expected cancelled request to return error")
	}

	// Flood events beyond EventsQueueDepth without reading
	for i := 0; i < 20; i++ {
		writeAppNotif(s2cW, "event", map[string]any{
			"type":     "item.delta",
			"threadId": fmt.Sprintf("th-%d", i),
		})
	}

	// Verify reader is still responsive to new calls
	time.Sleep(50 * time.Millisecond)
	th, err := client.StartThread(ctx, adapter.ThreadStartRequest{
		ModelID: "gpt-5.3-codex-spark",
	})
	if err != nil {
		t.Fatalf("StartThread after backpressure flood failed: %v", err)
	}
	if th.ID != "th_test_100" {
		t.Errorf("got thread ID %q, want %q", th.ID, "th_test_100")
	}
}

// 8. Closed Typed Errors Mapping Tests
func TestCodexAppServer_ClosedTypedErrorsMapping(t *testing.T) {
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()
	defer c2sR.Close()
	defer c2sW.Close()
	defer s2cR.Close()
	defer s2cW.Close()

	var currentErr map[string]any
	var mu sync.Mutex

	go fakeAppServer(t, c2sR, s2cW, func(method string, id int64, params map[string]any) (any, map[string]any, bool) {
		if method == "turn/start" {
			mu.Lock()
			e := currentErr
			mu.Unlock()
			if e != nil {
				return nil, e, true
			}
		}
		return nil, nil, false
	})

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout: 5 * time.Second,
	}
	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer client.Close(ctx)

	errorCases := []struct {
		name        string
		serverErr   map[string]any
		expectedErr *adapter.AppServerError
	}{
		{
			name:        "auth required (401)",
			serverErr:   map[string]any{"code": 401, "message": "unauthorized / auth required"},
			expectedErr: adapter.ErrAuthRequired,
		},
		{
			name:        "auth expired",
			serverErr:   map[string]any{"code": -32000, "message": "session token expired"},
			expectedErr: adapter.ErrAuthExpired,
		},
		{
			name:        "rate limited (429)",
			serverErr:   map[string]any{"code": 429, "message": "quota exceeded rate limit"},
			expectedErr: adapter.ErrRateLimited,
		},
		{
			name:        "unsupported version",
			serverErr:   map[string]any{"code": -32000, "message": "unsupported protocol version mismatch"},
			expectedErr: adapter.ErrUnsupportedVersion,
		},
		{
			name:        "model unavailable",
			serverErr:   map[string]any{"code": -32000, "message": "model unavailable: gpt-5.3-codex-spark not found"},
			expectedErr: adapter.ErrModelUnavailable,
		},
		{
			name:        "approval unsupported",
			serverErr:   map[string]any{"code": -32000, "message": "approval unsupported in this configuration"},
			expectedErr: adapter.ErrApprovalUnsupported,
		},
	}

	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			currentErr = tc.serverErr
			mu.Unlock()

			_, err := client.StartTurn(ctx, adapter.TurnStartRequest{
				ThreadID: "th-err-test",
				Prompt:   "test error prompt",
			})
			if err == nil {
				t.Fatalf("expected error %v, got nil", tc.expectedErr)
			}
			if !errors.Is(err, tc.expectedErr) {
				t.Errorf("errors.Is(err, %v) = false, got %v", tc.expectedErr, err)
			}
			var appErr *adapter.AppServerError
			if !errors.As(err, &appErr) || appErr.Code != tc.expectedErr.Code {
				t.Errorf("got error code %v, want %v", appErr.Code, tc.expectedErr.Code)
			}
		})
	}
}

// 9. Fake Process Lifecycle & Clean Shutdown Tests
func TestCodexAppServer_ProcessLifecycle_CleanShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Tested in dedicated Windows Job Object process tree test")
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "mock_server.go")
	body := `package main
import (
  "bufio"
  "encoding/json"
  "os"
)
func main() {
  sc := bufio.NewScanner(os.Stdin)
  for sc.Scan() {
    var req map[string]any
    if json.Unmarshal(sc.Bytes(), &req) == nil {
      id, _ := req["id"].(float64)
      method, _ := req["method"].(string)
      if method == "initialize" {
        resp, _ := json.Marshal(map[string]any{
          "jsonrpc": "2.0",
          "id": int64(id),
          "result": map[string]any{
            "serverInfo": map[string]any{"name": "mock"},
            "protocolVersion": 1,
          },
        })
        os.Stdout.Write(append(resp, '\n'))
      }
    }
  }
}
`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "mock_server")
	build := exec.Command("go", "build", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build mock server: %v\n%s", err, out)
	}

	client := adapter.NewCodexAppServerClient(adapter.CodexAppServerConfig{
		Executable:     bin,
		StartupTimeout: 5 * time.Second,
	})

	ctx := context.Background()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := client.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
