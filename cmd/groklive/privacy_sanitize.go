package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// Forbidden private-reasoning JSON keys (case-insensitive) for evidence privacy (#post-222).
// Includes Grok host field "thought" which is not covered by the old substring list alone.
var forbiddenReasoningKeys = map[string]struct{}{
	"thought":            {},
	"thoughts":           {},
	"raw_thoughts":       {},
	"private_reasoning":  {},
	"thinking_blocks":    {},
	"reasoning_content":  {},
	"encrypted_content":  {},
	"reasoning":          {},
	"chain_of_thought":   {},
	"cot":                {},
	"internal_monologue": {},
}

// sanitizeTrustLaunchCapture builds a closed allowlist trust_launch record.
// Never persists raw host stdout/stderr that may embed thought/session/request material.
func sanitizeTrustLaunchCapture(exitCode int, stdout, stderr string) map[string]any {
	out := map[string]any{
		"schema":      "reinframe.trust_launch.v1",
		"exit":        exitCode,
		"capture":     "closed_allowlist",
		"stdout_raw":  false,
		"stderr_raw":  false,
		"text":        "",
		"stop_reason": "",
	}
	// Usage is optional closed numeric map only.
	usageOut := map[string]any{}

	// Prefer structured parse of stdout as Grok --output-format json.
	var top map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &top); err == nil && top != nil {
		if t, ok := top["text"].(string); ok {
			out["text"] = boundStr(redactSecrets(t), 200)
		}
		if sr, ok := top["stopReason"].(string); ok {
			out["stop_reason"] = boundStr(sr, 64)
		} else if sr, ok := top["stop_reason"].(string); ok {
			out["stop_reason"] = boundStr(sr, 64)
		}
		if sid, ok := top["sessionId"].(string); ok && sid != "" {
			out["session_id_sha256"] = sha256Hex(sid)
		}
		if rid, ok := top["requestId"].(string); ok && rid != "" {
			out["request_id_sha256"] = sha256Hex(rid)
		}
		if u, ok := top["usage"].(map[string]any); ok {
			for _, k := range []string{
				"input_tokens", "output_tokens", "cache_read_input_tokens",
				"cache_creation_input_tokens", "total_tokens",
			} {
				if v, ok := u[k]; ok {
					switch v.(type) {
					case float64, int, int64, json.Number:
						usageOut[k] = v
					}
				}
			}
		}
		// Explicitly never copy thought / reasoning keys even if present.
	} else if strings.TrimSpace(stdout) != "" {
		// Non-JSON: only length + hash of redacted bytes — never raw body.
		red := redactSecrets(stdout)
		out["stdout_bytes"] = len(stdout)
		out["stdout_sha256"] = sha256Hex(red)
		// Best-effort plain text extract if it looks short and has no forbidden markers.
		if !contentHasPrivateReasoning([]byte(red)) && len(red) <= 80 {
			out["text"] = boundStr(red, 80)
		}
	}

	if strings.TrimSpace(stderr) != "" {
		red := redactSecrets(stderr)
		// Keep only a short bounded diagnostic if free of private reasoning keys.
		if !contentHasPrivateReasoning([]byte(red)) {
			out["stderr_text"] = boundStr(red, 200)
		} else {
			out["stderr_bytes"] = len(stderr)
			out["stderr_sha256"] = sha256Hex(red)
			out["stderr_redacted_private"] = true
		}
	}
	if len(usageOut) > 0 {
		out["usage"] = usageOut
	}
	return out
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// contentHasPrivateReasoning reports private reasoning material in evidence bytes.
// Walks JSON (including string values that themselves contain JSON) and also checks
// escaped key forms in raw text for nested stdout captures.
func contentHasPrivateReasoning(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	// Fast path: plain or escaped forbidden keys (covers nested stdout JSON text).
	if hasEscapedForbiddenReasoningKey(string(b)) {
		return true
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return false
	}
	return walkJSONForPrivateReasoning(v, 0)
}

func hasEscapedForbiddenReasoningKey(s string) bool {
	// Match \"thought\" and \\"thought\\" variants inside nested encodings.
	low := strings.ToLower(s)
	for k := range forbiddenReasoningKeys {
		if strings.Contains(low, `\"`+k+`\"`) || strings.Contains(low, `\\"`+k+`\\"`) {
			return true
		}
		// Also bare "thought" after JSON string unescape layers in raw file text.
		if strings.Contains(low, `"`+k+`"`) {
			return true
		}
	}
	return false
}

func walkJSONForPrivateReasoning(v any, depth int) bool {
	if depth > 32 {
		return false
	}
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			lk := strings.ToLower(strings.TrimSpace(k))
			// Normalize separators.
			lk = strings.Map(func(r rune) rune {
				if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
					return r
				}
				return -1
			}, lk)
			if _, bad := forbiddenReasoningKeys[lk]; bad {
				return true
			}
			if walkJSONForPrivateReasoning(child, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if walkJSONForPrivateReasoning(child, depth+1) {
				return true
			}
		}
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return false
		}
		// Nested JSON in string fields (stdout payloads).
		if (strings.HasPrefix(s, "{") || strings.HasPrefix(s, "[")) && len(s) < 1<<20 {
			var inner any
			if json.Unmarshal([]byte(s), &inner) == nil {
				if walkJSONForPrivateReasoning(inner, depth+1) {
					return true
				}
			}
		}
		if hasEscapedForbiddenReasoningKey(s) {
			return true
		}
	}
	return false
}

// validateEvidencePrivacyRejects reports an error if payload must not be written as evidence.
func validateEvidencePrivacyRejects(label string, payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if contentHasPrivateReasoning(b) {
		return fmt.Errorf("%s: refuses to write private reasoning / thought fields", label)
	}
	// Reject unhashed identity keys and raw stdout anywhere in the tree.
	if containsForbiddenEvidenceShape(payload) {
		return fmt.Errorf("%s: refuses unhashed identity or raw stdout fields", label)
	}
	return nil
}

func containsForbiddenEvidenceShape(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			switch k {
			case "sessionId", "session_id", "requestId", "request_id", "stdout":
				return true
			}
			if containsForbiddenEvidenceShape(child) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsForbiddenEvidenceShape(child) {
				return true
			}
		}
	}
	return false
}
