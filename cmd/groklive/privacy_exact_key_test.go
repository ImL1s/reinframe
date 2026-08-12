package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Exact-key privacy regression suite for contentHasPrivateReasoning (#post-222).
// Calls shipped production helpers only; does not reimplement detection.

func TestContentHasPrivateReasoning_MandatoryFalse_ExactKeyOnly(t *testing.T) {
	t.Parallel()
	falseCases := []string{
		`{"note":"a thought: experiment"}`,
		`{"note":"the key \"thought\": is forbidden"}`,
		`{"mascot":"x"}`,
		`{"not_reasoning":"x"}`,
		`{"cotton":"x"}`,
		`{"reasoning_note":"x"}`,
		`["thought"]`,
		`thought: prose`,
		`ordinary prose thought:`,
	}
	for _, tc := range falseCases {
		tc := tc
		if contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want false (exact-key only; no prose/suffix FP): %s", tc)
		}
	}
}

func TestContentHasPrivateReasoning_MandatoryTrue_ExactKeys(t *testing.T) {
	t.Parallel()
	trueCases := []string{
		`{"thought":"secret"}`,
		`{"reasoning_content":"secret"}`,
		`{"reasoning-content":"secret"}`,
		`{"nested":{"private_reasoning":"secret"}}`,
		`{"stdout":"{\"thought\":\"secret\"}"}`,
		`{\"thought\":\"secret\"}`,
		`\\\"thought\\\": \"secret\"`,
	}
	for _, tc := range trueCases {
		tc := tc
		if !contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want true for forbidden exact key: %s", tc)
		}
	}
}

// Nested contract: production treats a complete nested JSON object (with a
// forbidden thought key) embedded as a string value under "note" as private evidence.
func TestContentHasPrivateReasoning_NestedJSONStringInNote_IsPrivateEvidence(t *testing.T) {
	t.Parallel()
	payload := `{"note":"{\"thought\":\"example only\"}"}`
	if !contentHasPrivateReasoning([]byte(payload)) {
		t.Fatalf("nested complete JSON object with thought key in note must be private evidence: %s", payload)
	}
}

func TestContentHasPrivateReasoning_CaseInsensitiveKeys(t *testing.T) {
	t.Parallel()
	cases := []string{
		`{"Thought":"secret"}`,
		`{"THOUGHT":"secret"}`,
		`{"Reasoning_Content":"secret"}`,
		`{"PRIVATE_REASONING":"x"}`,
		`{"Reasoning-Content":"x"}`,
	}
	for _, tc := range cases {
		tc := tc
		if !contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want true for case-insensitive forbidden key: %s", tc)
		}
	}
}

func TestSanitizeTrustLaunchCapture_HashesSessionRequest_NoThought_StdoutRawFalse(t *testing.T) {
	t.Parallel()
	raw := `{
  "text": "TRUST_OK",
  "stopReason": "end_turn",
  "sessionId": "sess-exact-key-001",
  "requestId": "req-exact-key-002",
  "thought": "must never persist private chain",
  "usage": {"input_tokens": 3, "output_tokens": 1}
}`
	out := sanitizeTrustLaunchCapture(0, raw, "")
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(strings.ToLower(s), `"thought"`) {
		t.Fatalf("sanitized must not contain thought key: %s", s)
	}
	if strings.Contains(s, "private chain") {
		t.Fatalf("thought body leaked: %s", s)
	}
	sid, _ := out["session_id_sha256"].(string)
	if sid == "" || sid == "sess-exact-key-001" || len(sid) != 64 {
		t.Fatalf("session id must be SHA-256 hex, got %v", out["session_id_sha256"])
	}
	rid, _ := out["request_id_sha256"].(string)
	if rid == "" || rid == "req-exact-key-002" || len(rid) != 64 {
		t.Fatalf("request id must be SHA-256 hex, got %v", out["request_id_sha256"])
	}
	if out["stdout_raw"] != false {
		t.Fatalf("stdout_raw must be false, got %v", out["stdout_raw"])
	}
	if err := validateEvidencePrivacyRejects("trust_launch", out); err != nil {
		t.Fatalf("sanitized capture must pass privacy rejector: %v", err)
	}
}

func TestValidateEvidencePrivacyRejects_UnhashedTargetSessionIDUUID(t *testing.T) {
	t.Parallel()
	payload := map[string]any{
		"id":                "ACP-EXACT-KEY-001",
		"target_session_id": "019fefc9-c816-7233-99f4-07dc7074a7c0",
	}
	if err := validateEvidencePrivacyRejects("scenario", payload); err == nil {
		t.Fatal("unhashed UUID target_session_id must be rejected")
	}
	payload["target_session_id"] = sha256Hex("019fefc9-c816-7233-99f4-07dc7074a7c0")
	if err := validateEvidencePrivacyRejects("scenario", payload); err != nil {
		t.Fatalf("hashed target_session_id must be allowed: %v", err)
	}
}

func TestContentHasPrivateReasoning_CamelCaseReasoningKeys(t *testing.T) {
	t.Parallel()
	// camelCase must normalize to forbidden underscore form.
	for _, tc := range []string{
		`{"reasoningContent":"secret"}`,
		`{"chainOfThought":"secret"}`,
		`{"privateReasoning":"x"}`,
	} {
		if !contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want true for camelCase forbidden key: %s", tc)
		}
	}
}

func TestValidateEvidencePrivacyRejects_CaseFoldedIdentityKeys(t *testing.T) {
	t.Parallel()
	// Case variants of session/request/stdout must be rejected on write path.
	for _, payload := range []map[string]any{
		{"SessionId": "019fefc9-c816-7233-99f4-07dc7074a7c0"},
		{"RequestId": "d1bb0000-0000-0000-0000-000000000001"},
		{"STDOUT": "raw host body"},
		{"target_session_id": map[string]any{"uuid": "019fefc9-c816-7233-99f4-07dc7074a7c0"}},
	} {
		if err := validateEvidencePrivacyRejects("scenario", payload); err == nil {
			t.Fatalf("must reject case/type identity leak: %#v", payload)
		}
	}
}

func TestRedactLocalIdentity_HomeAndHostname(t *testing.T) {
	t.Parallel()
	in := `/Users/alice/.local/bin/grok on host-test.local path=/var/folders/9w/abc/project`
	out := redactLocalIdentity(in)
	if strings.Contains(out, "/Users/alice") || strings.Contains(out, "host-test.local") || strings.Contains(out, "/var/folders/") {
		t.Fatalf("local identity leaked: %s", out)
	}
	if !strings.Contains(out, "[HOME]") || !strings.Contains(out, "[HOSTNAME]") || !strings.Contains(out, "[TMP:") {
		t.Fatalf("expected redaction placeholders: %s", out)
	}
}

func TestContentHasLocalIdentityLeak(t *testing.T) {
	t.Parallel()
	if !contentHasLocalIdentityLeak(`binary=/Users/foo/bar`) {
		t.Fatal("want leak for /Users path")
	}
	if contentHasLocalIdentityLeak(`binary=grok path_sha256=abc hostname=[HOSTNAME]`) {
		t.Fatal("redacted text must not leak")
	}
	// Mixed: one good placeholder must not mask a residual raw /tmp path.
	if !contentHasLocalIdentityLeak(`ok=[TMP:abcd1234ef567890] bad=/tmp/alice/project`) {
		t.Fatal("raw /tmp path must leak even when a [TMP:…] placeholder is present")
	}
	if contentHasLocalIdentityLeak(`only=[TMP:abcd1234ef567890] and /tmp alone`) {
		t.Fatal("placeholder-only (and bare /tmp word without path segment) must not leak")
	}
	// Windows user + temp (raw and JSON-escaped).
	if !contentHasLocalIdentityLeak(`C:\Users\Alice\project`) {
		t.Fatal("want leak for Windows Users path")
	}
	if !contentHasLocalIdentityLeak(`C:\\Users\\Alice\\project`) {
		t.Fatal("want leak for JSON-escaped Windows Users path")
	}
	if !contentHasLocalIdentityLeak(`C:\Users\Alice\AppData\Local\Temp\x`) {
		t.Fatal("want leak for Windows Temp path")
	}
	if !contentHasLocalIdentityLeak(`C:\\Users\\Alice\\AppData\\Local\\Temp\\x`) {
		t.Fatal("want leak for JSON-escaped Windows Temp path")
	}
}

func TestRedactLsOwnership_StripsOwnerGroup(t *testing.T) {
	t.Parallel()
	in := "lrwxr-xr-x@ 1 alice  staff  27 Jun 18 00:14 [HOME]/.local/bin/grok"
	out := redactLsOwnership(in)
	if strings.Contains(out, "alice") || strings.Contains(out, "staff") {
		t.Fatalf("owner/group leaked: %s", out)
	}
	if !strings.Contains(out, "[USER]") || !strings.Contains(out, "[GROUP]") {
		t.Fatalf("expected placeholders: %s", out)
	}
	// Full redactLocalIdentity also covers env USER and ls.
	out2 := redactLocalIdentity(in)
	if strings.Contains(out2, "alice") {
		t.Fatalf("redactLocalIdentity left owner: %s", out2)
	}
}

func TestRedactLocalIdentity_RuntimeHostname(t *testing.T) {
	t.Parallel()
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" || h == "localhost" {
		t.Skip("no usable runtime hostname")
	}
	in := "Darwin " + h + " 25.4.0 Darwin Kernel"
	out := redactLocalIdentity(in)
	if strings.Contains(out, h) {
		t.Fatalf("runtime hostname not redacted: %s", out)
	}
	if !strings.Contains(out, "[HOSTNAME]") {
		t.Fatalf("expected [HOSTNAME]: %s", out)
	}
	if !contentHasLocalIdentityLeak(in) {
		t.Fatal("scanner must flag unredacted runtime hostname")
	}
	if contentHasLocalIdentityLeak(out) {
		t.Fatal("redacted uname line must not leak")
	}
	// Mixed placeholder + residual raw hostname must still fail (Codex P1).
	mixed := "host=[HOSTNAME] also raw=" + h
	if !contentHasLocalIdentityLeak(mixed) {
		t.Fatal("mixed placeholder+raw hostname must be a leak")
	}
}

func TestRedactHostnameToken_DoesNotCorruptSchemaIDs(t *testing.T) {
	t.Parallel()
	// Unrestricted ReplaceAll of hostname "build" would rewrite grok_build schema ids.
	in := `{"schema":"reinframe.grok_build_live_control.v2","goos":"darwin","note":"host build is online"}`
	out := redactHostnameToken(in, "build")
	if !strings.Contains(out, "grok_build") {
		t.Fatalf("schema token corrupted: %s", out)
	}
	if !strings.Contains(out, "goos") {
		t.Fatalf("goos key corrupted: %s", out)
	}
	if !strings.Contains(out, "[HOSTNAME]") {
		t.Fatalf("standalone hostname token not redacted: %s", out)
	}
	// Short hostname "go" must not rewrite goos/goarch keys.
	in2 := `{"goos":"linux","goarch":"amd64","msg":" go "}`
	out2 := redactHostnameToken(in2, "go")
	if !strings.Contains(out2, `"goos"`) || !strings.Contains(out2, `"goarch"`) {
		t.Fatalf("goos/goarch keys corrupted: %s", out2)
	}
	if !strings.Contains(out2, "[HOSTNAME]") {
		t.Fatalf("standalone go token not redacted: %s", out2)
	}
	// Case-insensitive redaction/scan (Pro R14 P1).
	mixed := "Darwin BUILD-01 kernel; also build-01 and Build-01"
	outM := redactHostnameToken(mixed, "BUILD-01")
	if strings.Contains(outM, "BUILD-01") || strings.Contains(outM, "build-01") || strings.Contains(outM, "Build-01") {
		t.Fatalf("case variants not redacted: %s", outM)
	}
	if !hostnameTokenPresent("uname says build-01", "BUILD-01") {
		t.Fatal("scanner must detect case-variant hostname")
	}
	if hostnameTokenPresent("reinframe.grok_build_acp.v1", "BUILD") {
		t.Fatal("case-insensitive matcher must still respect token boundaries")
	}
}

func TestHostnameTokenPresent_MatchesRedactorBoundaries(t *testing.T) {
	t.Parallel()
	// Scanner and redactor must agree: embedded "build" is not a leak.
	schema := `reinframe.grok_build_acp.v1 and reinframe.grok_build_live_control.v2`
	if hostnameTokenPresent(schema, "build") {
		t.Fatal("embedded build in schema id must not be a hostname token")
	}
	if hostnameTokenPresent(`{"goos":"linux"}`, "go") {
		t.Fatal("goos key must not count as hostname token go")
	}
	if !hostnameTokenPresent("Darwin build 25.4.0", "build") {
		t.Fatal("standalone build token must be detected")
	}
	// contentHasLocalIdentityLeak must not false-flag schema-only text when
	// runtime hostname happens to be a common token (Codex P2).
	// We cannot force os.Hostname(); instead assert token helper used by the scanner.
	if contentHasLocalIdentityLeak(schema) && strings.Contains(schema, "/Users/") {
		t.Fatal("unexpected path leak in schema fixture")
	}
	// Schema-only string has no path/.local; leak only if runtime hostname is
	// an unrestricted substring — that is the bug we fixed via token boundaries.
	// If this machine's hostname is "build", the old scanner would flag; new must not.
	if h, err := os.Hostname(); err == nil {
		h = strings.TrimSpace(h)
		if h == "build" || h == "go" {
			if contentHasLocalIdentityLeak(schema) {
				t.Fatalf("schema-only evidence must not leak for hostname=%s", h)
			}
		}
	}
}

func TestContentHasLocalIdentityLeak_MidLineLsOwner(t *testing.T) {
	t.Parallel()
	mid := `{"stdout":"lrwxr-xr-x@ 1 alice  staff  27 Jun 18 00:14 [HOME]/.local/bin/grok"}`
	if !contentHasLocalIdentityLeak(mid) {
		t.Fatal("mid-line ls owner must be a leak")
	}
	out := redactLocalIdentity(mid)
	if strings.Contains(out, "alice") {
		t.Fatalf("redact left owner: %s", out)
	}
	if contentHasLocalIdentityLeak(out) {
		t.Fatalf("redacted mid-line ls must not leak: %s", out)
	}
}

func TestRedactLocalIdentity_WindowsPaths(t *testing.T) {
	t.Parallel()
	in := `C:\Users\Alice\project and C:\\Users\\Alice\\AppData\\Local\\Temp\\run`
	out := redactLocalIdentity(in)
	if strings.Contains(out, `Users\Alice`) || strings.Contains(out, `Users\\Alice`) {
		t.Fatalf("Windows user path leaked: %s", out)
	}
	if strings.Contains(out, `AppData`) || strings.Contains(out, `Temp\\run`) || strings.Contains(out, `Temp\run`) {
		t.Fatalf("Windows temp path leaked: %s", out)
	}
	if !strings.Contains(out, "[HOME]") {
		t.Fatalf("expected [HOME] placeholder: %s", out)
	}
	// writeJSON path: redaction after marshal must still catch escaped backslashes.
	raw := `{"p":"C:\\Users\\Bob\\work"}`
	got := redactLocalIdentity(raw)
	if strings.Contains(got, "Bob") || strings.Contains(got, `Users\\`) {
		t.Fatalf("JSON-escaped path not redacted: %s", got)
	}
}

func TestRedactLocalIdentity_DoesNotCorruptGrokVersion(t *testing.T) {
	// When USER is a common product token, version/prose must not be rewritten (Pro R12).
	// Not parallel: uses t.Setenv.
	t.Setenv("USER", "grok")
	t.Setenv("LOGNAME", "grok")
	t.Setenv("USERNAME", "grok")
	in := `{"version":"grok 1.0.0 (3cd0d0cbcebe)","note":"agent stdio"}`
	out := redactLocalIdentity(in)
	if !strings.Contains(out, "grok 1.0.0") {
		t.Fatalf("version token corrupted: %s", out)
	}
	if strings.Contains(out, "[USER] 1.0") {
		t.Fatalf("account redaction rewrote version: %s", out)
	}
	// Scanner must also accept ordinary product/version text (Pro R13 P2).
	if contentHasLocalIdentityLeak(in) {
		t.Fatalf("scanner false-flagged product version as account leak: %s", in)
	}
	if contentHasLocalIdentityLeak(out) {
		t.Fatalf("scanner false-flagged redacted product version: %s", out)
	}
	// Path segments still redacted.
	pathIn := `/Users/grok/.local/bin/grok`
	pathOut := redactLocalIdentity(pathIn)
	if strings.Contains(pathOut, "/Users/grok/") {
		t.Fatalf("path segment not redacted: %s", pathOut)
	}
}
