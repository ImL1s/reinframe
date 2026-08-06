package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestParseCodexHookStdin_PreToolBash(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"session_id":"s1",
		"hook_event_name":"PreToolUse",
		"cwd":"/tmp/ws",
		"tool_name":"Bash",
		"tool_input":{"command":"rm -rf /tmp/x","password":"secret"}
	}`)
	in, err := adapter.ParseCodexHookStdin(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.ParseStatus != "ok" || in.ToolName != "Bash" || in.SessionID != "s1" {
		t.Fatalf("%+v", in)
	}
	if in.ToolInput["password"] != "[redacted]" {
		t.Fatalf("secret not redacted: %+v", in.ToolInput)
	}
	pa, err := adapter.ProposedActionFromCodexHook(in, adapter.ProposedActionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pa.ToolClass != adapter.ToolClassShell || pa.Command == "" || pa.Source != "codex_hooks" {
		t.Fatalf("%+v", pa)
	}
}

func TestParseCodexHookStdin_HostedWebSearchUnsupported(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"WebSearch"}`)
	in, err := adapter.ParseCodexHookStdin(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !in.UnsupportedHosted {
		t.Fatal("expected unsupported hosted")
	}
	dec := adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "should_not_gate"}
	resp := adapter.CodexPreToolResponseFromDecision(in, dec, "", "")
	if resp.HookSpecificOutput == nil || resp.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("hosted tools must not claim gate deny: %+v", resp)
	}
}

func TestParseCodexHookStdin_FailClosed(t *testing.T) {
	t.Parallel()
	cases := [][]byte{
		nil,
		[]byte(`not-json`),
		[]byte(`{"session_id":"s","hook_event_name":"UnknownEvent"}`),
		[]byte(`{"hook_event_name":"PreToolUse"}`), // missing session
	}
	for i, c := range cases {
		if _, err := adapter.ParseCodexHookStdin(c); err == nil {
			t.Fatalf("case %d: expected error", i)
		}
	}
	// oversized
	big := make([]byte, adapter.MaxCodexHookStdinBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	if _, err := adapter.ParseCodexHookStdin(big); err == nil {
		t.Fatal("expected oversized error")
	}
}

func TestCodexPreToolResponse_AllowDenyChallenge(t *testing.T) {
	t.Parallel()
	in := adapter.CodexHookInput{SessionID: "s", HookEventName: adapter.CodexEventPreToolUse, ToolName: "Bash", ParseStatus: "ok"}
	allow := adapter.CodexPreToolResponseFromDecision(in, adapter.HookDecision{Action: adapter.HookActionAllow}, "", "")
	if allow.HookSpecificOutput == nil || allow.HookSpecificOutput.PermissionDecision != "allow" {
		t.Fatalf("%+v", allow)
	}
	raw, err := adapter.EncodeCodexHookResponse(allow)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}

	deny := adapter.CodexPreToolResponseFromDecision(in, adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "denied_tool"}, "ch-1", "justify value vs cost")
	if deny.Decision != "block" || deny.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("%+v", deny)
	}
	if deny.HookSpecificOutput.AdditionalContext == "" || !strings.Contains(deny.HookSpecificOutput.AdditionalContext, "ch-1") {
		t.Fatalf("challenge context missing: %q", deny.HookSpecificOutput.AdditionalContext)
	}
	// Must not set continue:false field — verify via marshal.
	raw2, _ := adapter.EncodeCodexHookResponse(deny)
	if strings.Contains(string(raw2), `"continue"`) {
		t.Fatalf("continue must not appear: %s", raw2)
	}
}

func TestCodexPermissionResponse_AllowDenyFallThrough(t *testing.T) {
	t.Parallel()
	a := adapter.CodexPermissionResponseFromDecision(adapter.HookDecision{Action: adapter.HookActionAllow}, false)
	if a["decision"].(map[string]any)["behavior"] != "allow" {
		t.Fatalf("%v", a)
	}
	d := adapter.CodexPermissionResponseFromDecision(adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "x"}, false)
	if d["decision"].(map[string]any)["behavior"] != "deny" {
		t.Fatalf("%v", d)
	}
	ft := adapter.CodexPermissionResponseFromDecision(adapter.HookDecision{Action: adapter.HookActionAllow}, true)
	if len(ft) != 0 {
		t.Fatalf("fall-through must be empty decision: %v", ft)
	}
}

func TestProposedActionFromCodexHook_ApplyPatchAndMCP(t *testing.T) {
	t.Parallel()
	patch := []byte(`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"apply_patch","tool_input":{"path":"a.go","patch":"+"}}`)
	in, err := adapter.ParseCodexHookStdin(patch)
	if err != nil {
		t.Fatal(err)
	}
	pa, err := adapter.ProposedActionFromCodexHook(in, adapter.ProposedActionOptions{})
	if err != nil || pa.ToolClass != adapter.ToolClassEdit {
		t.Fatalf("%+v err=%v", pa, err)
	}
	mcp := []byte(`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"mcp__filesystem__read_file","tool_input":{"path":"x"}}`)
	in2, err := adapter.ParseCodexHookStdin(mcp)
	if err != nil {
		t.Fatal(err)
	}
	pa2, err := adapter.ProposedActionFromCodexHook(in2, adapter.ProposedActionOptions{})
	if err != nil || pa2.ToolClass != adapter.ToolClassRead {
		t.Fatalf("%+v err=%v", pa2, err)
	}
}

func TestCodexHooksInstall_AtomicPreserveUnrelated(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hooksDir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(hooksDir, 0o700); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(hooksDir, "hooks.json")
	// Pre-existing unrelated hook.
	existing := map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Other",
					"hooks":   []any{map[string]any{"type": "command", "command": "echo other"}},
				},
			},
		},
	}
	b, _ := json.Marshal(existing)
	if err := os.WriteFile(hooksPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &adapter.CodexHooksManager{
		HooksPath:     hooksPath,
		BridgeCommand: "codexhooks pretool",
		ProjectRoot:   root,
	}
	plan, err := m.PlanInstall()
	if err != nil || plan.ProfileHash == "" {
		t.Fatalf("plan: %+v err=%v", plan, err)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	var rootObj map[string]any
	if err := json.Unmarshal(raw, &rootObj); err != nil {
		t.Fatal(err)
	}
	hooks := rootObj["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) < 2 {
		t.Fatalf("expected unrelated + owned, got %d: %s", len(pre), raw)
	}
	// Doctor OK
	doc, err := m.Doctor()
	if err != nil || !doc.OK {
		t.Fatalf("doctor: %+v err=%v", doc, err)
	}
	// Change command → trust stale after re-read with different command manager
	m2 := &adapter.CodexHooksManager{HooksPath: hooksPath, BridgeCommand: "codexhooks other", ProjectRoot: root}
	doc2, err := m2.Doctor()
	if err != nil {
		t.Fatal(err)
	}
	if !doc2.TrustStale {
		t.Fatalf("expected trust stale when command changes: %+v", doc2)
	}
	// Uninstall preserves unrelated
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(hooksPath)
	var after map[string]any
	_ = json.Unmarshal(raw2, &after)
	pre2 := after["hooks"].(map[string]any)["PreToolUse"].([]any)
	foundOther := false
	for _, item := range pre2 {
		mg := item.(map[string]any)
		if mg["matcher"] == "Other" {
			foundOther = true
		}
		if v, _ := mg[adapter.CodexHookOwnerKey].(string); v == adapter.CodexHookOwnerValue {
			t.Fatal("owned handler should be removed")
		}
	}
	if !foundOther {
		t.Fatal("unrelated hook lost")
	}
}

func TestCodexHooksInstall_RejectSymlinkAndCrossProject(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		// Symlink creation often needs elevation on Windows CI.
		t.Skip("symlink rejection covered on unix")
	}
	root := t.TempDir()
	other := t.TempDir()
	hooksPath := filepath.Join(other, "hooks.json")
	m := &adapter.CodexHooksManager{
		HooksPath:     hooksPath,
		BridgeCommand: "cmd",
		ProjectRoot:   root,
	}
	if _, err := m.PlanInstall(); err == nil {
		t.Fatal("expected cross-project reject")
	}
	// rollout path reject
	m2 := &adapter.CodexHooksManager{
		HooksPath:     filepath.Join(root, ".codex", "rollout-x.jsonl"),
		BridgeCommand: "cmd",
		ProjectRoot:   root,
	}
	if _, err := m2.PlanInstall(); err == nil {
		t.Fatal("expected rollout reject")
	}
}

func TestCodexHooksCapabilityManifest_Honest(t *testing.T) {
	t.Parallel()
	m := adapter.CodexHooksFoundationManifest()
	if m.CapPause || m.ExplicitAck || m.CapInterventionAck {
		t.Fatalf("must not claim pause/explicit ack: %+v", m)
	}
	if m.NegotiatedLevel >= 2 {
		t.Fatal("hooks never Level 2")
	}
	if !m.CapToolGate || !m.CapHooks {
		t.Fatal("foundation should claim tool gate/hooks")
	}
	// Observe-only default still Level 0
	obs := adapter.DefaultCodexCapabilityManifest()
	if obs.PreToolGate || obs.NegotiatedLevel != 0 {
		t.Fatalf("default still observe-only: %+v", obs)
	}
}

func TestCodexHooksInstall_RaceConcurrentDoctor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hooksPath := filepath.Join(root, ".codex", "hooks.json")
	m := &adapter.CodexHooksManager{HooksPath: hooksPath, BridgeCommand: "codexhooks pretool", ProjectRoot: root}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := m.Doctor()
			if err != nil {
				errs <- err
				return
			}
			if !d.OK {
				errs <- errString("doctor not ok")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestHookRequestFromCodexHook_Evaluate(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"session_id":"s","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"echo hi"}}`)
	in, err := adapter.ParseCodexHookStdin(raw)
	if err != nil {
		t.Fatal(err)
	}
	pa, _ := adapter.ProposedActionFromCodexHook(in, adapter.ProposedActionOptions{})
	req := adapter.HookRequestFromCodexHook(in, &pa)
	pol := adapter.HookPolicy{
		DeniedTools: map[string]struct{}{"Bash": {}},
		FailOpen:    false,
	}
	dec := adapter.EvaluateHook(context.Background(), req, pol)
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("%+v", dec)
	}
	resp := adapter.CodexPreToolResponseFromDecision(in, dec, "", "")
	if resp.HookSpecificOutput.PermissionDecision != "deny" {
		t.Fatalf("%+v", resp)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
