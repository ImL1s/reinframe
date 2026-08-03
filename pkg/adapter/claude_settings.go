package adapter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reinframe Claude hook ownership markers (#106).
const (
	ClaudeHookOwnerKey     = "reinframe_owner"
	ClaudeHookOwnerValue   = "reinframe"
	ClaudeHookCommandToken = "claudebridge" // substring match for owned commands
)

// ClaudeSettingsManager mutates Claude Code settings JSON for project-local hooks.
// It does not claim support for every Claude version field; unsupported shapes fail closed.
type ClaudeSettingsManager struct {
	// SettingsPath is the JSON file to edit (e.g. .claude/settings.json under project).
	SettingsPath string
	// BridgeCommand is the command string installed for PreToolUse (absolute or PATH).
	BridgeCommand string
}

// ClaudeInstallPlan is a dry-run description of settings changes.
type ClaudeInstallPlan struct {
	SettingsPath string   `json:"settings_path"`
	WouldCreate  bool     `json:"would_create"`
	WouldBackup  bool     `json:"would_backup"`
	Actions      []string `json:"actions"`
	OwnedHooks   int      `json:"owned_hooks"`
}

// DoctorResult reports whether Reinframe-owned hooks are present and well-formed.
type DoctorResult struct {
	SettingsPath string   `json:"settings_path"`
	Exists       bool     `json:"exists"`
	OK           bool     `json:"ok"`
	Messages     []string `json:"messages"`
	OwnedHooks   int      `json:"owned_hooks"`
}

// PlanInstall returns what install would do without writing.
func (m *ClaudeSettingsManager) PlanInstall() (ClaudeInstallPlan, error) {
	if m.SettingsPath == "" {
		return ClaudeInstallPlan{}, fmt.Errorf("claude settings: SettingsPath required")
	}
	if strings.TrimSpace(m.BridgeCommand) == "" {
		return ClaudeInstallPlan{}, fmt.Errorf("claude settings: BridgeCommand required")
	}
	plan := ClaudeInstallPlan{SettingsPath: m.SettingsPath, Actions: nil}
	if _, err := os.Stat(m.SettingsPath); os.IsNotExist(err) {
		plan.WouldCreate = true
		plan.Actions = append(plan.Actions, "create settings file")
	} else if err != nil {
		return plan, err
	} else {
		plan.WouldBackup = true
		plan.Actions = append(plan.Actions, "backup existing settings")
	}
	plan.Actions = append(plan.Actions, "ensure PreToolUse reinframe-owned hook")
	plan.OwnedHooks = 1
	return plan, nil
}

// Install merges Reinframe PreToolUse hook into settings (idempotent).
func (m *ClaudeSettingsManager) Install() error {
	plan, err := m.PlanInstall()
	if err != nil {
		return err
	}
	_ = plan
	root, err := m.loadOrEmpty()
	if err != nil {
		return err
	}
	if err := m.backupIfExists(); err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}
	pre, _ := hooks["PreToolUse"].([]any)
	// Remove existing owned entries then append one.
	pre = filterOwnedHooks(pre, false)
	pre = append(pre, m.ownedHookEntry())
	hooks["PreToolUse"] = pre
	return m.write(root)
}

// Uninstall removes only Reinframe-owned hook entries.
func (m *ClaudeSettingsManager) Uninstall() error {
	if m.SettingsPath == "" {
		return fmt.Errorf("claude settings: SettingsPath required")
	}
	if _, err := os.Stat(m.SettingsPath); os.IsNotExist(err) {
		return nil
	}
	root, err := m.loadOrEmpty()
	if err != nil {
		return err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	if pre, ok := hooks["PreToolUse"].([]any); ok {
		hooks["PreToolUse"] = filterOwnedHooks(pre, true)
	}
	return m.write(root)
}

// Doctor validates Reinframe-owned hooks.
func (m *ClaudeSettingsManager) Doctor() (DoctorResult, error) {
	res := DoctorResult{SettingsPath: m.SettingsPath}
	if m.SettingsPath == "" {
		return res, fmt.Errorf("claude settings: SettingsPath required")
	}
	if _, err := os.Stat(m.SettingsPath); os.IsNotExist(err) {
		res.Messages = append(res.Messages, "settings file missing")
		return res, nil
	}
	res.Exists = true
	root, err := m.loadOrEmpty()
	if err != nil {
		return res, err
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		res.Messages = append(res.Messages, "no hooks object")
		return res, nil
	}
	pre, _ := hooks["PreToolUse"].([]any)
	for _, h := range pre {
		if isOwnedHook(h) {
			res.OwnedHooks++
		}
	}
	if res.OwnedHooks == 0 {
		res.Messages = append(res.Messages, "no reinframe-owned PreToolUse hooks")
		return res, nil
	}
	res.OK = true
	res.Messages = append(res.Messages, fmt.Sprintf("owned PreToolUse hooks=%d", res.OwnedHooks))
	return res, nil
}

func (m *ClaudeSettingsManager) ownedHookEntry() map[string]any {
	return map[string]any{
		ClaudeHookOwnerKey: ClaudeHookOwnerValue,
		"matcher":          ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": m.BridgeCommand,
			},
		},
	}
}

func (m *ClaudeSettingsManager) loadOrEmpty() (map[string]any, error) {
	b, err := os.ReadFile(m.SettingsPath)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("claude settings: malformed JSON (fail closed): %w", err)
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func (m *ClaudeSettingsManager) write(root map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(m.SettingsPath), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(m.SettingsPath, b, 0o600)
}

func (m *ClaudeSettingsManager) backupIfExists() error {
	if _, err := os.Stat(m.SettingsPath); os.IsNotExist(err) {
		return nil
	}
	src, err := os.ReadFile(m.SettingsPath)
	if err != nil {
		return err
	}
	bak := m.SettingsPath + ".reinframe.bak"
	return os.WriteFile(bak, src, 0o600)
}

func isOwnedHook(h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	if v, _ := m[ClaudeHookOwnerKey].(string); v == ClaudeHookOwnerValue {
		return true
	}
	// Fallback: command contains claudebridge token
	hooks, _ := m["hooks"].([]any)
	for _, hh := range hooks {
		hm, ok := hh.(map[string]any)
		if !ok {
			continue
		}
		cmd, _ := hm["command"].(string)
		if strings.Contains(cmd, ClaudeHookCommandToken) {
			return true
		}
	}
	return false
}

// filterOwnedHooks: if dropOwned, remove owned; else remove owned then keep others.
func filterOwnedHooks(pre []any, dropOwned bool) []any {
	out := make([]any, 0, len(pre))
	for _, h := range pre {
		owned := isOwnedHook(h)
		if dropOwned {
			if !owned {
				out = append(out, h)
			}
			continue
		}
		// rebuild without owned (caller appends fresh owned)
		if !owned {
			out = append(out, h)
		}
	}
	return out
}
