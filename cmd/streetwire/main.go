// Command streetwire is a street-level wiring demo for Reinframe.
//
// It shows how research said to connect:
//
//	Codex rollout JSONL (offline) → EventSource → detectors/policy demo
//	+ synthetic M2.0 control loop (detect→defer→deliver→ACK)
//	+ M2.1 over-SOP before_tool deny
//	+ #98 library tool-budget / hypothesis-loop fire·no-fire
//
// Usage:
//
//	go run ./cmd/streetwire                          # auto-pick newest rollout if present
//	go run ./cmd/streetwire -codex /path/to/rollout.jsonl
//	go run ./cmd/streetwire -no-codex
//
// Not a claim of production dual-host install into global Claude/Codex settings.
// #99/#100 are out of scope for this demo.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/detector"
	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/supervisor"
)

func main() {
	codexPath := flag.String("codex", "", "optional Codex rollout JSONL path (offline EventSource); empty = auto-pick newest under ~/.codex/sessions")
	noCodex := flag.Bool("no-codex", false, "skip Codex offline wire (synthetic control loop only)")
	flag.Parse()

	fmt.Println("=== Reinframe street-wire demo ===")
	fmt.Println("M2.2 residual adapters: offline/tail Codex, Claude bridge, FileActuator, #98")
	fmt.Println()

	path := *codexPath
	if !*noCodex {
		if path == "" {
			if auto, err := newestCodexRollout(); err == nil {
				path = auto
				fmt.Printf("[codex] auto-picked newest rollout\n")
			} else {
				fmt.Printf("[codex] no rollout found (%v); pass -codex PATH or run codex first\n", err)
				fmt.Println()
			}
		}
		if path != "" {
			if err := runCodexOffline(path); err != nil {
				fmt.Fprintf(os.Stderr, "codex offline: %v\n", err)
				os.Exit(1)
			}
			fmt.Println()
		}
	} else {
		fmt.Println("[codex] skipped (-no-codex)")
		fmt.Println()
	}

	if err := runSyntheticControlLoop(); err != nil {
		fmt.Fprintf(os.Stderr, "control loop: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	if err := runOverSOP(); err != nil {
		fmt.Fprintf(os.Stderr, "over-SOP: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	if err := runResidualDetectors(); err != nil {
		fmt.Fprintf(os.Stderr, "residual #98: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	if err := runClaudeBridgeDemo(); err != nil {
		fmt.Fprintf(os.Stderr, "claude bridge: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	if err := runFileActuatorDemo(); err != nil {
		fmt.Fprintf(os.Stderr, "file actuator: %v\n", err)
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("=== street-wire OK ===")
	fmt.Println("Honesty:")
	fmt.Println("  done: offline+tail Codex EventSource, Claude PreTool bridge APIs, FileActuator, #98 library+policy")
	fmt.Println("  not claimed: global host install, live Claude session E2E in CI, process attach daemon, #99/#100")
}

func runCodexOffline(path string) error {
	fmt.Println("--- A) Offline Codex rollout → EventSource (#95 scaffold) ---")
	src := &adapter.CodexRolloutSource{Path: path}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := src.Events(ctx)
	if err != nil {
		return err
	}
	// Feed shipped events into #98 tool-budget detector (library signal only).
	tb := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{
		MaxToolCalls: detector.DefaultMaxToolCalls,
	})
	var n int
	var toolNames = map[string]int{}
	var budgetFires int
	var sessionID string
	for ev := range ch {
		n++
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		if ev.EventType == "tool_call" {
			var m map[string]any
			_ = jsonUnmarshal(ev.Payload, &m)
			if name, ok := m["tool_name"].(string); ok {
				toolNames[name]++
			}
		}
		if sig, ok := tb.Observe(ev); ok && sig != nil {
			budgetFires++
		}
	}
	fmt.Printf("  file: %s\n", path)
	fmt.Printf("  lines_read=%d tool_events=%d exec=%d spawn_agent=%d\n",
		src.LinesRead, src.ToolCalls, src.ExecCalls, src.SpawnCalls)
	fmt.Printf("  session_meta: %v\n", src.SessionMeta)
	fmt.Printf("  tool histogram: %v\n", toolNames)
	fmt.Printf("  events_emitted=%d\n", n)
	// Offline parse emits no file_change progress markers, so long sessions fire.
	fmt.Printf("  #98 tool_budget max=%d session=%s tools=%d fires=%d (no progress in offline parse; library only)\n",
		detector.DefaultMaxToolCalls, sessionID, tb.ToolCount(sessionID), budgetFires)
	if src.ExecCalls >= detector.DefaultMaxToolCalls {
		fmt.Println("  note: high exec count — #82 error-loop alone may NOT fire; #98 library applies")
	}
	return nil
}

func runSyntheticControlLoop() error {
	fmt.Println("--- B) Synthetic M2.0: 3× failure → defer → deliver → ACK → allow ---")
	ctx := context.Background()
	act := adapter.NewFakeActuator()
	act.AutoAck = false
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		return err
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		return err
	}
	sess := "street-demo"
	fail := `{"error":"exit status 1: undefined: Foo"}`
	for i := 1; i <= 3; i++ {
		ev := protocol.AgentEvent{
			EventID:     fmt.Sprintf("e%d", i),
			SessionID:   sess,
			SequenceNum: int64(i),
			EventType:   "error",
			Timestamp:   time.Now().UTC(),
			Payload:     []byte(fail),
		}
		sig, iv, err := orch.HandleEvent(ctx, ev)
		if err != nil {
			return err
		}
		fmt.Printf("  failure %d: signal=%v intervention=%v\n", i, sig != nil, iv != nil)
	}
	dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	fmt.Printf("  PreTool after fire: action=%s reason=%s\n", dec.Action, dec.ReasonCode)
	item, _, err := orch.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		return err
	}
	fmt.Printf("  Deliver: calls=%d state=%s\n", act.CallCount(), item.State)
	_ = orch.Acknowledge(item.Intervention.InterventionID, adapter.AckStatusAcked)
	dec = orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	fmt.Printf("  PreTool after ACK: action=%s reason=%s\n", dec.Action, dec.ReasonCode)
	return nil
}

func runOverSOP() error {
	fmt.Println("--- C) M2.1 over-SOP: typo task + criteria met → deny full suite ---")
	ctx := context.Background()
	intake, err := adapter.IntakeFromHost(adapter.HostTaskPayload{
		SessionID: "street-sop",
		Prompt:    "fix typo in README",
	}, adapter.TaskIntakeOptions{BuildContract: true})
	if err != nil {
		return err
	}
	c := intake.Contract
	c.Complexity = protocol.ComplexitySimple
	c.Risk = protocol.RiskLow
	led := protocol.NewEvidenceLedger(c.TaskID, c.Revision)
	for _, cr := range c.SuccessCriteria {
		led.CriteriaStatus[cr.ID] = protocol.CriterionStatus{CriterionID: cr.ID, Status: "met"}
	}
	eng := policy.NewEngine(policy.EngineConfig{})
	dec := eng.EvaluateBeforeTool(ctx, policy.BeforeToolInput{
		Request:    adapter.HookRequest{SessionID: "street-sop", ToolName: "go test -race ./..."},
		Contract:   c,
		Ledger:     &led,
		BasePolicy: adapter.HookPolicy{},
	})
	fmt.Printf("  contract complexity=%s risk=%s\n", c.Complexity, c.Risk)
	fmt.Printf("  before_tool full suite: action=%s reason=%s\n", dec.Action, dec.ReasonCode)
	if dec.Action != adapter.HookActionDeny {
		return fmt.Errorf("expected deny, got %s", dec.Action)
	}
	return nil
}

func runResidualDetectors() error {
	fmt.Println("--- D) #98 residual library: tool-budget + hypothesis-loop ---")
	// Fire: 5 tools, max 5, no progress.
	tbFire := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 5})
	var fired *protocol.TunnelSignal
	for i := 1; i <= 5; i++ {
		body, _ := json.Marshal(map[string]string{"tool_name": "exec"})
		sig, ok := tbFire.Observe(protocol.AgentEvent{
			EventID:   fmt.Sprintf("t%d", i),
			SessionID: "tb-fire",
			EventType: "tool_call",
			Timestamp: time.Now().UTC(),
			Payload:   body,
		})
		if ok {
			fired = sig
		}
	}
	if fired == nil || fired.FailureMode != detector.FailureModeToolBudgetChurn {
		return fmt.Errorf("tool-budget fire missing")
	}
	fmt.Printf("  tool_budget fire: mode=%s details.max=%s\n", fired.FailureMode, fired.Details["max_tool_calls"])

	// No-fire: short session under budget.
	tbOK := detector.NewToolBudgetChurnDetector(detector.ToolBudgetConfig{MaxToolCalls: 30})
	for i := 1; i <= 7; i++ {
		body, _ := json.Marshal(map[string]string{"tool_name": "exec"})
		if sig, ok := tbOK.Observe(protocol.AgentEvent{
			SessionID: "tb-ok", EventType: "tool_call",
			Payload: body, Timestamp: time.Now().UTC(),
		}); ok {
			return fmt.Errorf("tool-budget false positive: %+v", sig)
		}
	}
	fmt.Printf("  tool_budget no-fire short session: tools=%d max=30\n", tbOK.ToolCount("tb-ok"))

	// Hypothesis loop fire / no-fire.
	hl := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
	text := "possible SSRF in webhook fetcher"
	var hlSig *protocol.TunnelSignal
	for i := 0; i < 3; i++ {
		sig, ok := hl.Observe("hl-fire", detector.HypothesisObservation{
			Text:        text,
			EvidenceIDs: []string{"hooks/fetch.go"},
		})
		if ok {
			hlSig = sig
		}
	}
	if hlSig == nil || hlSig.FailureMode != detector.FailureModeHypothesisLoop {
		return fmt.Errorf("hypothesis-loop fire missing")
	}
	fmt.Printf("  hypothesis_loop fire: mode=%s count=%s\n", hlSig.FailureMode, hlSig.Details["count"])

	hlOK := detector.NewHypothesisLoopDetector(detector.HypothesisLoopConfig{Threshold: 3})
	for _, id := range []string{"a.go", "b.go", "c.go"} {
		if sig, ok := hlOK.Observe("hl-ok", detector.HypothesisObservation{
			Text: text, EvidenceIDs: []string{id},
		}); ok {
			return fmt.Errorf("hypothesis-loop false positive on new evidence: %+v", sig)
		}
	}
	fmt.Println("  hypothesis_loop no-fire when new evidence IDs arrive")
	fmt.Println("  (library + EvaluateSlow wiring; live host auto-intervention not claimed)")
	return nil
}

func runClaudeBridgeDemo() error {
	fmt.Println("--- E) #96 Claude PreTool bridge (fixture → core gate) ---")
	// allow
	rawAllow := []byte(`{"session_id":"bridge","tool_name":"Read","tool_input":{"file_path":"x.go"}}`)
	resp, dec, err := adapter.EvaluateClaudePreToolJSON(context.Background(), rawAllow, adapter.ClaudeBridgeConfig{})
	if err != nil {
		return err
	}
	if dec.Action != adapter.HookActionAllow || resp.Decision != "approve" {
		return fmt.Errorf("allow path: dec=%+v resp=%+v", dec, resp)
	}
	fmt.Printf("  allow Read: decision=%s reinframe.action=%s\n", resp.Decision, resp.Reinframe.Action)

	// deny
	rawDeny := []byte(`{"session_id":"bridge","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	resp, dec, err = adapter.EvaluateClaudePreToolJSON(context.Background(), rawDeny, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{DeniedTools: map[string]struct{}{"Bash": {}}},
	})
	if err != nil {
		return err
	}
	if dec.Action != adapter.HookActionDeny || resp.Decision != "block" {
		return fmt.Errorf("deny path: dec=%+v resp=%+v", dec, resp)
	}
	fmt.Printf("  deny Bash: decision=%s reason=%s\n", resp.Decision, resp.Reinframe.ReasonCode)

	// defer
	resp, dec, err = adapter.EvaluateClaudePreTool(context.Background(), adapter.ClaudePreToolInput{
		SessionID: "bridge", ToolName: "Edit",
	}, adapter.ClaudeBridgeConfig{
		Policy: adapter.HookPolicy{PendingAdvisoryInterventionID: "iv-demo"},
	})
	if err != nil {
		return err
	}
	if dec.Action != adapter.HookActionDefer || resp.Decision != "block" {
		return fmt.Errorf("defer path: dec=%+v", dec)
	}
	fmt.Printf("  defer Edit: decision=%s intervention=%s\n", resp.Decision, resp.Reinframe.InterventionID)
	fmt.Println("  (CLI: go run ./cmd/claudebridge pretool; host settings install optional)")
	return nil
}

func runFileActuatorDemo() error {
	fmt.Println("--- F) #97 FileActuator (non-fake advice channel + ACK) ---")
	dir, err := os.MkdirTemp("", "reinframe-advice-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "advice.jsonl")
	act := &adapter.FileActuator{Path: path}
	del, err := adapter.NewAdvisoryDelivery(adapter.AdvisoryDeliveryConfig{
		Actuator:               act,
		SupportsAdviceDelivery: true,
		DefaultTTL:             time.Minute,
	})
	if err != nil {
		return err
	}
	orch, err := supervisor.NewOrchestrator(supervisor.Config{
		Detector: detector.NewRepeatedFailureDetector(detector.Config{Threshold: 3}),
		Policy:   policy.NewEngine(policy.EngineConfig{}),
		Delivery: del,
	})
	if err != nil {
		return err
	}
	ctx := context.Background()
	sess := "file-demo"
	fail := `{"error":"exit status 1: undefined: Z"}`
	for i := 1; i <= 3; i++ {
		ev := protocol.AgentEvent{
			EventID: fmt.Sprintf("fe%d", i), SessionID: sess, SequenceNum: int64(i),
			EventType: "error", Timestamp: time.Now().UTC(), Payload: []byte(fail),
		}
		if _, _, err := orch.HandleEvent(ctx, ev); err != nil {
			return err
		}
	}
	item, res, err := orch.DeliverAtSafeBoundary(ctx, sess)
	if err != nil {
		return err
	}
	if item == nil || res.AckStatus != adapter.AckStatusPending {
		return fmt.Errorf("item=%v res=%+v", item, res)
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		return fmt.Errorf("advice file missing: %v", err)
	}
	fmt.Printf("  delivered state=%s ack=%s bytes=%d\n", item.State, res.AckStatus, len(raw))
	if err := orch.Acknowledge(item.Intervention.InterventionID, adapter.AckStatusAcked); err != nil {
		return err
	}
	dec := orch.EvaluatePreTool(ctx, adapter.HookRequest{SessionID: sess, ToolName: "Edit"})
	fmt.Printf("  after ACK PreTool=%s (channel: reinframe.advice.v1 JSONL)\n", dec.Action)
	return nil
}

func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}

// newestCodexRollout finds the most recently modified rollout-*.jsonl under ~/.codex/sessions.
func newestCodexRollout() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(home, ".codex", "sessions")
	var best string
	var bestMod time.Time
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		base := d.Name()
		if len(base) < 8 || base[:8] != "rollout-" || filepath.Ext(base) != ".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if best == "" || info.ModTime().After(bestMod) {
			best = path
			bestMod = info.ModTime()
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if best == "" {
		return "", fmt.Errorf("no rollout-*.jsonl under %s", root)
	}
	return best, nil
}
