package adapter

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Reinframe Claude hook ownership markers (#106 / #117).
const (
	ClaudeHookOwnerKey      = "reinframe_owner"
	ClaudeHookOwnerValue    = "reinframe"
	ClaudeHookSchemaVersion = "reinframe.claude_hook.v1"
	ClaudeHookSchemaKey     = "reinframe_schema_version"
	// ClaudeHookCommandToken is historical documentation only — ownership must
	// not use substring matching (#117). Kept for migration dry-run reports.
	ClaudeHookCommandToken = "claudebridge"
)

// ClaudeSettingsManager mutates Claude Code settings JSON for project-local hooks.
// Unsupported shapes fail closed. Install/Uninstall only touch Reinframe-owned entries.
type ClaudeSettingsManager struct {
	// SettingsPath is the JSON file to edit (e.g. .claude/settings.json under project).
	SettingsPath string
	// BridgeCommand is the exact command string installed for PreToolUse.
	BridgeCommand string
	// AllowLegacyExactCommandMigration when true, PlanInstall reports exact
	// BridgeCommand matches without owner marker as migrate candidates — never auto-delete.
	AllowLegacyExactCommandMigration bool
}

// ClaudeInstallPlan is a dry-run description of settings changes.
type ClaudeInstallPlan struct {
	SettingsPath     string   `json:"settings_path"`
	WouldCreate      bool     `json:"would_create"`
	WouldBackup      bool     `json:"would_backup"`
	Actions          []string `json:"actions"`
	OwnedHooks       int      `json:"owned_hooks"`
	LegacyCandidates int      `json:"legacy_candidates,omitempty"`
	AmbiguousSkipped int      `json:"ambiguous_skipped,omitempty"`
}

// DoctorResult reports whether Reinframe-owned hooks are present and well-formed.
type DoctorResult struct {
	SettingsPath string   `json:"settings_path"`
	Exists       bool     `json:"exists"`
	OK           bool     `json:"ok"`
	Messages     []string `json:"messages"`
	OwnedHooks   int      `json:"owned_hooks"`
	ValidOwned   int      `json:"valid_owned"`
}

// settingsFileState captures content for concurrency checks.
type settingsFileState struct {
	bytes []byte
	mode  fs.FileMode
	size  int64
	mod   int64 // unix nano
}

// PlanInstall returns what install would do without writing.
func (m *ClaudeSettingsManager) PlanInstall() (ClaudeInstallPlan, error) {
	if m.SettingsPath == "" {
		return ClaudeInstallPlan{}, fmt.Errorf("claude settings: SettingsPath required")
	}
	if strings.TrimSpace(m.BridgeCommand) == "" {
		return ClaudeInstallPlan{}, fmt.Errorf("claude settings: BridgeCommand required")
	}
	if err := m.rejectSymlinkPath(); err != nil {
		return ClaudeInstallPlan{}, err
	}
	plan := ClaudeInstallPlan{SettingsPath: m.SettingsPath}
	if _, err := os.Lstat(m.SettingsPath); os.IsNotExist(err) {
		plan.WouldCreate = true
		plan.Actions = append(plan.Actions, "create settings file")
	} else if err != nil {
		return plan, err
	} else {
		plan.WouldBackup = true
		plan.Actions = append(plan.Actions, "backup existing settings")
	}
	plan.Actions = append(plan.Actions, "ensure PreToolUse reinframe-owned hook (exact ownership marker)")
	plan.OwnedHooks = 1
	if m.AllowLegacyExactCommandMigration {
		if st, err := m.readState(); err == nil && len(st.bytes) > 0 {
			var root map[string]any
			if json.Unmarshal(st.bytes, &root) == nil {
				plan.LegacyCandidates, plan.AmbiguousSkipped = countLegacyCandidates(root, m.BridgeCommand)
				if plan.LegacyCandidates > 0 {
					plan.Actions = append(plan.Actions, fmt.Sprintf("legacy exact-command candidates=%d (dry-run only; not auto-deleted)", plan.LegacyCandidates))
				}
			}
		}
	}
	return plan, nil
}

// Install merges Reinframe PreToolUse hook into settings (idempotent).
func (m *ClaudeSettingsManager) Install() error {
	if _, err := m.PlanInstall(); err != nil {
		return err
	}
	if err := m.rejectSymlinkPath(); err != nil {
		return err
	}
	before, err := m.readState()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	root, err := m.parseRoot(before.bytes, os.IsNotExist(err))
	if err != nil {
		return err
	}
	hooks, err := requireHooksObject(root, true)
	if err != nil {
		return err
	}
	pre, err := requirePreToolUseArray(hooks, true)
	if err != nil {
		return err
	}
	// Drop only exact owned entries, then append one canonical entry.
	pre = filterOwnedHooksStrict(pre, true)
	pre = append(pre, m.ownedHookEntry())
	hooks["PreToolUse"] = pre
	root["hooks"] = hooks
	if err := m.backupIfExists(); err != nil {
		return err
	}
	return m.writeAtomic(root, before)
}

// Uninstall removes only Reinframe-owned hook entries (exact ownership).
func (m *ClaudeSettingsManager) Uninstall() error {
	if m.SettingsPath == "" {
		return fmt.Errorf("claude settings: SettingsPath required")
	}
	if err := m.rejectSymlinkPath(); err != nil {
		return err
	}
	before, err := m.readState()
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	root, err := m.parseRoot(before.bytes, false)
	if err != nil {
		return err
	}
	hooks, err := requireHooksObject(root, false)
	if err != nil {
		return err
	}
	if hooks == nil {
		return nil
	}
	pre, err := requirePreToolUseArray(hooks, false)
	if err != nil {
		return err
	}
	if pre == nil {
		return nil
	}
	hooks["PreToolUse"] = filterOwnedHooksStrict(pre, true)
	root["hooks"] = hooks
	if err := m.backupIfExists(); err != nil {
		return err
	}
	return m.writeAtomic(root, before)
}

// Doctor validates Reinframe-owned hooks with full nested schema checks.
func (m *ClaudeSettingsManager) Doctor() (DoctorResult, error) {
	res := DoctorResult{SettingsPath: m.SettingsPath}
	if m.SettingsPath == "" {
		return res, fmt.Errorf("claude settings: SettingsPath required")
	}
	if err := m.rejectSymlinkPath(); err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	before, err := m.readState()
	if err != nil {
		if os.IsNotExist(err) {
			res.Messages = append(res.Messages, "settings file missing")
			return res, nil
		}
		return res, err
	}
	res.Exists = true
	root, err := m.parseRoot(before.bytes, false)
	if err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	hooks, err := requireHooksObject(root, false)
	if err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	if hooks == nil {
		res.Messages = append(res.Messages, "no hooks object")
		return res, nil
	}
	pre, err := requirePreToolUseArray(hooks, false)
	if err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	if pre == nil {
		res.Messages = append(res.Messages, "no PreToolUse array")
		return res, nil
	}
	for _, h := range pre {
		if !isOwnedHookStrict(h) {
			continue
		}
		res.OwnedHooks++
		if err := validateOwnedHookSchema(h, m.BridgeCommand); err != nil {
			res.Messages = append(res.Messages, "owned hook invalid: "+err.Error())
			continue
		}
		res.ValidOwned++
	}
	if res.ValidOwned == 0 {
		if res.OwnedHooks > 0 {
			res.Messages = append(res.Messages, "owned markers present but schema invalid (not OK)")
		} else {
			res.Messages = append(res.Messages, "no reinframe-owned PreToolUse hooks")
		}
		return res, nil
	}
	res.OK = true
	res.Messages = append(res.Messages, fmt.Sprintf("valid owned PreToolUse hooks=%d", res.ValidOwned))
	return res, nil
}

func (m *ClaudeSettingsManager) ownedHookEntry() map[string]any {
	return map[string]any{
		ClaudeHookOwnerKey:  ClaudeHookOwnerValue,
		ClaudeHookSchemaKey: ClaudeHookSchemaVersion,
		"matcher":           ".*",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": m.BridgeCommand,
			},
		},
	}
}

func (m *ClaudeSettingsManager) rejectSymlinkPath() error {
	if m.SettingsPath == "" {
		return nil
	}
	// Fail closed if path is a symlink (or intermediate reparse on platforms that report ModeSymlink).
	fi, err := os.Lstat(m.SettingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Parent must not be a symlink redirecting writes.
			dir := filepath.Dir(m.SettingsPath)
			dfi, derr := os.Lstat(dir)
			if derr == nil && dfi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("claude settings: settings parent is symlink (fail closed)")
			}
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("claude settings: settings path is symlink (fail closed)")
	}
	return nil
}

func (m *ClaudeSettingsManager) readState() (settingsFileState, error) {
	fi, err := os.Lstat(m.SettingsPath)
	if err != nil {
		return settingsFileState{}, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return settingsFileState{}, fmt.Errorf("claude settings: settings path is symlink (fail closed)")
	}
	b, err := os.ReadFile(m.SettingsPath)
	if err != nil {
		return settingsFileState{}, err
	}
	return settingsFileState{bytes: b, mode: fi.Mode().Perm(), size: fi.Size(), mod: fi.ModTime().UnixNano()}, nil
}

func (m *ClaudeSettingsManager) parseRoot(b []byte, allowMissing bool) (map[string]any, error) {
	if len(b) == 0 {
		if allowMissing {
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
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

func requireHooksObject(root map[string]any, create bool) (map[string]any, error) {
	v, ok := root["hooks"]
	if !ok || v == nil {
		if create {
			h := map[string]any{}
			root["hooks"] = h
			return h, nil
		}
		return nil, nil
	}
	h, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("claude settings: hooks must be object (fail closed)")
	}
	return h, nil
}

func requirePreToolUseArray(hooks map[string]any, create bool) ([]any, error) {
	v, ok := hooks["PreToolUse"]
	if !ok || v == nil {
		if create {
			return []any{}, nil
		}
		return nil, nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("claude settings: PreToolUse must be array (fail closed)")
	}
	return arr, nil
}

func (m *ClaudeSettingsManager) writeAtomic(root map[string]any, before settingsFileState) error {
	if err := os.MkdirAll(filepath.Dir(m.SettingsPath), 0o755); err != nil {
		return err
	}
	// Concurrency: if file existed, require unchanged size+mtime since read.
	if before.mod != 0 {
		fi, err := os.Lstat(m.SettingsPath)
		if err != nil {
			return fmt.Errorf("claude settings: concurrent change detected (stat): %w", err)
		}
		if fi.Size() != before.size || fi.ModTime().UnixNano() != before.mod {
			return fmt.Errorf("claude settings: concurrent modification detected (fail closed)")
		}
	}
	b, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	mode := fs.FileMode(0o600)
	if before.mode != 0 {
		// Preserve original permissions, but never widen beyond 0o600 for this path.
		mode = before.mode
		if mode&0o077 != 0 {
			mode = 0o600
		}
	}
	tmp := m.SettingsPath + ".reinframe.tmp"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, m.SettingsPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func (m *ClaudeSettingsManager) backupIfExists() error {
	fi, err := os.Lstat(m.SettingsPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("claude settings: refuse backup of symlink")
	}
	src, err := os.ReadFile(m.SettingsPath)
	if err != nil {
		return err
	}
	// Versioned backup with retention: keep .reinframe.bak and rotate prior once.
	bak := m.SettingsPath + ".reinframe.bak"
	prev := m.SettingsPath + ".reinframe.bak.1"
	if _, err := os.Stat(bak); err == nil {
		_ = os.Rename(bak, prev)
	}
	return os.WriteFile(bak, src, 0o600)
}

// isOwnedHookStrict requires exact ownership marker (no substring command match).
func isOwnedHookStrict(h any) bool {
	m, ok := h.(map[string]any)
	if !ok {
		return false
	}
	v, _ := m[ClaudeHookOwnerKey].(string)
	return v == ClaudeHookOwnerValue
}

func validateOwnedHookSchema(h any, expectCmd string) error {
	m, ok := h.(map[string]any)
	if !ok {
		return fmt.Errorf("not an object")
	}
	if v, _ := m[ClaudeHookOwnerKey].(string); v != ClaudeHookOwnerValue {
		return fmt.Errorf("missing owner")
	}
	if v, _ := m[ClaudeHookSchemaKey].(string); v != ClaudeHookSchemaVersion {
		return fmt.Errorf("schema version want %s got %q", ClaudeHookSchemaVersion, v)
	}
	if _, ok := m["matcher"].(string); !ok {
		return fmt.Errorf("matcher required string")
	}
	hooks, ok := m["hooks"].([]any)
	if !ok || len(hooks) == 0 {
		return fmt.Errorf("hooks array required")
	}
	hm, ok := hooks[0].(map[string]any)
	if !ok {
		return fmt.Errorf("hook entry not object")
	}
	if t, _ := hm["type"].(string); t != "command" {
		return fmt.Errorf("hook type must be command")
	}
	cmd, _ := hm["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("command required")
	}
	if expectCmd != "" && cmd != expectCmd {
		// Still valid ownership if command differs (operator upgraded binary path).
		// Doctor OK as long as schema is well-formed; message optional.
		_ = expectCmd
	}
	return nil
}

// filterOwnedHooksStrict: dropOwned true → remove owned; false → keep all (unused).
func filterOwnedHooksStrict(pre []any, dropOwned bool) []any {
	out := make([]any, 0, len(pre))
	for _, h := range pre {
		owned := isOwnedHookStrict(h)
		if dropOwned {
			if !owned {
				out = append(out, h)
			}
			continue
		}
		out = append(out, h)
	}
	return out
}

func countLegacyCandidates(root map[string]any, exactCmd string) (exact int, ambiguous int) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return 0, 0
	}
	pre, _ := hooks["PreToolUse"].([]any)
	for _, h := range pre {
		m, ok := h.(map[string]any)
		if !ok {
			continue
		}
		if isOwnedHookStrict(h) {
			continue
		}
		arr, _ := m["hooks"].([]any)
		for _, hh := range arr {
			hm, ok := hh.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hm["command"].(string)
			if cmd == exactCmd {
				exact++
			} else if strings.Contains(cmd, ClaudeHookCommandToken) {
				ambiguous++
			}
		}
	}
	return exact, ambiguous
}
