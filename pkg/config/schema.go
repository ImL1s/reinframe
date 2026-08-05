// Package config defines the versioned Reinframe configuration surface.
//
// #53 — Config schema with SchemaVersion, session defaults, store BusyTimeout,
// hook-gate FailOpen, secret refs as env placeholders, and YAML/JSON tags.
// Loaders for on-disk YAML may land later; this package owns the struct contract
// and round-trip marshal behavior.
package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// CurrentSchemaVersion is the schema version emitted by Default and expected
// by Validate for this package revision.
const CurrentSchemaVersion = 1

// EnvPlaceholderPrefix / EnvPlaceholderSuffix mark secret references that must
// be resolved from the process environment (e.g. "${OPENAI_API_KEY}").
// Raw secret values must never appear in config files (ADR 003).
const (
	EnvPlaceholderPrefix = "${"
	EnvPlaceholderSuffix = "}"
)

// Config is the versioned top-level Reinframe configuration document.
type Config struct {
	// SchemaVersion identifies the document shape. Bump when fields are
	// removed or change meaning incompatibly.
	SchemaVersion int `json:"schema_version" yaml:"schema_version"`

	// Session holds defaults applied when a supervised session starts.
	Session SessionDefaults `json:"session" yaml:"session"`

	// Store configures the SQLite WAL event store.
	Store StoreConfig `json:"store" yaml:"store"`

	// HookGate configures the synchronous PreTool/PreCommand fast path (#67).
	HookGate HookGateConfig `json:"hook_gate" yaml:"hook_gate"`

	// Reviewer configures ReviewerProvider selection and egress posture (ADR 003).
	Reviewer ReviewerConfig `json:"reviewer" yaml:"reviewer"`

	// ClassifierProvider configures the Stage-1 classifier provider runtime (#132).
	// Separate from Reviewer — never silently reuses reviewer advice path.
	// Empty/disabled means shadow/tests use FakeClassifierProvider only (no network).
	ClassifierProvider ClassifierProviderConfig `json:"classifier_provider,omitempty" yaml:"classifier_provider,omitempty"`

	// Secrets maps logical names to env placeholders only — never raw values.
	Secrets SecretsConfig `json:"secrets" yaml:"secrets"`

	// Workspace configures managed worktree isolation (ADR 004).
	Workspace WorkspaceConfig `json:"workspace" yaml:"workspace"`
}

// ClassifierProviderConfig selects the optional real classifier provider (#132).
// kind empty or "none" disables network providers (default).
type ClassifierProviderConfig struct {
	// Kind: ""|"none"|"openai_compatible".
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Model identifier (required when kind is openai_compatible).
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// BaseURL for OpenAI-compatible endpoint.
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	// Path defaults to /v1/chat/completions.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// APIKeyRef must be ${ENV} placeholder — never a raw secret.
	APIKeyRef string `json:"api_key_ref,omitempty" yaml:"api_key_ref,omitempty"`
	// TimeoutMS per-call timeout (default 1500).
	TimeoutMS int `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	// MaxInputBytes / MaxOutputBytes bounds.
	MaxInputBytes  int `json:"max_input_bytes,omitempty" yaml:"max_input_bytes,omitempty"`
	MaxOutputBytes int `json:"max_output_bytes,omitempty" yaml:"max_output_bytes,omitempty"`
	// CapabilitiesProfile defaults to generic-none-v1.
	CapabilitiesProfile string `json:"capabilities_profile,omitempty" yaml:"capabilities_profile,omitempty"`
}

// SessionDefaults are applied to new supervised sessions unless overridden.
type SessionDefaults struct {
	// MaxDuration is the default wall-clock session budget (Go duration, e.g. "2h").
	// Empty means no default wall limit from config.
	MaxDuration string `json:"max_duration,omitempty" yaml:"max_duration,omitempty"`

	// DefaultLevel is the Axis A integration level (0=Observe … 3=Full-control).
	DefaultLevel int `json:"default_level" yaml:"default_level"`

	// LocalOnlyReviewer, when true, forces local/offline reviewer path and
	// blocks cloud egress even if Reviewer.Mode names a remote provider.
	// Default true per ADR 003.
	LocalOnlyReviewer bool `json:"local_only_reviewer" yaml:"local_only_reviewer"`
}

// StoreConfig configures pkg/state StoreOptions.
type StoreConfig struct {
	// DatabasePath is the SQLite file path. Empty or ":memory:" selects in-memory.
	DatabasePath string `json:"database_path,omitempty" yaml:"database_path,omitempty"`

	// BusyTimeout is the SQLite busy_timeout as a Go duration string (e.g. "5s").
	BusyTimeout string `json:"busy_timeout" yaml:"busy_timeout"`
}

// HookGateConfig configures deterministic hook evaluation.
type HookGateConfig struct {
	// FailOpen controls timeout / cancel behavior:
	//   true  → allow (fail-open)
	//   false → deny  (fail-closed)
	FailOpen bool `json:"fail_open" yaml:"fail_open"`

	// Timeout is the per-call wall budget (Go duration, e.g. "50ms").
	// Empty uses the adapter package default at evaluation time.
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// ReviewerConfig selects the reviewer implementation and remote endpoint metadata.
type ReviewerConfig struct {
	// Mode is one of: "local", "openai_compatible", "cloud".
	// Default "local" (ADR 003 local-only posture).
	Mode string `json:"mode" yaml:"mode"`

	// Model is the default model identifier for the selected provider.
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// BaseURL is an optional OpenAI-compatible base URL (remote modes only).
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`

	// APIKeyRef is an env placeholder for the provider API key
	// (e.g. "${REINFRAME_REVIEWER_API_KEY}"). Must not be a raw secret.
	APIKeyRef string `json:"api_key_ref,omitempty" yaml:"api_key_ref,omitempty"`
}

// SecretsConfig holds named secret references as environment placeholders.
type SecretsConfig struct {
	// Refs maps logical secret names to env placeholders.
	// Example: "reviewer_api_key": "${REINFRAME_REVIEWER_API_KEY}"
	Refs map[string]string `json:"refs,omitempty" yaml:"refs,omitempty"`
}

// WorkspaceConfig configures supervisor-owned worktree isolation (ADR 004).
type WorkspaceConfig struct {
	// ManagedWorktreeRoot is the supervisor-owned worktree (or project) path.
	// Empty means "bind at session start".
	ManagedWorktreeRoot string `json:"managed_worktree_root,omitempty" yaml:"managed_worktree_root,omitempty"`

	// EnforceIsolation, when true, requires agent writes to stay inside the
	// managed root (hook gate / path policy).
	EnforceIsolation bool `json:"enforce_isolation" yaml:"enforce_isolation"`
}

// Default returns a Config with safe foundation defaults (ADR 003 / ADR 004).
func Default() Config {
	return Config{
		SchemaVersion: CurrentSchemaVersion,
		Session: SessionDefaults{
			MaxDuration:       "",
			DefaultLevel:      0,
			LocalOnlyReviewer: true,
		},
		Store: StoreConfig{
			DatabasePath: "",
			BusyTimeout:  "5s",
		},
		HookGate: HookGateConfig{
			FailOpen: true,
			Timeout:  "50ms",
		},
		Reviewer: ReviewerConfig{
			Mode: "local",
		},
		Secrets: SecretsConfig{
			Refs: map[string]string{},
		},
		Workspace: WorkspaceConfig{
			EnforceIsolation: true,
		},
	}
}

// Validate checks schema version, duration fields, reviewer mode, and that
// secret-like fields use env placeholders rather than raw values.
func (c Config) Validate() error {
	if c.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %d (want %d)", c.SchemaVersion, CurrentSchemaVersion)
	}
	if c.Session.DefaultLevel < 0 || c.Session.DefaultLevel > 3 {
		return fmt.Errorf("session.default_level must be 0..3, got %d", c.Session.DefaultLevel)
	}
	if err := validateOptionalDuration("session.max_duration", c.Session.MaxDuration); err != nil {
		return err
	}
	if err := validateRequiredDuration("store.busy_timeout", c.Store.BusyTimeout); err != nil {
		return err
	}
	if err := validateOptionalDuration("hook_gate.timeout", c.HookGate.Timeout); err != nil {
		return err
	}
	switch c.Reviewer.Mode {
	case "local", "openai_compatible", "cloud":
		// ok
	case "":
		return fmt.Errorf("reviewer.mode is required")
	default:
		return fmt.Errorf("reviewer.mode %q is not supported", c.Reviewer.Mode)
	}
	if c.Reviewer.APIKeyRef != "" {
		if err := validateEnvPlaceholder("reviewer.api_key_ref", c.Reviewer.APIKeyRef); err != nil {
			return err
		}
	}
	if err := validateClassifierProvider(c.ClassifierProvider); err != nil {
		return err
	}
	for name, ref := range c.Secrets.Refs {
		if err := validateEnvPlaceholder(fmt.Sprintf("secrets.refs[%s]", name), ref); err != nil {
			return err
		}
	}
	return nil
}

func validateClassifierProvider(cp ClassifierProviderConfig) error {
	kind := strings.TrimSpace(cp.Kind)
	// Secret-like fields always validated with redacted errors (even when disabled).
	if cp.APIKeyRef != "" {
		if err := validateEnvPlaceholder("classifier_provider.api_key_ref", cp.APIKeyRef); err != nil {
			// Never echo the raw value.
			return fmt.Errorf("classifier_provider.api_key_ref is invalid")
		}
	}
	switch kind {
	case "", "none":
		// Disabled: every other field must be empty/zero.
		if strings.TrimSpace(cp.Model) != "" || strings.TrimSpace(cp.BaseURL) != "" ||
			strings.TrimSpace(cp.Path) != "" || strings.TrimSpace(cp.APIKeyRef) != "" ||
			cp.TimeoutMS != 0 || cp.MaxInputBytes != 0 || cp.MaxOutputBytes != 0 ||
			strings.TrimSpace(cp.CapabilitiesProfile) != "" {
			return fmt.Errorf("classifier_provider: disabled kind requires empty fields")
		}
		return nil
	case "openai_compatible":
		// ok
	default:
		return fmt.Errorf("classifier_provider.kind is not supported")
	}
	if strings.TrimSpace(cp.Model) == "" {
		return fmt.Errorf("classifier_provider.model is required")
	}
	base := strings.TrimSpace(cp.BaseURL)
	if base == "" {
		return fmt.Errorf("classifier_provider.base_url is required")
	}
	// Origin-only contract: scheme+host, no path/query/fragment/userinfo.
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("classifier_provider.base_url is invalid")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("classifier_provider.base_url scheme must be http or https")
	}
	if u.User != nil {
		return fmt.Errorf("classifier_provider.base_url must not include userinfo")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("classifier_provider.base_url must not include query or fragment")
	}
	p := u.EscapedPath()
	if p != "" && p != "/" {
		return fmt.Errorf("classifier_provider.base_url must be origin only")
	}
	if path := strings.TrimSpace(cp.Path); path != "" {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("classifier_provider.path must begin with /")
		}
		if strings.Contains(path, "://") || strings.ContainsAny(path, "?#") {
			return fmt.Errorf("classifier_provider.path is invalid")
		}
		for _, seg := range strings.Split(path, "/") {
			if seg == ".." {
				return fmt.Errorf("classifier_provider.path traversal rejected")
			}
		}
	}
	if cp.TimeoutMS < 0 || cp.TimeoutMS > 60000 {
		return fmt.Errorf("classifier_provider.timeout_ms out of range")
	}
	if cp.MaxInputBytes < 0 || cp.MaxInputBytes > 1<<20 {
		return fmt.Errorf("classifier_provider.max_input_bytes out of range")
	}
	if cp.MaxOutputBytes < 0 || cp.MaxOutputBytes > 256<<10 {
		return fmt.Errorf("classifier_provider.max_output_bytes out of range")
	}
	switch strings.TrimSpace(cp.CapabilitiesProfile) {
	case "", "generic-none-v1":
	default:
		return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
	}
	return nil
}

// BusyTimeoutDuration parses Store.BusyTimeout.
func (c Config) BusyTimeoutDuration() (time.Duration, error) {
	return time.ParseDuration(c.Store.BusyTimeout)
}

// MarshalJSONDocument encodes Config as pretty JSON (stable for fixtures).
func MarshalJSONDocument(c Config) ([]byte, error) {
	return json.MarshalIndent(c, "", "  ")
}

// UnmarshalJSONDocument decodes Config from JSON bytes.
func UnmarshalJSONDocument(data []byte) (Config, error) {
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func validateOptionalDuration(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
	}
	return nil
}

func validateRequiredDuration(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if _, err := time.ParseDuration(value); err != nil {
		return fmt.Errorf("%s: invalid duration %q: %w", field, value, err)
	}
	return nil
}

// IsEnvPlaceholder reports whether s is a ${NAME} style reference.
// NAME must match [A-Za-z_][A-Za-z0-9_]* (no spaces, dashes, dots, or leading digits).
func IsEnvPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, EnvPlaceholderPrefix) || !strings.HasSuffix(s, EnvPlaceholderSuffix) {
		return false
	}
	inner := s[len(EnvPlaceholderPrefix) : len(s)-len(EnvPlaceholderSuffix)]
	if inner == "" {
		return false
	}
	for i, r := range inner {
		if i == 0 {
			if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_') {
				return false
			}
			continue
		}
		if !((r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_') {
			return false
		}
	}
	return true
}

func validateEnvPlaceholder(field, value string) error {
	if !IsEnvPlaceholder(value) {
		// Never echo the supplied value (may be a raw secret).
		return fmt.Errorf("%s must be an env placeholder like ${VAR_NAME}", field)
	}
	return nil
}
