package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
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

// forbiddenQuotedKeyColon matches only complete quoted JSON keys (optional
// backslash-escape layers around the quotes) followed by a colon.
// Quotes are required — bare `thought:` or suffix keys like mascot/cotton never match.
var forbiddenQuotedKeyColon = func() *regexp.Regexp {
	names := make([]string, 0, len(forbiddenReasoningKeys))
	for k := range forbiddenReasoningKeys {
		names = append(names, regexp.QuoteMeta(k))
	}
	sort.Strings(names)
	// Required quotes around the exact key; optional backslash layers before each quote.
	// Examples: "thought":   \"thought\":   \\"thought\\":
	// Never optional-quote / bare-name forms (avoids mascot/cotton/prose false positives).
	pat := `(?i)(?:\\)*"(?:` + strings.Join(names, "|") + `)(?:\\)*"\s*:`
	return regexp.MustCompile(pat)
}()

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
		// Non-JSON: length + hash only — never copy unstructured body into text.
		red := redactSecrets(stdout)
		out["stdout_bytes"] = len(stdout)
		out["stdout_sha256"] = sha256Hex(red)
	}

	if strings.TrimSpace(stderr) != "" {
		// Never copy unstructured stderr diagnostics into the record.
		red := redactSecrets(stderr)
		out["stderr_bytes"] = len(stderr)
		out["stderr_sha256"] = sha256Hex(red)
		if contentHasPrivateReasoning([]byte(red)) {
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
//
// Valid JSON: parse first and walk actual object keys only (no prose regex).
// Nested JSON-in-string: recurse only when the complete trimmed string parses
// as a JSON object or array.
// Non-JSON fallback: only fully quoted keys + colon (optional backslash layers).
func contentHasPrivateReasoning(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	trimmed := bytesTrimSpace(b)
	var v any
	if err := json.Unmarshal(trimmed, &v); err == nil {
		return walkJSONForPrivateReasoning(v, 0)
	}
	// Malformed / non-JSON: require complete quoted key boundaries.
	return forbiddenQuotedKeyColon.Match(trimmed)
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func normalizeReasoningKey(k string) string {
	k = strings.TrimSpace(k)
	// Split camelCase / PascalCase so reasoningContent → reasoning_content.
	var split strings.Builder
	runes := []rune(k)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				split.WriteByte('_')
			}
		}
		split.WriteRune(r)
	}
	lk := strings.ToLower(split.String())
	var b strings.Builder
	for _, r := range lk {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '_' || r == '-' || r == ' ' || r == '.':
			// Canonical underscore form so reasoning-content matches reasoning_content.
			if b.Len() > 0 {
				b.WriteByte('_')
			}
		}
	}
	// Collapse repeated underscores.
	s := b.String()
	for strings.Contains(s, "__") {
		s = strings.ReplaceAll(s, "__", "_")
	}
	return strings.Trim(s, "_")
}

func walkJSONForPrivateReasoning(v any, depth int) bool {
	if depth > 32 {
		// Fail closed: deep nesting is treated as private-reasoning risk.
		return true
	}
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			lk := normalizeReasoningKey(k)
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
		// Only recurse when the complete string is (or unescapes to) a JSON object/array.
		// Do not regex-scan ordinary prose string values (avoids \"thought\": prose FPs).
		if len(s) < 1<<20 {
			if walkParsedJSONString(s, depth+1) {
				return true
			}
			// One JSON-string unescape layer for multiply-escaped embeds.
			// Use encoding/json (not strconv.Unquote) so valid JSON escapes like \/ work.
			// {\"thought\":\"secret\"} → {"thought":"secret"} then walk keys.
			// Prose like: the key \"thought\": is forbidden → unescapes to prose, not object.
			if unq, ok := jsonUnescapeStringLayer(s); ok {
				unq = strings.TrimSpace(unq)
				if unq != s && walkParsedJSONString(unq, depth+1) {
					return true
				}
			}
		}
	}
	return false
}

// jsonUnescapeStringLayer applies one JSON string-unescape layer to s.
// s is treated as the interior of a JSON string (already decoded once by outer parse,
// but still containing JSON escape sequences such as \" or \/).
func jsonUnescapeStringLayer(s string) (string, bool) {
	if s == "" || len(s) >= 1<<20 {
		return "", false
	}
	// Prefer wrapping as a JSON string document and decoding with encoding/json
	// so JSON-only escapes (e.g. \/) are accepted — strconv.Unquote rejects them.
	var out string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &out); err == nil {
		return out, true
	}
	// If s is already a complete quoted JSON string value, decode directly.
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out, true
		}
	}
	return "", false
}

// walkParsedJSONString returns true when s is a complete JSON object/array that
// contains a forbidden reasoning key (recursive walk).
func walkParsedJSONString(s string, depth int) bool {
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") {
		return false
	}
	var inner any
	if json.Unmarshal([]byte(s), &inner) != nil {
		return false
	}
	return walkJSONForPrivateReasoning(inner, depth)
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
	// Walk via JSON round-trip so struct tags (target_session_id, sessionId, …) are checked.
	var as any
	if err := json.Unmarshal(b, &as); err != nil {
		return fmt.Errorf("%s: privacy reparse: %w", label, err)
	}
	if containsForbiddenEvidenceShape(as) {
		return fmt.Errorf("%s: refuses unhashed identity or raw stdout fields", label)
	}
	return nil
}

func containsForbiddenEvidenceShape(v any) bool {
	return containsForbiddenEvidenceShapeDepth(v, 0)
}

func containsForbiddenEvidenceShapeDepth(v any, depth int) bool {
	if depth > 32 {
		// Fail closed on pathological nesting.
		return true
	}
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			nk := normalizeReasoningKey(k)
			switch nk {
			case "sessionid", "session_id", "requestid", "request_id", "stdout":
				// Any plaintext identity / raw stdout field name (case/format variants).
				return true
			case "target_session_id":
				// Public evidence may only carry a full SHA-256 hex (64 chars), never a UUID.
				s, ok := child.(string)
				if !ok || s == "" || !isSHA256Hex(s) {
					return true
				}
			}
			if containsForbiddenEvidenceShapeDepth(child, depth+1) {
				return true
			}
		}
	case []any:
		for _, child := range t {
			if containsForbiddenEvidenceShapeDepth(child, depth+1) {
				return true
			}
		}
	}
	return false
}

// isSHA256Hex reports a lowercase/uppercase 64-char hex digest.
func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}
