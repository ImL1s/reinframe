package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestParseGrokHookStdin_PreTool(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"hookEventName":"pre_tool_use",
		"sessionId":"s1",
		"cwd":"/ws",
		"workspaceRoot":"/ws",
		"toolName":"run_terminal_command",
		"toolInput":{"command":"rm -rf /","token":"sekrit"}
	}`)
	in, err := adapter.ParseGrokHookStdin(raw)
	if err != nil {
		t.Fatal(err)
	}
	if in.HookEventName != adapter.GrokEventPreToolUse || in.SessionID != "s1" {
		t.Fatalf("%+v", in)
	}
	if in.ToolInput["token"] != "[redacted]" {
		t.Fatalf("secret: %+v", in.ToolInput)
	}
	pa, err := adapter.ProposedActionFromGrokHook(in, adapter.ProposedActionOptions{})
	if err != nil || pa.ToolClass != adapter.ToolClassShell || pa.Command == "" {
		t.Fatalf("%+v err=%v", pa, err)
	}
}

func TestGrokPreToolResponse_AllowDenyChallenge(t *testing.T) {
	t.Parallel()
	a := adapter.GrokPreToolResponseFromDecision(adapter.HookDecision{Action: adapter.HookActionAllow}, "")
	if a.Decision != "allow" {
		t.Fatalf("%+v", a)
	}
	d := adapter.GrokPreToolResponseFromDecision(adapter.HookDecision{Action: adapter.HookActionDeny, ReasonCode: "unsafe"}, "ch-9")
	if d.Decision != "deny" || d.Reason == "" {
		t.Fatalf("%+v", d)
	}
	raw, err := adapter.EncodeGrokHookResponse(d)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if m["decision"] != "deny" {
		t.Fatalf("%s", raw)
	}
}

func TestRecordHostHookOutcome_FailOpen(t *testing.T) {
	t.Parallel()
	// Intended deny but timeout exit 1 without deny JSON → fail-open
	o := adapter.RecordHostHookOutcome(true, 1, false)
	if o != adapter.HostOutcomeFailOpen {
		t.Fatalf("%v", o)
	}
	// Valid deny JSON → enforced
	if adapter.RecordHostHookOutcome(true, 0, true) != adapter.HostOutcomeEnforcedDeny {
		t.Fatal("expected enforced")
	}
	// Exit 2 is explicit deny for PreToolUse
	if adapter.RecordHostHookOutcome(true, 2, false) != adapter.HostOutcomeEnforcedDeny {
		t.Fatal("expected exit 2 enforced")
	}
	// Allow path
	if adapter.RecordHostHookOutcome(false, 0, false) != adapter.HostOutcomeAllowed {
		t.Fatal("expected allowed")
	}
}

func TestParseGrokHookStdin_FailClosed(t *testing.T) {
	t.Parallel()
	if _, err := adapter.ParseGrokHookStdin([]byte(`not-json`)); err == nil {
		t.Fatal("expected error")
	}
	if _, err := adapter.ParseGrokHookStdin([]byte(`{"hookEventName":"PreToolUse"}`)); err == nil {
		t.Fatal("session required")
	}
}

func TestGrokHooksInstall_AtomicDoctorUninstall(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	hooksFile := filepath.Join(root, ".grok", "hooks", "reinframe-pretool.json")
	m := &adapter.GrokHooksManager{
		HooksFile:     hooksFile,
		BridgeCommand: "grokhooks pretool",
		ProjectRoot:   root,
	}
	plan, err := m.PlanInstall()
	if err != nil || plan.ProfileHash == "" {
		t.Fatalf("%+v err=%v", plan, err)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	// Unrelated sibling file preserved concept: write another file
	other := filepath.Join(root, ".grok", "hooks", "user.json")
	if err := os.WriteFile(other, []byte(`{"hooks":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := m.Doctor()
	if err != nil || !doc.OK {
		t.Fatalf("%+v err=%v", doc, err)
	}
	// Stale trust
	m2 := &adapter.GrokHooksManager{HooksFile: hooksFile, BridgeCommand: "other", ProjectRoot: root}
	d2, _ := m2.Doctor()
	if !d2.TrustStale {
		t.Fatalf("%+v", d2)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hooksFile); !os.IsNotExist(err) {
		t.Fatal("owned file should be removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("unrelated file lost")
	}
}

func TestGrokHooksInstall_RejectCrossProjectAndAuth(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := &adapter.GrokHooksManager{
		HooksFile:     filepath.Join(t.TempDir(), "x.json"),
		BridgeCommand: "c",
		ProjectRoot:   root,
	}
	if _, err := m.PlanInstall(); err == nil {
		t.Fatal("cross-project")
	}
	m2 := &adapter.GrokHooksManager{
		HooksFile:     filepath.Join(root, ".grok", "auth.json"),
		BridgeCommand: "c",
		ProjectRoot:   root,
	}
	if _, err := m2.PlanInstall(); err == nil {
		t.Fatal("auth reject")
	}
}

func TestGrokHooksManifest_Honest(t *testing.T) {
	t.Parallel()
	m := adapter.NewGrokHooksFoundationManifest()
	if m.CapPause || m.ExplicitAck || m.FailClosedHooks || m.NegotiatedLevel >= 2 {
		t.Fatalf("%+v", m)
	}
	if !m.CapToolGate || !m.CapHooks {
		t.Fatal("expected gate/hooks")
	}
}

func TestGrokStaticDenyFragment(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := &adapter.GrokHooksManager{
		HooksFile:               filepath.Join(root, ".grok", "hooks", "r.json"),
		BridgeCommand:           "c",
		ProjectRoot:             root,
		OptionalPermissionsPath: filepath.Join(root, ".grok", "reinframe-deny.json"),
	}
	if err := m.InstallStaticDenyFragment([]string{"run_terminal_command"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(m.OptionalPermissionsPath)
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc[adapter.GrokHookOwnerKey] != adapter.GrokHookOwnerValue {
		t.Fatalf("%s", b)
	}
}

func TestGrokHook_EvaluateDeny(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"hookEventName":"PreToolUse","sessionId":"s","toolName":"Bash","toolInput":{"command":"x"}}`)
	in, err := adapter.ParseGrokHookStdin(raw)
	if err != nil {
		t.Fatal(err)
	}
	pa, _ := adapter.ProposedActionFromGrokHook(in, adapter.ProposedActionOptions{})
	req := adapter.HookRequestFromGrokHook(in, &pa)
	dec := adapter.EvaluateHook(context.Background(), req, adapter.HookPolicy{
		DeniedTools: map[string]struct{}{"Bash": {}},
	})
	if dec.Action != adapter.HookActionDeny {
		t.Fatalf("%+v", dec)
	}
	resp := adapter.GrokPreToolResponseFromDecision(dec, "")
	if resp.Decision != "deny" {
		t.Fatalf("%+v", resp)
	}
}

func TestGrokHooksInstall_RaceDoctor(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	m := &adapter.GrokHooksManager{
		HooksFile:     filepath.Join(root, ".grok", "hooks", "r.json"),
		BridgeCommand: "c",
		ProjectRoot:   root,
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := m.Doctor()
			if err != nil || !d.OK {
				t.Errorf("%+v %v", d, err)
			}
		}()
	}
	wg.Wait()
}
