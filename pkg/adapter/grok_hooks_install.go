package adapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GrokHooksManager installs project-local Grok Build hooks under .grok/hooks/ (#165).
// Never writes ~/.grok/auth.json or session history.
type GrokHooksManager struct {
	// HooksFile is e.g. <project>/.grok/hooks/reinframe-pretool.json
	HooksFile string
	// BridgeCommand is the exact command for PreToolUse.
	BridgeCommand string
	// ProjectRoot when set requires HooksFile under <root>/.grok/hooks/
	ProjectRoot string
	// OptionalPermissionsPath when set installs Reinframe-owned static deny rules.
	OptionalPermissionsPath string
}

// GrokHooksInstallPlan is dry-run install output.
type GrokHooksInstallPlan struct {
	HooksFile      string   `json:"hooks_file"`
	WouldCreate    bool     `json:"would_create"`
	Actions        []string `json:"actions"`
	Profile        string   `json:"profile"`
	ProfileHash    string   `json:"profile_hash"`
	TrustHint      string   `json:"trust_hint"`
	PreserveOthers bool     `json:"preserve_others"`
}

// GrokHooksDoctorResult is doctor output.
type GrokHooksDoctorResult struct {
	HooksFile   string   `json:"hooks_file"`
	Exists      bool     `json:"exists"`
	OK          bool     `json:"ok"`
	Messages    []string `json:"messages"`
	ProfileHash string   `json:"profile_hash,omitempty"`
	TrustStale  bool     `json:"trust_stale,omitempty"`
}

// PlanInstall dry-runs install.
func (m *GrokHooksManager) PlanInstall() (GrokHooksInstallPlan, error) {
	if err := m.validate(); err != nil {
		return GrokHooksInstallPlan{}, err
	}
	plan := GrokHooksInstallPlan{
		HooksFile:      m.HooksFile,
		Profile:        GrokHooksProfileV1,
		ProfileHash:    GrokProfileContentHash(m.BridgeCommand, GrokHooksProfileV1),
		PreserveOthers: true,
		TrustHint: fmt.Sprintf(
			"Project hooks require folder trust: /hooks-trust or --trust. command=%q hash=%s",
			m.BridgeCommand, GrokProfileContentHash(m.BridgeCommand, GrokHooksProfileV1),
		),
	}
	if _, err := os.Lstat(m.HooksFile); os.IsNotExist(err) {
		plan.WouldCreate = true
		plan.Actions = append(plan.Actions, "create reinframe-owned hook JSON")
	} else if err != nil {
		return plan, err
	} else {
		plan.Actions = append(plan.Actions, "overwrite reinframe-owned hook JSON only")
	}
	plan.Actions = append(plan.Actions, "install PreToolUse/SessionStart/PostToolUse/Stop handlers")
	if m.OptionalPermissionsPath != "" {
		plan.Actions = append(plan.Actions, "optional Reinframe-owned static permission deny fragment")
	}
	return plan, nil
}

// Install writes Reinframe-owned hook file atomically.
func (m *GrokHooksManager) Install() error {
	if _, err := m.PlanInstall(); err != nil {
		return err
	}
	root := m.ownedHooksDocument()
	return m.writeAtomicJSON(m.HooksFile, root)
}

// Uninstall removes Reinframe-owned hook file only (not unrelated .grok/hooks/*.json).
func (m *GrokHooksManager) Uninstall() error {
	if err := m.validate(); err != nil {
		return err
	}
	// Only delete if ownership markers present.
	b, err := os.ReadFile(m.HooksFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		return fmt.Errorf("grok hooks: refuse uninstall of non-JSON file")
	}
	if v, _ := root[GrokHookOwnerKey].(string); v != GrokHookOwnerValue {
		return fmt.Errorf("grok hooks: file not reinframe-owned; refuse uninstall")
	}
	return os.Remove(m.HooksFile)
}

// Doctor validates owned file and profile hash.
func (m *GrokHooksManager) Doctor() (GrokHooksDoctorResult, error) {
	res := GrokHooksDoctorResult{HooksFile: m.HooksFile}
	if err := m.validate(); err != nil {
		res.Messages = append(res.Messages, err.Error())
		return res, nil
	}
	want := GrokProfileContentHash(m.BridgeCommand, GrokHooksProfileV1)
	res.ProfileHash = want
	b, err := os.ReadFile(m.HooksFile)
	if err != nil {
		if os.IsNotExist(err) {
			res.Messages = append(res.Messages, "hooks file missing")
			return res, nil
		}
		return res, err
	}
	res.Exists = true
	var root map[string]any
	if err := json.Unmarshal(b, &root); err != nil {
		res.Messages = append(res.Messages, "invalid json")
		return res, nil
	}
	if v, _ := root[GrokHookOwnerKey].(string); v != GrokHookOwnerValue {
		res.Messages = append(res.Messages, "not reinframe-owned")
		return res, nil
	}
	if got, _ := root[GrokHookProfileHashKey].(string); got != "" && got != want {
		res.TrustStale = true
		res.Messages = append(res.Messages, "profile hash stale — reinstall and re-trust project")
		return res, nil
	}
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		res.Messages = append(res.Messages, "no hooks object")
		return res, nil
	}
	if _, ok := hooks[GrokEventPreToolUse]; !ok {
		res.Messages = append(res.Messages, "missing PreToolUse")
		return res, nil
	}
	res.OK = true
	res.Messages = append(res.Messages, "valid reinframe grok hooks foundation")
	return res, nil
}

func (m *GrokHooksManager) ownedHooksDocument() map[string]any {
	handler := map[string]any{
		"type":            "command",
		"command":         m.BridgeCommand,
		"timeout":         10,
		GrokHookOwnerKey:  GrokHookOwnerValue,
		GrokHookSchemaKey: GrokHookSchemaVersion,
	}
	group := func(matcher string) map[string]any {
		g := map[string]any{
			"hooks":           []any{handler},
			GrokHookOwnerKey:  GrokHookOwnerValue,
			GrokHookSchemaKey: GrokHookSchemaVersion,
		}
		if matcher != "" {
			g["matcher"] = matcher
		}
		return g
	}
	return map[string]any{
		GrokHookOwnerKey:       GrokHookOwnerValue,
		GrokHookSchemaKey:      GrokHookSchemaVersion,
		GrokHookProfileHashKey: GrokProfileContentHash(m.BridgeCommand, GrokHooksProfileV1),
		"description":          "Reinframe Grok Build native hooks (" + GrokHooksProfileV1 + ")",
		"hooks": map[string]any{
			GrokEventSessionStart:     []any{group("")},
			GrokEventUserPromptSubmit: []any{group("")},
			GrokEventPreToolUse:       []any{group("Bash|run_terminal_command|Edit|Write|search_replace|Read|read_file|mcp__.*|.*")},
			GrokEventPostToolUse:      []any{group("")},
			GrokEventStop:             []any{group("")},
		},
	}
}

// InstallStaticDenyFragment writes an optional ownership-scoped permission snippet.
// Does not modify user permission rules outside this file.
func (m *GrokHooksManager) InstallStaticDenyFragment(deniedTools []string) error {
	if m.OptionalPermissionsPath == "" {
		return fmt.Errorf("grok hooks: OptionalPermissionsPath required")
	}
	if err := rejectSymlinkPath(m.OptionalPermissionsPath); err != nil {
		return err
	}
	if m.ProjectRoot != "" {
		root, _ := filepath.Abs(m.ProjectRoot)
		p, _ := filepath.Abs(m.OptionalPermissionsPath)
		rel, err := filepath.Rel(filepath.Join(root, ".grok"), p)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("grok hooks: permissions path must be under project .grok/")
		}
	}
	doc := map[string]any{
		GrokHookOwnerKey:  GrokHookOwnerValue,
		GrokHookSchemaKey: "reinframe.grok_static_deny.v1",
		"note":            "optional Reinframe-owned static deny fragment; host fail-open still applies to hooks",
		"deny_tools":      deniedTools,
	}
	return m.writeAtomicJSON(m.OptionalPermissionsPath, doc)
}

func (m *GrokHooksManager) validate() error {
	if m.HooksFile == "" {
		return fmt.Errorf("grok hooks: HooksFile required")
	}
	if strings.TrimSpace(m.BridgeCommand) == "" {
		return fmt.Errorf("grok hooks: BridgeCommand required")
	}
	// Never auth/session history paths.
	low := strings.ToLower(m.HooksFile)
	if strings.Contains(low, "auth.json") || strings.Contains(low, "session") && strings.HasSuffix(low, ".jsonl") {
		return fmt.Errorf("grok hooks: refuse auth/session-history path")
	}
	if err := rejectSymlinkPath(m.HooksFile); err != nil {
		return err
	}
	if m.ProjectRoot != "" {
		root, err := filepath.Abs(m.ProjectRoot)
		if err != nil {
			return err
		}
		hf, err := filepath.Abs(m.HooksFile)
		if err != nil {
			return err
		}
		hooksDir := filepath.Join(root, ".grok", "hooks")
		rel, err := filepath.Rel(hooksDir, hf)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("grok hooks: HooksFile must be under project .grok/hooks/")
		}
	}
	return nil
}

func (m *GrokHooksManager) writeAtomicJSON(path string, root map[string]any) error {
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := rejectSymlinkPath(dir); err != nil {
		return err
	}
	// Backup existing
	if b, err := os.ReadFile(path); err == nil {
		sum := sha256.Sum256(b)
		_ = os.WriteFile(path+".bak."+hex.EncodeToString(sum[:4]), b, 0o600)
	}
	tmp, err := os.CreateTemp(dir, ".grok-hooks-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	_ = tmp.Chmod(0o600)
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
