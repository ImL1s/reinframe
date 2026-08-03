package adapter_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

func TestClaudeSettings_InstallIdempotentPreservesForeign(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	initial := map[string]any{
		"permissions": map[string]any{"allow": []string{"Bash(ls:*)"}},
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{"matcher": "Read", "hooks": []any{map[string]any{"type": "command", "command": "echo foreign"}}},
			},
		},
	}
	b, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &adapter.ClaudeSettingsManager{
		SettingsPath:  path,
		BridgeCommand: "/usr/bin/claudebridge pretool",
	}
	plan, err := m.PlanInstall()
	if err != nil || plan.WouldCreate {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	// permissions preserved
	if root["permissions"] == nil {
		t.Fatal("permissions wiped")
	}
	hooks := root["hooks"].(map[string]any)
	pre := hooks["PreToolUse"].([]any)
	if len(pre) != 2 {
		t.Fatalf("pre hooks=%d want 2 (foreign+owned)", len(pre))
	}
	doc, err := m.Doctor()
	if err != nil || !doc.OK || doc.OwnedHooks != 1 {
		t.Fatalf("doctor=%+v err=%v", doc, err)
	}
	if err := m.Uninstall(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	_ = json.Unmarshal(raw, &root)
	pre = root["hooks"].(map[string]any)["PreToolUse"].([]any)
	if len(pre) != 1 {
		t.Fatalf("after uninstall pre=%d", len(pre))
	}
	doc, _ = m.Doctor()
	if doc.OwnedHooks != 0 {
		t.Fatalf("owned still %d", doc.OwnedHooks)
	}
}

func TestClaudeSettings_MalformedFailClosed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := &adapter.ClaudeSettingsManager{SettingsPath: path, BridgeCommand: "claudebridge pretool"}
	if err := m.Install(); err == nil {
		t.Fatal("expected fail closed")
	}
}

func TestClaudeSettings_DryRunPlanCreate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "missing", "settings.json")
	m := &adapter.ClaudeSettingsManager{SettingsPath: path, BridgeCommand: "claudebridge pretool"}
	plan, err := m.PlanInstall()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.WouldCreate {
		t.Fatalf("%+v", plan)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("dry-run must not create")
	}
}
