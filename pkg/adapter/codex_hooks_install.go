package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CodexHooksManager installs/doctors project-local Codex hooks.json (#163).
//
// Install only inside the selected repository's .codex/hooks.json.
// Never writes rollout/transcript files. Never silently bypasses trust.
type CodexHooksManager struct {
	// HooksPath is the project-local hooks.json path (e.g. <repo>/.codex/hooks.json).
	HooksPath string
	// BridgeCommand is the exact command string Codex will run for Reinframe hooks.
	BridgeCommand string
	// ProjectRoot when set is used to reject cross-project / outside-root paths.
	ProjectRoot string
}

// CodexHooksInstallPlan is a dry-run description of hooks.json changes.
type CodexHooksInstallPlan struct {
	HooksPath      string   `json:"hooks_path"`
	WouldCreate    bool     `json:"would_create"`
	WouldBackup    bool     `json:"would_backup"`
	Actions        []string `json:"actions"`
	OwnedHandlers  int      `json:"owned_handlers"`
	Profile        string   `json:"profile"`
	ProfileHash    string   `json:"profile_hash"`
	TrustHint      string   `json:"trust_hint"`
	PreserveOthers bool     `json:"preserve_others"`
}

// CodexHooksDoctorResult reports Reinframe-owned Codex hooks health and trust hash.
type CodexHooksDoctorResult struct {
	HooksPath     string   `json:"hooks_path"`
	Exists        bool     `json:"exists"`
	OK            bool     `json:"ok"`
	Messages      []string `json:"messages"`
	OwnedHandlers int      `json:"owned_handlers"`
	ValidOwned    int      `json:"valid_owned"`
	ProfileHash   string   `json:"profile_hash,omitempty"`
	// TrustStale is true when stored profile hash differs from current command/profile.
	TrustStale bool `json:"trust_stale,omitempty"`
}

// PlanInstall describes install without writing.
func (m *CodexHooksManager) PlanInstall() (CodexHooksInstallPlan, error) {
	if err := m.validatePaths(); err != nil {
		return CodexHooksInstallPlan{}, err
	}
	plan := CodexHooksInstallPlan{
		HooksPath:      m.HooksPath,
		Profile:        CodexHooksProfileV1,
		ProfileHash:    ProfileContentHash(m.BridgeCommand, CodexHooksProfileV1),
		PreserveOthers: true,
		TrustHint: fmt.Sprintf(
			"Codex requires /hooks review+trust for command=%q profile=%s hash=%s",
			m.BridgeCommand, CodexHooksProfileV1, ProfileContentHash(m.BridgeCommand, CodexHooksProfileV1),
		),
	}
	if _, err := os.Lstat(m.HooksPath); os.IsNotExist(err) {
		plan.WouldCreate = true
		plan.Actions = append(plan.Actions, "create hooks.json")
	} else if err != nil {
		return plan, err
	} else {
		plan.WouldBackup = true
		plan.Actions = append(plan.Actions, "backup existing hooks.json")
	}
	plan.Actions = append(plan.Actions,
		"merge reinframe-owned PreToolUse/PermissionRequest/SessionStart/UserPromptSubmit/PostToolUse/Stop handlers",
		"preserve unrelated user hooks",
	)
	plan.OwnedHandlers = 6
	return plan, nil
}

// Install merges Reinframe-owned handlers into hooks.json (idempotent).
func (m *CodexHooksManager) Install() error {
	if _, err := m.PlanInstall(); err != nil {
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
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	// Remove only reinframe-owned matcher groups, then append canonical set.
	for _, event := range []string{
		CodexEventSessionStart, CodexEventUserPromptSubmit, CodexEventPreToolUse,
		CodexEventPermissionRequest, CodexEventPostToolUse, CodexEventStop,
	} {
		hooks[event] = m.mergeEventHandlers(hooks[event], event)
	}
	root["hooks"] = hooks
	root["description"] = "Reinframe project-local Codex hooks (" + CodexHooksProfileV1 + ")"
	root[CodexHookOwnerKey] = CodexHookOwnerValue
	root[CodexHookSchemaKey] = CodexHookSchemaVersion
	root[CodexHookProfileHashKey] = ProfileContentHash(m.BridgeCommand, CodexHooksProfileV1)
	if err := m.backupIfExists(); err != nil {
		return err
	}
	return m.writeAtomic(root, before)
}

// Uninstall removes only Reinframe-owned handlers and ownership markers.
func (m *CodexHooksManager) Uninstall() error {
	if err := m.validatePaths(); err != nil {
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
	hooks, _ := root["hooks"].(map[string]any)
	if hooks != nil {
		for _, event := range []string{
			CodexEventSessionStart, CodexEventUserPromptSubmit, CodexEventPreToolUse,
			CodexEventPermissionRequest, CodexEventPostToolUse, CodexEventStop,
		} {
			hooks[event] = filterOwnedMatcherGroups(hooks[event], true)
		}
		root["hooks"] = hooks
	}
	delete(root, CodexHookOwnerKey)
	delete(root, CodexHookSchemaKey)
	delete(root, CodexHookProfileHashKey)
	if err := m.backupIfExists(); err != nil {
		return err
	}
	return m.writeAtomic(root, before)
}

// Doctor validates owned handlers and trust hash freshness.
func (m *CodexHooksManager) Doctor() (CodexHooksDoctorResult, error) {
	res := CodexHooksDoctorResult{HooksPath: m.HooksPath}
	if err := m.validatePaths(); err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	before, err := m.readState()
	if err != nil {
		if os.IsNotExist(err) {
			res.Messages = append(res.Messages, "hooks.json missing")
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
	wantHash := ProfileContentHash(m.BridgeCommand, CodexHooksProfileV1)
	res.ProfileHash = wantHash
	if got, _ := root[CodexHookProfileHashKey].(string); got != "" && got != wantHash {
		res.TrustStale = true
		res.Messages = append(res.Messages, "stored profile hash stale — re-run install and re-trust via Codex /hooks")
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		res.Messages = append(res.Messages, "no hooks object")
		return res, nil
	}
	for _, event := range []string{CodexEventPreToolUse, CodexEventPermissionRequest} {
		arr, _ := hooks[event].([]any)
		for _, item := range arr {
			mg, ok := item.(map[string]any)
			if !ok || !isOwnedMatcherGroup(mg) {
				continue
			}
			res.OwnedHandlers++
			if err := validateOwnedMatcherGroup(mg, m.BridgeCommand); err != nil {
				res.Messages = append(res.Messages, event+": "+err.Error())
				continue
			}
			res.ValidOwned++
		}
	}
	if res.ValidOwned == 0 {
		res.Messages = append(res.Messages, "no valid reinframe-owned PreToolUse/PermissionRequest handlers")
		return res, nil
	}
	if res.TrustStale {
		return res, nil
	}
	res.OK = true
	res.Messages = append(res.Messages, fmt.Sprintf("valid owned handlers=%d profile=%s", res.ValidOwned, CodexHooksProfileV1))
	return res, nil
}

func (m *CodexHooksManager) mergeEventHandlers(existing any, event string) []any {
	// Keep non-owned matcher groups; replace owned ones with one canonical group.
	var kept []any
	if arr, ok := existing.([]any); ok {
		for _, item := range arr {
			mg, ok := item.(map[string]any)
			if !ok {
				kept = append(kept, item)
				continue
			}
			if isOwnedMatcherGroup(mg) {
				continue // drop owned
			}
			kept = append(kept, item)
		}
	}
	return append(kept, m.ownedMatcherGroup(event))
}

func (m *CodexHooksManager) ownedMatcherGroup(event string) map[string]any {
	matcher := ".*"
	switch event {
	case CodexEventPreToolUse, CodexEventPermissionRequest, CodexEventPostToolUse:
		// Match Bash, apply_patch, Edit, Write, and local MCP tools (not hosted WebSearch).
		matcher = "^(Bash|apply_patch|Edit|Write|mcp__.*|update_plan|Agent)$"
	case CodexEventSessionStart:
		matcher = "startup|resume|clear|compact"
	case CodexEventUserPromptSubmit, CodexEventStop:
		// matcher ignored by host for these events
		matcher = ""
	}
	handler := map[string]any{
		"type":                   "command",
		"command":                m.BridgeCommand,
		"timeout":                30,
		"statusMessage":          "Reinframe " + event,
		"additionalContextLimit": 2500,
		CodexHookOwnerKey:        CodexHookOwnerValue,
		CodexHookSchemaKey:       CodexHookSchemaVersion,
	}
	group := map[string]any{
		CodexHookOwnerKey:  CodexHookOwnerValue,
		CodexHookSchemaKey: CodexHookSchemaVersion,
		"hooks":            []any{handler},
	}
	if matcher != "" {
		group["matcher"] = matcher
	}
	return group
}

func filterOwnedMatcherGroups(existing any, dropOwned bool) []any {
	var out []any
	arr, ok := existing.([]any)
	if !ok {
		return out
	}
	for _, item := range arr {
		mg, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if dropOwned && isOwnedMatcherGroup(mg) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func isOwnedMatcherGroup(mg map[string]any) bool {
	if v, _ := mg[CodexHookOwnerKey].(string); v == CodexHookOwnerValue {
		return true
	}
	// Also detect owned handlers nested under hooks[].
	if hooks, ok := mg["hooks"].([]any); ok {
		for _, h := range hooks {
			hm, ok := h.(map[string]any)
			if !ok {
				continue
			}
			if v, _ := hm[CodexHookOwnerKey].(string); v == CodexHookOwnerValue {
				return true
			}
		}
	}
	return false
}

func validateOwnedMatcherGroup(mg map[string]any, wantCmd string) error {
	if v, _ := mg[CodexHookOwnerKey].(string); v != CodexHookOwnerValue {
		return fmt.Errorf("missing ownership marker")
	}
	if v, _ := mg[CodexHookSchemaKey].(string); v != CodexHookSchemaVersion {
		return fmt.Errorf("schema version mismatch")
	}
	hooks, ok := mg["hooks"].([]any)
	if !ok || len(hooks) == 0 {
		return fmt.Errorf("empty hooks array")
	}
	h, ok := hooks[0].(map[string]any)
	if !ok {
		return fmt.Errorf("handler not object")
	}
	if t, _ := h["type"].(string); t != "command" {
		return fmt.Errorf("type must be command")
	}
	cmd, _ := h["command"].(string)
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("command required")
	}
	if wantCmd != "" && cmd != wantCmd {
		return fmt.Errorf("command mismatch")
	}
	return nil
}

func (m *CodexHooksManager) validatePaths() error {
	if m.HooksPath == "" {
		return fmt.Errorf("codex hooks: HooksPath required")
	}
	if strings.TrimSpace(m.BridgeCommand) == "" {
		return fmt.Errorf("codex hooks: BridgeCommand required")
	}
	if err := rejectSymlinkPath(m.HooksPath); err != nil {
		return err
	}
	if m.ProjectRoot != "" {
		root, err := filepath.Abs(m.ProjectRoot)
		if err != nil {
			return err
		}
		hp, err := filepath.Abs(m.HooksPath)
		if err != nil {
			return err
		}
		// Require hooks under <root>/.codex/
		codexDir := filepath.Join(root, ".codex")
		rel, err := filepath.Rel(codexDir, hp)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("codex hooks: HooksPath must be under project .codex/ (cross-project rejected)")
		}
	}
	// Never allow writing to rollout/transcript paths.
	base := strings.ToLower(filepath.Base(m.HooksPath))
	if strings.Contains(base, "rollout") || strings.HasSuffix(base, ".jsonl") {
		return fmt.Errorf("codex hooks: refusing rollout/transcript path")
	}
	return nil
}

type fileState struct {
	bytes []byte
	mode  fs.FileMode
}

func (m *CodexHooksManager) readState() (fileState, error) {
	fi, err := os.Lstat(m.HooksPath)
	if err != nil {
		return fileState{}, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fileState{}, fmt.Errorf("codex hooks: path is symlink")
	}
	b, err := os.ReadFile(m.HooksPath)
	if err != nil {
		return fileState{}, err
	}
	return fileState{bytes: b, mode: fi.Mode()}, nil
}

func (m *CodexHooksManager) parseRoot(b []byte, allowEmpty bool) (map[string]any, error) {
	if len(b) == 0 {
		if allowEmpty {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("codex hooks: empty file")
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return nil, fmt.Errorf("codex hooks: parse: %w", err)
	}
	if root == nil {
		return map[string]any{}, nil
	}
	return root, nil
}

func (m *CodexHooksManager) backupIfExists() error {
	if _, err := os.Lstat(m.HooksPath); os.IsNotExist(err) {
		return nil
	}
	src, err := os.ReadFile(m.HooksPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(src)
	bak := m.HooksPath + ".bak." + hex.EncodeToString(sum[:4])
	return os.WriteFile(bak, src, 0o600)
}

func (m *CodexHooksManager) writeAtomic(root map[string]any, before fileState) error {
	// Concurrency: if file existed and content changed since read, fail closed.
	if len(before.bytes) > 0 {
		cur, err := os.ReadFile(m.HooksPath)
		if err == nil && string(cur) != string(before.bytes) {
			return fmt.Errorf("codex hooks: concurrent modification detected")
		}
	}
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(m.HooksPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".hooks-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.HooksPath)
}

func rejectSymlinkPath(p string) error {
	if p == "" {
		return nil
	}
	fi, err := os.Lstat(p)
	if err != nil {
		if os.IsNotExist(err) {
			dir := filepath.Dir(p)
			dfi, derr := os.Lstat(dir)
			if derr == nil && dfi.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("codex hooks: parent is symlink (fail closed)")
			}
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("codex hooks: path is symlink (fail closed)")
	}
	return nil
}
