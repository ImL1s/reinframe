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
	"strconv"
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

	// ClassifierCache configures process-local exact RawAssessment cache (#138).
	// Default disabled. Does not cache Stage-2 ResolvedDecision.
	ClassifierCache ClassifierCacheConfig `json:"classifier_cache,omitempty" yaml:"classifier_cache,omitempty"`

	// Secrets maps logical names to env placeholders only — never raw values.
	Secrets SecretsConfig `json:"secrets" yaml:"secrets"`

	// Workspace configures managed worktree isolation (ADR 004).
	Workspace WorkspaceConfig `json:"workspace" yaml:"workspace"`

	// CodexRuntime configures the delegated Codex runtime boundary (#183).
	// Disabled by default; delegates credential ownership to the child codex process.
	CodexRuntime CodexRuntimeConfig `json:"codex_runtime,omitempty" yaml:"codex_runtime,omitempty"`
}

// ClassifierCacheConfig is the process-local exact assessment cache (#138).
type ClassifierCacheConfig struct {
	Exact ExactCacheYAML `json:"exact,omitempty" yaml:"exact,omitempty"`
	// Singleflight coalesces concurrent identical keys (default true when exact.enabled).
	Singleflight *bool `json:"singleflight,omitempty" yaml:"singleflight,omitempty"`
}

// ExactCacheYAML is the YAML surface for exact cache bounds.
type ExactCacheYAML struct {
	Enabled    bool   `json:"enabled,omitempty" yaml:"enabled,omitempty"`
	MaxEntries int    `json:"max_entries,omitempty" yaml:"max_entries,omitempty"`
	MaxBytes   int    `json:"max_bytes,omitempty" yaml:"max_bytes,omitempty"`
	TTL        string `json:"ttl,omitempty" yaml:"ttl,omitempty"` // Go duration, e.g. "10m"
}

// ClassifierProviderConfig selects the optional real classifier provider (#132).
// kind empty or "none" disables network providers (default).
type ClassifierProviderConfig struct {
	// Kind: ""|"none"|"openai_compatible"|"openai_responses"|"anthropic_messages"|"gemini_generate_content"|"xai_responses".
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Model identifier (required when kind is a network provider).
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// BaseURL for OpenAI-compatible endpoint (origin only).
	BaseURL string `json:"base_url,omitempty" yaml:"base_url,omitempty"`
	// Path defaults to /v1/chat/completions.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// APIKeyRef must be ${ENV} placeholder — never a raw secret.
	APIKeyRef string `json:"api_key_ref,omitempty" yaml:"api_key_ref,omitempty"`
	// Platform pins Anthropic direct Claude API vs hosted variants (#135).
	// Empty defaults to claude_api for anthropic_messages; unsupported platforms fail closed.
	Platform string `json:"platform,omitempty" yaml:"platform,omitempty"`
	// TimeoutMS per-call timeout (default 1500).
	TimeoutMS int `json:"timeout_ms,omitempty" yaml:"timeout_ms,omitempty"`
	// MaxInputBytes / MaxOutputBytes bounds.
	MaxInputBytes  int `json:"max_input_bytes,omitempty" yaml:"max_input_bytes,omitempty"`
	MaxOutputBytes int `json:"max_output_bytes,omitempty" yaml:"max_output_bytes,omitempty"`
	// CapabilitiesProfile defaults to generic-none-v1.
	CapabilitiesProfile string `json:"capabilities_profile,omitempty" yaml:"capabilities_profile,omitempty"`
	// EgressProfile is an optional secret-free partition for native cache keys (#134/#135).
	EgressProfile string `json:"egress_profile,omitempty" yaml:"egress_profile,omitempty"`
}

// NormalizeKind returns the closed, trimmed kind used by validation and factory.
func (cp ClassifierProviderConfig) NormalizeKind() string {
	k := strings.TrimSpace(strings.ToLower(cp.Kind))
	switch k {
	case "", "none":
		return "none"
	case "openai_compatible":
		return "openai_compatible"
	case "openai_responses":
		return "openai_responses"
	case "anthropic_messages":
		return "anthropic_messages"
	case "gemini_generate_content":
		return "gemini_generate_content"
	case "xai_responses":
		return "xai_responses"
	default:
		return k
	}
}

// redactedAPIKeyRef returns a diagnostics-safe api_key_ref (env placeholders kept).
func (cp ClassifierProviderConfig) redactedAPIKeyRef() string {
	if cp.APIKeyRef == "" {
		return ""
	}
	if IsEnvPlaceholder(cp.APIKeyRef) {
		return cp.APIKeyRef
	}
	return "[REDACTED]"
}

// MarshalJSON redacts secret-like api_key_ref values that are not env placeholders,
// so diagnostics remain secret-safe even before Validate succeeds.
func (cp ClassifierProviderConfig) MarshalJSON() ([]byte, error) {
	type alias ClassifierProviderConfig
	out := alias(cp)
	out.APIKeyRef = cp.redactedAPIKeyRef()
	return json.Marshal(out)
}

// String is secret-safe for fmt / logs (never prints raw api_key_ref secrets).
func (cp ClassifierProviderConfig) String() string {
	return fmt.Sprintf(
		"ClassifierProviderConfig{kind:%q model:%q base_url:%q path:%q api_key_ref:%q timeout_ms:%d max_input_bytes:%d max_output_bytes:%d capabilities_profile:%q}",
		cp.Kind, cp.Model, cp.BaseURL, cp.Path, cp.redactedAPIKeyRef(),
		cp.TimeoutMS, cp.MaxInputBytes, cp.MaxOutputBytes, cp.CapabilitiesProfile,
	)
}

// GoString is secret-safe for %#v diagnostics.
func (cp ClassifierProviderConfig) GoString() string {
	return cp.String()
}

// Format implements fmt.Formatter so %v / %+v / %#v never leak raw secrets.
func (cp ClassifierProviderConfig) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v', 's':
		if f.Flag('#') {
			_, _ = fmt.Fprint(f, cp.GoString())
			return
		}
		_, _ = fmt.Fprint(f, cp.String())
	default:
		_, _ = fmt.Fprint(f, cp.String())
	}
}

// YAML note: structural `yaml:` tags remain for a future secure loader, but this
// package does not ship a YAML marshaler (no gopkg.in/yaml dependency). Do not
// claim secret-safe YAML dumps until a redacting MarshalYAML + loader lands.
// Secret-safe diagnostics for this type are JSON + fmt (String/GoString/Format).

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

// CodexRuntimeConfig configures the delegated Codex runtime boundary (#183).
// Reinframe strictly delegates credential ownership to the child codex process.
// Reinframe never opens ~/.codex/auth.json, never accepts raw API keys/tokens for this runtime,
// and enforces ChatGPT subscription as the default authentication contract.
type CodexRuntimeConfig struct {
	// Enabled controls whether the delegated Codex runtime is active (default false).
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Executable is the binary name or path for the codex CLI (default "codex").
	// Untrusted project configurations MUST NOT override this field.
	Executable string `json:"executable,omitempty" yaml:"executable,omitempty"`

	// CredentialOwner specifies who owns credentials: "codex_process" (default) or "reinframe_env".
	// Untrusted project configurations MUST NOT override this field.
	CredentialOwner string `json:"credential_owner,omitempty" yaml:"credential_owner,omitempty"`

	// RequiredAuth specifies the mandatory auth mode: "chatgpt_subscription" (default), "api_key", or "unknown".
	RequiredAuth string `json:"required_auth,omitempty" yaml:"required_auth,omitempty"`

	// AllowInteractiveLogin allows interactive operator login prompt when unauthenticated (default false).
	AllowInteractiveLogin bool `json:"allow_interactive_login" yaml:"allow_interactive_login"`

	// RuntimeProfile is an optional partition/profile identifier (default "default").
	RuntimeProfile string `json:"runtime_profile,omitempty" yaml:"runtime_profile,omitempty"`

	// BinarySHA256 is an optional SHA-256 hex digest for binary integrity verification.
	BinarySHA256 string `json:"binary_sha256,omitempty" yaml:"binary_sha256,omitempty"`

	// StatusCheckTimeoutMS bounds status probe execution (default 3000ms).
	StatusCheckTimeoutMS int `json:"status_check_timeout_ms,omitempty" yaml:"status_check_timeout_ms,omitempty"`
}

// NormalizeCredentialOwner returns the trimmed credential owner, defaulting to "codex_process".
func (cr CodexRuntimeConfig) NormalizeCredentialOwner() string {
	co := strings.TrimSpace(strings.ToLower(cr.CredentialOwner))
	if co == "" {
		return "codex_process"
	}
	return co
}

// NormalizeRequiredAuth returns the trimmed required auth mode, defaulting to "chatgpt_subscription".
func (cr CodexRuntimeConfig) NormalizeRequiredAuth() string {
	ra := strings.TrimSpace(strings.ToLower(cr.RequiredAuth))
	if ra == "" {
		return "chatgpt_subscription"
	}
	return ra
}

// NormalizeExecutable returns the trimmed executable name/path, defaulting to "codex".
func (cr CodexRuntimeConfig) NormalizeExecutable() string {
	ex := strings.TrimSpace(cr.Executable)
	if ex == "" {
		return "codex"
	}
	return ex
}

// NormalizeProfile returns the trimmed runtime profile, defaulting to "default".
func (cr CodexRuntimeConfig) NormalizeProfile() string {
	p := strings.TrimSpace(cr.RuntimeProfile)
	if p == "" {
		return "default"
	}
	return p
}

// String provides secret-safe diagnostics formatting.
func (cr CodexRuntimeConfig) String() string {
	return fmt.Sprintf(
		"CodexRuntimeConfig{enabled:%t executable:%q credential_owner:%q required_auth:%q allow_interactive:%t profile:%q sha256:%q timeout_ms:%d}",
		cr.Enabled, cr.NormalizeExecutable(), cr.NormalizeCredentialOwner(), cr.NormalizeRequiredAuth(),
		cr.AllowInteractiveLogin, cr.NormalizeProfile(), cr.BinarySHA256, cr.StatusCheckTimeoutMS,
	)
}

// GoString implements fmt.GoStringer.
func (cr CodexRuntimeConfig) GoString() string {
	return cr.String()
}

// Format implements fmt.Formatter to prevent accidental secret leakage.
func (cr CodexRuntimeConfig) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v', 's':
		if f.Flag('#') {
			_, _ = fmt.Fprint(f, cr.GoString())
			return
		}
		_, _ = fmt.Fprint(f, cr.String())
	default:
		_, _ = fmt.Fprint(f, cr.String())
	}
}

// Default returns a Config with safe foundation defaults (ADR 003 / ADR 004 / #183).
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
		CodexRuntime: CodexRuntimeConfig{
			Enabled:               false,
			Executable:            "codex",
			CredentialOwner:       "codex_process",
			RequiredAuth:          "chatgpt_subscription",
			AllowInteractiveLogin: false,
			RuntimeProfile:        "default",
			StatusCheckTimeoutMS:  3000,
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
	if err := validateClassifierCache(c.ClassifierCache); err != nil {
		return err
	}
	if err := validateCodexRuntime(c.CodexRuntime); err != nil {
		return err
	}
	for name, ref := range c.Secrets.Refs {
		if err := validateEnvPlaceholder(fmt.Sprintf("secrets.refs[%s]", name), ref); err != nil {
			return err
		}
	}
	return nil
}

func validateClassifierCache(cc ClassifierCacheConfig) error {
	ex := cc.Exact
	if !ex.Enabled {
		if ex.MaxEntries != 0 || ex.MaxBytes != 0 || strings.TrimSpace(ex.TTL) != "" {
			return fmt.Errorf("classifier_cache.exact: disabled requires empty bounds")
		}
		return nil
	}
	if ex.MaxEntries < 1 || ex.MaxEntries > 1_000_000 {
		return fmt.Errorf("classifier_cache.exact.max_entries out of range")
	}
	if ex.MaxBytes < 1024 || ex.MaxBytes > 256<<20 {
		return fmt.Errorf("classifier_cache.exact.max_bytes out of range")
	}
	ttl := strings.TrimSpace(ex.TTL)
	if ttl == "" {
		return fmt.Errorf("classifier_cache.exact.ttl is required when enabled")
	}
	d, err := time.ParseDuration(ttl)
	if err != nil || d < time.Second || d > 24*time.Hour {
		return fmt.Errorf("classifier_cache.exact.ttl out of range")
	}
	return nil
}

// ExactEnabled reports whether exact cache is on.
func (cc ClassifierCacheConfig) ExactEnabled() bool { return cc.Exact.Enabled }

// SingleflightEnabled defaults true when exact is enabled.
func (cc ClassifierCacheConfig) SingleflightEnabled() bool {
	if cc.Singleflight == nil {
		return cc.Exact.Enabled
	}
	return *cc.Singleflight
}

// ExactCacheBounds returns normalized bounds for classifier.ExactCacheConfig construction.
// ttl is 0 when disabled. Import cycle: callers map into classifier.ExactCacheConfig.
func (cc ClassifierCacheConfig) ExactCacheBounds() (enabled bool, maxEntries, maxBytes int, ttl time.Duration, singleflight bool, err error) {
	if !cc.Exact.Enabled {
		return false, 0, 0, 0, false, nil
	}
	maxEntries = cc.Exact.MaxEntries
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	maxBytes = cc.Exact.MaxBytes
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	ttlStr := strings.TrimSpace(cc.Exact.TTL)
	if ttlStr == "" {
		ttl = 10 * time.Minute
	} else {
		ttl, err = time.ParseDuration(ttlStr)
		if err != nil {
			return false, 0, 0, 0, false, fmt.Errorf("classifier_cache.exact.ttl: %w", err)
		}
	}
	return true, maxEntries, maxBytes, ttl, cc.SingleflightEnabled(), nil
}

func validateClassifierProvider(cp ClassifierProviderConfig) error {
	kind := cp.NormalizeKind()
	// Secret-like fields always validated with redacted errors (even when disabled).
	if cp.APIKeyRef != "" {
		if err := validateEnvPlaceholder("classifier_provider.api_key_ref", cp.APIKeyRef); err != nil {
			// Never echo the raw value.
			return fmt.Errorf("classifier_provider.api_key_ref is invalid")
		}
	}
	switch kind {
	case "none":
		// Disabled: every other field must be empty/zero.
		if strings.TrimSpace(cp.Model) != "" || strings.TrimSpace(cp.BaseURL) != "" ||
			strings.TrimSpace(cp.Path) != "" || strings.TrimSpace(cp.APIKeyRef) != "" ||
			strings.TrimSpace(cp.Platform) != "" ||
			cp.TimeoutMS != 0 || cp.MaxInputBytes != 0 || cp.MaxOutputBytes != 0 ||
			strings.TrimSpace(cp.CapabilitiesProfile) != "" || strings.TrimSpace(cp.EgressProfile) != "" {
			return fmt.Errorf("classifier_provider: disabled kind requires empty fields")
		}
		return nil
	case "openai_compatible", "openai_responses", "anthropic_messages", "gemini_generate_content", "xai_responses":
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
	if strings.TrimSpace(u.Hostname()) == "" {
		return fmt.Errorf("classifier_provider.base_url hostname is required")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("classifier_provider.base_url port must be 1-65535")
		}
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
	prof := strings.TrimSpace(cp.CapabilitiesProfile)
	switch kind {
	case "openai_responses":
		switch prof {
		case "", "openai-off-v1", "openai-implicit-v1", "openai-explicit-prefix-v1":
		default:
			return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
		}
		if strings.TrimSpace(cp.Platform) != "" {
			return fmt.Errorf("classifier_provider.platform is only valid for anthropic_messages")
		}
	case "anthropic_messages":
		switch prof {
		case "", "anthropic-off-v1",
			"anthropic-automatic-5m-v1", "anthropic-automatic-1h-v1",
			"anthropic-explicit-prefix-5m-v1", "anthropic-explicit-prefix-1h-v1":
		default:
			return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
		}
		plat := strings.TrimSpace(strings.ToLower(cp.Platform))
		switch plat {
		case "", "claude_api":
		default:
			return fmt.Errorf("classifier_provider.platform is not supported")
		}
	case "gemini_generate_content":
		switch prof {
		case "", "gemini-off-v1", "gemini-implicit-v1", "gemini-implicit-min1024-v1":
		default:
			return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
		}
		if strings.TrimSpace(cp.Platform) != "" {
			return fmt.Errorf("classifier_provider.platform is only valid for anthropic_messages")
		}
	case "xai_responses":
		switch prof {
		case "", "xai-off-v1", "xai-responses-prefix-v1":
		default:
			return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
		}
		if strings.TrimSpace(cp.Platform) != "" {
			return fmt.Errorf("classifier_provider.platform is only valid for anthropic_messages")
		}
	default:
		switch prof {
		case "", "generic-none-v1":
		case "openai-implicit-v1", "openai-explicit-prefix-v1", "openai-off-v1":
			return fmt.Errorf("classifier_provider: openai cache profiles require kind openai_responses")
		case "anthropic-off-v1", "anthropic-automatic-5m-v1", "anthropic-automatic-1h-v1",
			"anthropic-explicit-prefix-5m-v1", "anthropic-explicit-prefix-1h-v1":
			return fmt.Errorf("classifier_provider: anthropic cache profiles require kind anthropic_messages")
		case "gemini-off-v1", "gemini-implicit-v1", "gemini-implicit-min1024-v1":
			return fmt.Errorf("classifier_provider: gemini cache profiles require kind gemini_generate_content")
		case "xai-off-v1", "xai-responses-prefix-v1":
			return fmt.Errorf("classifier_provider: xai cache profiles require kind xai_responses")
		default:
			return fmt.Errorf("classifier_provider.capabilities_profile is not supported")
		}
		if strings.TrimSpace(cp.Platform) != "" {
			return fmt.Errorf("classifier_provider.platform is only valid for anthropic_messages")
		}
	}
	if len(cp.EgressProfile) > 256 {
		return fmt.Errorf("classifier_provider.egress_profile too long")
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

// UnmarshalJSONDocument decodes Config from JSON bytes after verifying no raw secrets are injected.
func UnmarshalJSONDocument(data []byte) (Config, error) {
	if err := validateNoProhibitedSecretKeys(data); err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

func validateCodexRuntime(cr CodexRuntimeConfig) error {
	exec := cr.NormalizeExecutable()
	if strings.ContainsAny(exec, "&|;$`\n\r><()\"'") {
		return fmt.Errorf("codex_runtime.executable contains illegal shell characters")
	}

	owner := cr.NormalizeCredentialOwner()
	switch owner {
	case "codex_process", "reinframe_env":
		// valid
	default:
		return fmt.Errorf("codex_runtime.credential_owner %q is not supported", cr.CredentialOwner)
	}

	reqAuth := cr.NormalizeRequiredAuth()
	switch reqAuth {
	case "chatgpt_subscription", "api_key", "unknown":
		// valid
	default:
		return fmt.Errorf("codex_runtime.required_auth %q is not supported", cr.RequiredAuth)
	}

	if cr.StatusCheckTimeoutMS < 0 || cr.StatusCheckTimeoutMS > 60000 {
		return fmt.Errorf("codex_runtime.status_check_timeout_ms out of range (0-60000)")
	}

	if cr.BinarySHA256 != "" {
		h := strings.TrimSpace(cr.BinarySHA256)
		if len(h) != 64 {
			return fmt.Errorf("codex_runtime.binary_sha256 must be 64 hex characters")
		}
		for _, r := range h {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return fmt.Errorf("codex_runtime.binary_sha256 contains non-hex characters")
			}
		}
	}
	return nil
}

// ValidateUntrustedProjectOverride verifies that an untrusted project config does not attempt
// to override protected security surfaces such as Codex executable, credential owner, worktree isolation, or raw secrets.
func ValidateUntrustedProjectOverride(base, untrusted Config) error {
	// Codex runtime boundary protection: project config cannot redirect executable or hijack credential ownership
	if untrusted.CodexRuntime.Executable != "" && untrusted.CodexRuntime.NormalizeExecutable() != base.CodexRuntime.NormalizeExecutable() {
		return fmt.Errorf("security policy violation: untrusted project config cannot override codex_runtime.executable")
	}
	if untrusted.CodexRuntime.CredentialOwner != "" && untrusted.CodexRuntime.NormalizeCredentialOwner() != base.CodexRuntime.NormalizeCredentialOwner() {
		return fmt.Errorf("security policy violation: untrusted project config cannot override codex_runtime.credential_owner")
	}
	if untrusted.CodexRuntime.BinarySHA256 != "" && base.CodexRuntime.BinarySHA256 != "" && untrusted.CodexRuntime.BinarySHA256 != base.CodexRuntime.BinarySHA256 {
		return fmt.Errorf("security policy violation: untrusted project config cannot override codex_runtime.binary_sha256")
	}

	// Worktree isolation protection
	if untrusted.Workspace.ManagedWorktreeRoot != "" && untrusted.Workspace.ManagedWorktreeRoot != base.Workspace.ManagedWorktreeRoot {
		return fmt.Errorf("security policy violation: untrusted project config cannot override workspace.managed_worktree_root")
	}
	if base.Workspace.EnforceIsolation && !untrusted.Workspace.EnforceIsolation {
		return fmt.Errorf("security policy violation: untrusted project config cannot disable workspace.enforce_isolation")
	}

	// Secret injection check
	for k, v := range untrusted.Secrets.Refs {
		if !IsEnvPlaceholder(v) {
			return fmt.Errorf("security policy violation: untrusted project config secrets.refs[%s] contains raw secret", k)
		}
	}
	return nil
}

func validateNoProhibitedSecretKeys(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	prohibited := []string{
		"oauth_token", "refresh_token", "access_token", "session_token",
		"auth_token", "cookie", "auth.json", "client_secret",
	}

	for _, p := range prohibited {
		if _, ok := raw[p]; ok {
			return fmt.Errorf("security violation: raw secret field %q is prohibited in config", p)
		}
	}

	if crRaw, ok := raw["codex_runtime"]; ok && len(crRaw) > 0 {
		var crMap map[string]json.RawMessage
		if err := json.Unmarshal(crRaw, &crMap); err == nil {
			for _, p := range prohibited {
				if _, ok := crMap[p]; ok {
					return fmt.Errorf("security violation: raw secret field %q is prohibited in codex_runtime config", p)
				}
			}
			// In codex_runtime, direct api_key field is also prohibited
			if _, ok := crMap["api_key"]; ok {
				return fmt.Errorf("security violation: raw secret field %q is prohibited in codex_runtime config", "api_key")
			}
		}
	}
	return nil
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
			if !isEnvIdentStart(r) {
				return false
			}
			continue
		}
		if !isEnvIdentCont(r) {
			return false
		}
	}
	return true
}

func isEnvIdentStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '_'
}

func isEnvIdentCont(r rune) bool {
	return isEnvIdentStart(r) || (r >= '0' && r <= '9')
}

func validateEnvPlaceholder(field, value string) error {
	if !IsEnvPlaceholder(value) {
		// Never echo the supplied value (may be a raw secret).
		return fmt.Errorf("%s must be an env placeholder like ${VAR_NAME}", field)
	}
	return nil
}
