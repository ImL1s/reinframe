package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSanitizeTrustLaunchCapture_StripsThought(t *testing.T) {
	t.Parallel()
	raw := `{
  "text": "TRUST_OK",
  "stopReason": "end_turn",
  "sessionId": "sess-abc-123",
  "requestId": "req-xyz-456",
  "thought": "The user wants me to simply say TRUST_OK and stop.",
  "usage": {"input_tokens": 10, "output_tokens": 2}
}`
	out := sanitizeTrustLaunchCapture(0, raw, "")
	b, _ := json.Marshal(out)
	s := string(b)
	if strings.Contains(strings.ToLower(s), `"thought"`) {
		t.Fatalf("sanitized must not contain thought key: %s", s)
	}
	if strings.Contains(s, "simply say") {
		t.Fatalf("thought body leaked: %s", s)
	}
	if out["text"] != "TRUST_OK" {
		t.Fatalf("text=%v", out["text"])
	}
	if out["stop_reason"] != "end_turn" {
		t.Fatalf("stop=%v", out["stop_reason"])
	}
	if out["session_id_sha256"] == nil || out["session_id_sha256"] == "sess-abc-123" {
		t.Fatalf("session id must be hashed, got %v", out["session_id_sha256"])
	}
	if out["request_id_sha256"] == nil || out["request_id_sha256"] == "req-xyz-456" {
		t.Fatalf("request id must be hashed, got %v", out["request_id_sha256"])
	}
	if out["stdout_raw"] != false {
		t.Fatal("stdout_raw must be false")
	}
	if err := validateEvidencePrivacyRejects("trust_launch", out); err != nil {
		t.Fatalf("sanitized must pass privacy rejector: %v", err)
	}
}

func TestSanitizeTrustLaunchCapture_RejectsRawStdoutWrite(t *testing.T) {
	t.Parallel()
	bad := map[string]any{
		"exit":   0,
		"stdout": `{"text":"TRUST_OK","thought":"private chain"}`,
	}
	if err := validateEvidencePrivacyRejects("trust_launch", bad); err == nil {
		t.Fatal("must reject raw stdout with thought")
	}
}

func TestContentHasPrivateReasoning_NestedEscapedThought(t *testing.T) {
	t.Parallel()
	// Shape matching the leaked trust_launch on main after PR #222.
	payload := map[string]any{
		"exit":   0,
		"stdout": "{\n  \"text\": \"TRUST_OK\",\n  \"thought\": \"The user wants me to simply say TRUST_OK\",\n  \"sessionId\": \"abc\"\n}",
	}
	b, _ := json.Marshal(payload)
	if !contentHasPrivateReasoning(b) {
		t.Fatalf("must detect thought inside nested stdout JSON: %s", string(b))
	}
}

func TestContentHasPrivateReasoning_TopLevelThought(t *testing.T) {
	t.Parallel()
	b := []byte(`{"thought":"secret"}`)
	if !contentHasPrivateReasoning(b) {
		t.Fatal("top-level thought must be detected")
	}
}

func TestContentHasPrivateReasoning_KeyPositionOnly(t *testing.T) {
	t.Parallel()
	// Prose / array value must not trip the detector.
	if contentHasPrivateReasoning([]byte(`{"note":"the word thought appears here"}`)) {
		t.Fatal("prose must not match")
	}
	if contentHasPrivateReasoning([]byte(`{"fields":["thought"]}`)) {
		t.Fatal("array value must not match as key")
	}
	// Hyphenated key normalizes to underscore form.
	if !contentHasPrivateReasoning([]byte(`{"reasoning-content":"x"}`)) {
		t.Fatal("reasoning-content key must match")
	}
}

func TestSanitizeTrustLaunch_NonJSONDoesNotCopyBody(t *testing.T) {
	t.Parallel()
	out := sanitizeTrustLaunchCapture(1, "requestId=req-123 short fail", "boom")
	if out["text"] != "" {
		t.Fatalf("non-JSON body must not be copied into text: %v", out["text"])
	}
	if _, ok := out["stdout_sha256"]; !ok {
		t.Fatal("expected stdout_sha256")
	}
	if _, ok := out["stderr_text"]; ok {
		t.Fatal("stderr_text must not be set for unstructured stderr")
	}
}

func TestScanPrivacy_DetectsThoughtInTrustLaunchShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Pre-fix shape: bounded stdout still carrying thought.
	body := `{
  "exit": 0,
  "stderr": "",
  "stdout": "{\n  \"text\": \"TRUST_OK\",\n  \"stopReason\": \"end_turn\",\n  \"sessionId\": \"019feca4-682d-7181-9228-fc242426e232\",\n  \"requestId\": \"d1bbb2e4-938f-43c0-a24c-e62570ec047b\",\n  \"thought\": \"The user wants me to simply say TRUST_OK and stop.\",\n  \"usage\": {\n    \"input_tokens\": 10\n  }\n}"
}`
	if err := os.WriteFile(filepath.Join(dir, "trust_launch.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["raw_thoughts_stored"] != true {
		t.Fatalf("scanPrivacy must set raw_thoughts_stored for nested thought: %+v", p)
	}
}

func TestContentHasPrivateReasoning_ExactQuotedKeyOnly(t *testing.T) {
	t.Parallel()
	// Mandatory true cases
	trueCases := []string{
		`{"thought":"secret"}`,
		`{"reasoning_content":"secret"}`,
		`{"reasoning-content":"secret"}`,
		`{"nested":{"private_reasoning":"secret"}}`,
		`{"stdout":"{\"thought\":\"secret\"}"}`,
		`{"thought" : "secret"}`,
	}
	for _, tc := range trueCases {
		if !contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want true for exact key case: %s", tc)
		}
	}
	// Non-JSON but fully quoted key + colon (escaped layers)
	escaped := []string{
		`\"thought\":\"secret\"`,
		`\\"thought\\": \"secret\"`,
	}
	for _, tc := range escaped {
		if !contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("want true for escaped quoted key: %s", tc)
		}
	}
}

func TestContentHasPrivateReasoning_DoesNotMatchKeySuffix(t *testing.T) {
	t.Parallel()
	falseCases := []string{
		`{"mascot":"x"}`,
		`{"not_reasoning":"x"}`,
		`{"cotton":"x"}`,
		`{"reasoning_note":"x"}`,
	}
	for _, tc := range falseCases {
		if contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("suffix/partial key must not match: %s", tc)
		}
	}
}

func TestContentHasPrivateReasoning_DoesNotMatchProseValue(t *testing.T) {
	t.Parallel()
	falseCases := []string{
		`{"note":"a thought: experiment"}`,
		`{"note":"the key \"thought\": is forbidden"}`,
		`["thought"]`,
		`thought: prose`,
	}
	for _, tc := range falseCases {
		if contentHasPrivateReasoning([]byte(tc)) {
			t.Fatalf("prose/array value must not match: %s", tc)
		}
	}
}

func TestContentHasPrivateReasoning_EscapedNestedJSON(t *testing.T) {
	t.Parallel()
	// Outer JSON with nested JSON string containing a real thought key.
	outer := map[string]any{
		"stdout": `{"thought":"secret"}`,
	}
	b, err := json.Marshal(outer)
	if err != nil {
		t.Fatal(err)
	}
	if !contentHasPrivateReasoning(b) {
		t.Fatalf("nested JSON-in-string must detect thought key: %s", string(b))
	}
	// Escaped form as raw non-JSON text (trust capture residue)
	if !contentHasPrivateReasoning([]byte(`{\"thought\":\"secret\"}`)) {
		t.Fatal("escaped quoted thought key must match fallback")
	}
}
