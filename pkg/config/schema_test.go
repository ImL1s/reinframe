package config_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/config"
)

func TestDefault_Validate(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
	if cfg.SchemaVersion != config.CurrentSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", cfg.SchemaVersion, config.CurrentSchemaVersion)
	}
	if !cfg.Session.LocalOnlyReviewer {
		t.Fatal("LocalOnlyReviewer default must be true (ADR 003)")
	}
	if cfg.Reviewer.Mode != "local" {
		t.Fatalf("Reviewer.Mode = %q, want local", cfg.Reviewer.Mode)
	}
	if !cfg.Workspace.EnforceIsolation {
		t.Fatal("EnforceIsolation default must be true (ADR 004)")
	}
	if !cfg.HookGate.FailOpen {
		t.Fatal("HookGate.FailOpen default expected true for observe-friendly foundation")
	}
	if cfg.CodexRuntime.Enabled {
		t.Fatal("CodexRuntime.Enabled default must be false")
	}
	if cfg.CodexRuntime.Executable != "codex" {
		t.Fatalf("CodexRuntime.Executable = %q, want codex", cfg.CodexRuntime.Executable)
	}
	if cfg.CodexRuntime.CredentialOwner != "codex_process" {
		t.Fatalf("CodexRuntime.CredentialOwner = %q, want codex_process", cfg.CodexRuntime.CredentialOwner)
	}
	if cfg.CodexRuntime.RequiredAuth != "chatgpt_subscription" {
		t.Fatalf("CodexRuntime.RequiredAuth = %q, want chatgpt_subscription", cfg.CodexRuntime.RequiredAuth)
	}
	if cfg.CodexRuntime.AllowInteractiveLogin {
		t.Fatal("CodexRuntime.AllowInteractiveLogin default must be false")
	}
}

func TestJSONRoundTrip_Default(t *testing.T) {
	t.Parallel()
	original := config.Default()
	original.Secrets.Refs = map[string]string{
		"reviewer_api_key": "${REINFRAME_REVIEWER_API_KEY}",
		"openai_api_key":   "${OPENAI_API_KEY}",
	}
	original.Reviewer.APIKeyRef = "${REINFRAME_REVIEWER_API_KEY}"
	original.Reviewer.Model = "local-stub"
	original.Session.MaxDuration = "2h"
	original.Workspace.ManagedWorktreeRoot = "/tmp/reinframe-wt"

	data, err := config.MarshalJSONDocument(original)
	if err != nil {
		t.Fatalf("MarshalJSONDocument: %v", err)
	}
	if !json.Valid(data) {
		t.Fatalf("marshal produced invalid JSON: %s", data)
	}

	// Wire names must be snake_case per tags.
	raw := string(data)
	for _, key := range []string{
		`"schema_version"`,
		`"busy_timeout"`,
		`"fail_open"`,
		`"local_only_reviewer"`,
		`"api_key_ref"`,
		`"enforce_isolation"`,
	} {
		if !strings.Contains(raw, key) {
			t.Fatalf("expected JSON key %s in document:\n%s", key, raw)
		}
	}

	restored, err := config.UnmarshalJSONDocument(data)
	if err != nil {
		t.Fatalf("UnmarshalJSONDocument: %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("restored.Validate: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("roundtrip mismatch\noriginal: %#v\nrestored: %#v", original, restored)
	}
}

func TestJSONRoundTrip_encodingJSON(t *testing.T) {
	t.Parallel()
	// Direct encoding/json path (same tags) for double-check.
	original := config.Default()
	original.Store.BusyTimeout = "1500ms"
	original.HookGate.FailOpen = false
	original.Session.DefaultLevel = 1
	// omitempty drops empty maps; use nil so DeepEqual survives roundtrip.
	original.Secrets.Refs = nil

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var restored config.Config
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(original, restored) {
		t.Fatalf("encoding/json roundtrip mismatch\noriginal: %#v\nrestored: %#v", original, restored)
	}
	d, err := restored.BusyTimeoutDuration()
	if err != nil {
		t.Fatalf("BusyTimeoutDuration: %v", err)
	}
	if d != 1500*time.Millisecond {
		t.Fatalf("BusyTimeoutDuration = %v, want 1500ms", d)
	}
}

func TestYAMLTagsPresent(t *testing.T) {
	t.Parallel()
	// Foundation stub does not pull gopkg.in/yaml yet; assert yaml tags exist
	// so future loaders share the same field names as JSON.
	typ := reflect.TypeOf(config.Config{})
	fields := map[string]string{
		"SchemaVersion": "schema_version",
		"Session":       "session",
		"Store":         "store",
		"HookGate":      "hook_gate",
		"Reviewer":      "reviewer",
		"Secrets":       "secrets",
		"Workspace":     "workspace",
		"CodexRuntime":  "codex_runtime",
	}
	for name, wantYAML := range fields {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("missing field %s", name)
		}
		tag := f.Tag.Get("yaml")
		if tag != wantYAML && !strings.HasPrefix(tag, wantYAML+",") {
			t.Fatalf("field %s yaml tag = %q, want %q", name, tag, wantYAML)
		}
		jtag := f.Tag.Get("json")
		if jtag != wantYAML && !strings.HasPrefix(jtag, wantYAML+",") {
			t.Fatalf("field %s json tag = %q, want matching %q", name, jtag, wantYAML)
		}
	}
}

func TestValidate_RejectsRawSecrets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Reviewer.APIKeyRef = "sk-live-not-a-placeholder"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for raw API key in api_key_ref")
	}

	cfg = config.Default()
	cfg.Secrets.Refs = map[string]string{"k": "plaintext-secret"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for raw secret in secrets.refs")
	}
}

func TestValidate_AcceptsEnvPlaceholders(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Reviewer.APIKeyRef = "${REINFRAME_REVIEWER_API_KEY}"
	cfg.Secrets.Refs = map[string]string{"reviewer_api_key": "${REINFRAME_REVIEWER_API_KEY}"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate with placeholders: %v", err)
	}
}

func TestValidate_SchemaVersion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.SchemaVersion = 0
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected schema_version error")
	}
}

func TestValidate_DurationsAndMode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Store.BusyTimeout = "not-a-duration"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected busy_timeout parse error")
	}

	cfg = config.Default()
	cfg.Reviewer.Mode = "telepathy"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected reviewer.mode error")
	}

	cfg = config.Default()
	cfg.Session.DefaultLevel = 9
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected default_level error")
	}
}

func TestIsEnvPlaceholder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"${OPENAI_API_KEY}", true},
		{"${A}", true},
		{"OPENAI_API_KEY", false},
		{"${}", false},
		{"${nested${x}}", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := config.IsEnvPlaceholder(tc.in); got != tc.want {
			t.Fatalf("IsEnvPlaceholder(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestClassifierProviderConfig_Validate(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	// empty kind ok
	cfg.ClassifierProvider = config.ClassifierProviderConfig{Kind: "none"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	// raw key rejected
	cfg.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "sk-raw",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("raw api key must fail")
	}
	// unknown profile
	cfg.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "${REINFRAME_CLASSIFIER_API_KEY}", CapabilitiesProfile: "native-openai",
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown profile must fail")
	}
	// invalid base_url shapes (OBJECTIVE G / P2-E origin-only) + port range
	for _, bad := range []string{
		"not-a-url", "ftp://127.0.0.1/v1", "http://user:pass@127.0.0.1:1",
		"http://127.0.0.1:1/v1", "http://127.0.0.1:1?x=1",
		"http://127.0.0.1:0", "http://127.0.0.1:65536", "http://127.0.0.1:99999",
	} {
		cfg.ClassifierProvider = config.ClassifierProviderConfig{
			Kind: "openai_compatible", Model: "m", BaseURL: bad,
			APIKeyRef: "${REINFRAME_CLASSIFIER_API_KEY}", CapabilitiesProfile: "generic-none-v1",
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("base_url %q must fail validation", bad)
		}
	}
	// disabled kind must not hide raw secrets / extraneous fields
	cfg.ClassifierProvider = config.ClassifierProviderConfig{Kind: "none", APIKeyRef: "sk-live-secret"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("kind none + raw api_key_ref must fail")
	} else if strings.Contains(err.Error(), "sk-live-secret") {
		t.Fatal("validation error must not echo raw secret")
	}
	cfg.ClassifierProvider = config.ClassifierProviderConfig{Kind: "none", Model: "m"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("kind none + model must fail")
	}
	for _, bad := range []string{"${A B}", "${A-B}", "${1KEY}", "${}"} {
		cfg.ClassifierProvider = config.ClassifierProviderConfig{
			Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
			APIKeyRef: bad, CapabilitiesProfile: "generic-none-v1",
		}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("placeholder %q must fail", bad)
		}
	}
	// good
	cfg.ClassifierProvider = config.ClassifierProviderConfig{
		Kind: "openai_compatible", Model: "m", BaseURL: "http://127.0.0.1:1",
		APIKeyRef: "${REINFRAME_CLASSIFIER_API_KEY}", CapabilitiesProfile: "generic-none-v1",
		TimeoutMS: 1500, MaxInputBytes: 1024, MaxOutputBytes: 512,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	// secret not in JSON
	b, _ := config.MarshalJSONDocument(cfg)
	if strings.Contains(string(b), "sk-") {
		t.Fatal("raw secret in json")
	}
}

func TestValidate_CodexRuntime(t *testing.T) {
	t.Parallel()

	// Default is valid
	cfg := config.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config failed validation: %v", err)
	}

	// Enabled with valid configurations
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.Executable = "codex"
	cfg.CodexRuntime.CredentialOwner = "codex_process"
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"
	cfg.CodexRuntime.AllowInteractiveLogin = false
	cfg.CodexRuntime.RuntimeProfile = "default"
	cfg.CodexRuntime.StatusCheckTimeoutMS = 5000
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid enabled codex runtime config failed: %v", err)
	}

	// reinframe_env and api_key are valid closed enums
	cfg.CodexRuntime.CredentialOwner = "reinframe_env"
	cfg.CodexRuntime.RequiredAuth = "api_key"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid env/api_key config failed: %v", err)
	}

	// Valid SHA256 hex digest
	cfg.CodexRuntime.BinarySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid sha256 failed: %v", err)
	}

	// Illegal shell metacharacters in executable name/path
	for _, badExec := range []string{
		"codex; rm -rf /",
		"codex | bash",
		"codex && evil",
		"`evil`",
		"$(evil)",
		"codex > out",
		"codex\nmalicious",
		`codex"`,
	} {
		badCfg := cfg
		badCfg.CodexRuntime.Executable = badExec
		if err := badCfg.Validate(); err == nil {
			t.Fatalf("expected error for shell metacharacters in executable %q", badExec)
		}
	}

	// Invalid credential owner
	for _, badOwner := range []string{"root", "shared", "oauth", "other"} {
		badCfg := cfg
		badCfg.CodexRuntime.CredentialOwner = badOwner
		if err := badCfg.Validate(); err == nil {
			t.Fatalf("expected error for invalid credential owner %q", badOwner)
		}
	}

	// Invalid required auth
	for _, badAuth := range []string{"oauth2", "cookie", "token", "password"} {
		badCfg := cfg
		badCfg.CodexRuntime.RequiredAuth = badAuth
		if err := badCfg.Validate(); err == nil {
			t.Fatalf("expected error for invalid required auth %q", badAuth)
		}
	}

	// Timeout out of bounds
	badCfg := cfg
	badCfg.CodexRuntime.StatusCheckTimeoutMS = -1
	if err := badCfg.Validate(); err == nil {
		t.Fatal("expected error for negative status_check_timeout_ms")
	}
	badCfg.CodexRuntime.StatusCheckTimeoutMS = 70000
	if err := badCfg.Validate(); err == nil {
		t.Fatal("expected error for status_check_timeout_ms > 60000")
	}

	// Invalid binary SHA256 (not 64 hex characters)
	badCfg = cfg
	badCfg.CodexRuntime.BinarySHA256 = "not-a-valid-sha256"
	if err := badCfg.Validate(); err == nil {
		t.Fatal("expected error for non-hex sha256")
	}
}

func TestValidate_CodexRuntime_ProhibitedSecretKeys(t *testing.T) {
	t.Parallel()

	// Direct raw secret fields in JSON must be rejected by UnmarshalJSONDocument
	prohibitedJSONs := []string{
		`{"schema_version":1,"store":{"busy_timeout":"5s"},"reviewer":{"mode":"local"},"codex_runtime":{"enabled":true,"oauth_token":"gho_secret"}}`,
		`{"schema_version":1,"store":{"busy_timeout":"5s"},"reviewer":{"mode":"local"},"codex_runtime":{"enabled":true,"refresh_token":"rt_secret"}}`,
		`{"schema_version":1,"store":{"busy_timeout":"5s"},"reviewer":{"mode":"local"},"codex_runtime":{"enabled":true,"api_key":"sk-secret"}}`,
		`{"schema_version":1,"store":{"busy_timeout":"5s"},"reviewer":{"mode":"local"},"codex_runtime":{"enabled":true,"cookie":"session=123"}}`,
		`{"schema_version":1,"store":{"busy_timeout":"5s"},"reviewer":{"mode":"local"},"oauth_token":"root_secret"}`,
	}

	for _, badJSON := range prohibitedJSONs {
		_, err := config.UnmarshalJSONDocument([]byte(badJSON))
		if err == nil {
			t.Fatalf("expected UnmarshalJSONDocument to reject prohibited secret in JSON:\n%s", badJSON)
		}
		if !strings.Contains(err.Error(), "security violation") {
			t.Fatalf("expected security violation error message, got: %v", err)
		}
	}
}

func TestValidateUntrustedProjectOverride(t *testing.T) {
	t.Parallel()

	base := config.Default()
	base.CodexRuntime.Executable = "/usr/local/bin/codex"
	base.CodexRuntime.CredentialOwner = "codex_process"
	base.CodexRuntime.BinarySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	base.Workspace.ManagedWorktreeRoot = "/var/worktrees/wt1"
	base.Workspace.EnforceIsolation = true

	// Safe project config (matching base constraints)
	safeProject := config.Default()
	safeProject.CodexRuntime.Executable = "/usr/local/bin/codex"
	safeProject.CodexRuntime.CredentialOwner = "codex_process"
	safeProject.CodexRuntime.BinarySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	safeProject.Workspace.ManagedWorktreeRoot = "/var/worktrees/wt1"
	safeProject.Workspace.EnforceIsolation = true

	if err := config.ValidateUntrustedProjectOverride(base, safeProject); err != nil {
		t.Fatalf("expected safe project override to pass, got: %v", err)
	}

	// Untrusted override: attempting to change executable
	badExecProject := safeProject
	badExecProject.CodexRuntime.Executable = "/tmp/malicious/codex"
	if err := config.ValidateUntrustedProjectOverride(base, badExecProject); err == nil {
		t.Fatal("expected error when project overrides codex_runtime.executable")
	}

	// Untrusted override: attempting to change credential owner
	badOwnerProject := safeProject
	badOwnerProject.CodexRuntime.CredentialOwner = "reinframe_env"
	if err := config.ValidateUntrustedProjectOverride(base, badOwnerProject); err == nil {
		t.Fatal("expected error when project overrides codex_runtime.credential_owner")
	}

	// Untrusted override: attempting to change binary sha256
	badHashProject := safeProject
	badHashProject.CodexRuntime.BinarySHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	if err := config.ValidateUntrustedProjectOverride(base, badHashProject); err == nil {
		t.Fatal("expected error when project overrides codex_runtime.binary_sha256")
	}

	// Untrusted override: attempting to disable worktree isolation
	badIsoProject := safeProject
	badIsoProject.Workspace.EnforceIsolation = false
	if err := config.ValidateUntrustedProjectOverride(base, badIsoProject); err == nil {
		t.Fatal("expected error when project disables workspace.enforce_isolation")
	}

	// Untrusted override: attempting to inject raw secrets into secrets.refs
	badSecretProject := safeProject
	badSecretProject.Secrets.Refs = map[string]string{
		"injected": "raw_secret_password",
	}
	if err := config.ValidateUntrustedProjectOverride(base, badSecretProject); err == nil {
		t.Fatal("expected error when project injects raw secrets into secrets.refs")
	}
}

