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
