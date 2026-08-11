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
