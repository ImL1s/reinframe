package main

import (
	"encoding/json"
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
