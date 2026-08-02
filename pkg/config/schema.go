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

	// Secrets maps logical names to env placeholders only — never raw values.
	Secrets SecretsConfig `json:"secrets" yaml:"secrets"`

	// Workspace configures managed worktree isolation (ADR 004).
	Workspace WorkspaceConfig `json:"workspace" yaml:"workspace"`
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
	for name, ref := range c.Secrets.Refs {
		if err := validateEnvPlaceholder(fmt.Sprintf("secrets.refs[%s]", name), ref); err != nil {
			return err
		}
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
func IsEnvPlaceholder(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, EnvPlaceholderPrefix) || !strings.HasSuffix(s, EnvPlaceholderSuffix) {
		return false
	}
	inner := s[len(EnvPlaceholderPrefix) : len(s)-len(EnvPlaceholderSuffix)]
	if inner == "" {
		return false
	}
	// Disallow nested braces / whitespace-only names.
	if strings.ContainsAny(inner, "${}") {
		return false
	}
	return strings.TrimSpace(inner) == inner
}

func validateEnvPlaceholder(field, value string) error {
	if !IsEnvPlaceholder(value) {
		return fmt.Errorf("%s must be an env placeholder like ${VAR_NAME}, got %q", field, value)
	}
	return nil
}
