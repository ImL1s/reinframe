package classifier

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// StrictParseLimits for RawAssessment content.
const (
	MaxExplanationBytes   = 512
	MaxEvidenceIDs        = 64
	MaxEvidenceIDLen      = 128
	MaxNestedJSONDepth    = 8
	MaxJSONTokensEstimate = 4096
)

// ParseError is a typed strict-parser failure (bounded message).
type ParseError struct {
	Reason  string
	Message string
}

func (e *ParseError) Error() string {
	if e == nil {
		return "classifier: parse error"
	}
	return fmt.Sprintf("classifier: parse %s: %s", e.Reason, boundErr(e.Message))
}

func parseFail(reason, msg string) *ParseError {
	return &ParseError{Reason: reason, Message: msg}
}

// isJSONWhitespace is the closed RFC 8259 whitespace set only.
func isJSONWhitespace(b byte) bool {
	return b == 0x20 || b == 0x09 || b == 0x0A || b == 0x0D
}

// trimJSONWhitespace trims only JSON whitespace (space/tab/LF/CR).
// Does not strip NBSP, EM SPACE, or other Unicode spaces.
func trimJSONWhitespace(s []byte) []byte {
	start := 0
	for start < len(s) && isJSONWhitespace(s[start]) {
		start++
	}
	end := len(s)
	for end > start && isJSONWhitespace(s[end-1]) {
		end--
	}
	return s[start:end]
}

// hasNonJSONBoundaryWhitespace is true when leading/trailing bytes exist that are
// not JSON whitespace but would be stripped by strings/bytes.TrimSpace (e.g. NBSP).
func hasNonJSONBoundaryWhitespace(s []byte) bool {
	// Leading non-JSON whitespace that is Unicode space or other trim targets.
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRune(s[i:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if size == 1 && isJSONWhitespace(s[i]) {
			i++
			continue
		}
		// Non-ASCII space or other Unicode whitespace at boundary is rejected.
		if r != utf8.RuneError && isUnicodeSpaceNotJSON(r) {
			return true
		}
		break
	}
	// Trailing
	j := len(s)
	for j > 0 {
		r, size := utf8.DecodeLastRune(s[:j])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if size == 1 && isJSONWhitespace(s[j-1]) {
			j--
			continue
		}
		if r != utf8.RuneError && isUnicodeSpaceNotJSON(r) {
			return true
		}
		break
	}
	return false
}

func isUnicodeSpaceNotJSON(r rune) bool {
	// JSON whitespace is only SP/TAB/LF/CR. Any other Unicode space is forbidden at boundary.
	switch r {
	case 0x20, 0x09, 0x0A, 0x0D:
		return false
	}
	// Common separators that TrimSpace would strip.
	if r == 0xA0 || r == 0x2003 || r == 0x2002 || r == 0x2009 || r == 0x2028 || r == 0x2029 || r == 0xFEFF {
		return true
	}
	// General Unicode space category (Zs) and a few controls.
	if r > 0x7F {
		// Use simple ranges for Zs
		if r == 0x1680 || (r >= 0x2000 && r <= 0x200A) || r == 0x202F || r == 0x205F || r == 0x3000 {
			return true
		}
	}
	return false
}

// ParseRawAssessmentStrict parses exactly one closed RawAssessment JSON object.
// content must be the model text only (already extracted from transport).
// allowedEvidence is the set of event IDs present in the ProviderRequest input.
// Nil or empty allowlist rejects any non-empty evidence_event_ids (fail closed).
func ParseRawAssessmentStrict(content []byte, maxBytes int, allowedEvidence map[string]struct{}) (RawAssessment, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxOutputBytes
	}
	if len(content) > maxBytes {
		return RawAssessment{}, parseFail("oversized", "content exceeds max_output_bytes")
	}
	if !utf8.Valid(content) {
		return RawAssessment{}, parseFail("utf8", "invalid UTF-8")
	}
	if hasNonJSONBoundaryWhitespace(content) {
		return RawAssessment{}, parseFail("whitespace", "non-JSON boundary whitespace rejected")
	}
	trimmed := trimJSONWhitespace(content)
	if len(trimmed) == 0 {
		return RawAssessment{}, parseFail("empty", "empty content")
	}
	// Reject markdown fences and prose wrappers.
	if bytes.HasPrefix(trimmed, []byte("```")) || bytes.Contains(trimmed, []byte("```")) {
		return RawAssessment{}, parseFail("fence", "markdown fence rejected")
	}
	if trimmed[0] != '{' {
		return RawAssessment{}, parseFail("prose", "content must start with JSON object")
	}

	// Token-level scan: one object, no duplicate keys, trailing must be exactly EOF.
	if err := validateSingleJSONObject(trimmed); err != nil {
		return RawAssessment{}, err
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return RawAssessment{}, parseFail("json", "decode failed")
	}

	// Closed field allowlist — assessment content may only carry score evidence.
	allowed := map[string]struct{}{
		"schema_version":     {},
		"severity":           {},
		"reason_code":        {},
		"evidence_event_ids": {},
	}
	forbidden := []string{
		"input_tokens", "output_tokens", "cached_tokens", "cache_hit",
		"provider_request_id", "usage", "cache_read_tokens", "uncached_input_tokens",
		"model_id", "model_version", "prompt_hash", "ruleset_id", "ruleset_hash",
		"parse_status", "latency_ms",
	}
	for _, k := range forbidden {
		if _, ok := raw[k]; ok {
			return RawAssessment{}, parseFail("unknown_field", "forbidden field "+k)
		}
	}
	for k := range raw {
		if _, ok := allowed[k]; !ok {
			return RawAssessment{}, parseFail("unknown_field", "unknown field "+k)
		}
	}

	for _, req := range []string{"schema_version", "severity", "reason_code"} {
		if _, ok := raw[req]; !ok {
			return RawAssessment{}, parseFail("missing_field", "missing "+req)
		}
	}

	sv, err := decodeStrictString(raw["schema_version"])
	if err != nil {
		return RawAssessment{}, err
	}
	if sv != SchemaRawAssessment {
		return RawAssessment{}, parseFail("schema", "unsupported schema_version")
	}

	sev, err := decodeStrictInt(raw["severity"])
	if err != nil {
		return RawAssessment{}, err
	}
	if !ValidateSeverity(sev) {
		return RawAssessment{}, parseFail("severity", "severity out of range")
	}

	rc, err := decodeStrictString(raw["reason_code"])
	if err != nil {
		return RawAssessment{}, err
	}
	if !ValidateRawReasonCode(rc) {
		return RawAssessment{}, parseFail("reason_code", "unknown reason_code")
	}

	var evidence []string
	if evRaw, ok := raw["evidence_event_ids"]; ok {
		evidence, err = decodeStrictStringArray(evRaw)
		if err != nil {
			return RawAssessment{}, err
		}
		if len(evidence) > MaxEvidenceIDs {
			return RawAssessment{}, parseFail("evidence", "too many evidence ids")
		}
		// Fail closed: any non-empty evidence requires a non-nil, non-empty allowlist.
		if len(evidence) > 0 && len(allowedEvidence) == 0 {
			return RawAssessment{}, parseFail("evidence", "evidence ids require allowlist")
		}
		seen := map[string]struct{}{}
		for _, id := range evidence {
			if id == "" || len(id) > MaxEvidenceIDLen {
				return RawAssessment{}, parseFail("evidence", "malformed evidence id")
			}
			if _, dup := seen[id]; dup {
				return RawAssessment{}, parseFail("evidence", "duplicate evidence id")
			}
			seen[id] = struct{}{}
			if _, ok := allowedEvidence[id]; !ok {
				return RawAssessment{}, parseFail("evidence", "unknown evidence id")
			}
		}
	}

	return RawAssessment{
		SchemaVersion:    SchemaRawAssessment,
		Severity:         sev,
		ReasonCode:       rc,
		EvidenceEventIDs: evidence,
		ParseStatus:      ParseStatusOK,
	}, nil
}

func decodeStrictString(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return "", parseFail("type", "expected string")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", parseFail("type", "invalid string")
	}
	return s, nil
}

func decodeStrictInt(raw json.RawMessage) (int, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return 0, parseFail("type", "empty number")
	}
	s := string(raw)
	if s[0] == '"' {
		return 0, parseFail("type", "no string coercion")
	}
	if strings.ContainsAny(s, ".eE") || strings.EqualFold(s, "NaN") ||
		strings.EqualFold(s, "Infinity") || strings.EqualFold(s, "+Infinity") ||
		strings.EqualFold(s, "-Infinity") {
		return 0, parseFail("type", "severity must be integer")
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, parseFail("type", "invalid number")
	}
	i64, err := n.Int64()
	if err != nil {
		return 0, parseFail("type", "severity must be integer")
	}
	const maxInt = int64(^uint(0) >> 1)
	if i64 > maxInt || i64 < -maxInt-1 {
		return 0, parseFail("type", "integer overflow")
	}
	return int(i64), nil
}

func decodeStrictStringArray(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '[' {
		return nil, parseFail("type", "expected array")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, parseFail("type", "array decode")
	}
	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, parseFail("type", "expected array")
	}
	var out []string
	for dec.More() {
		var el json.RawMessage
		if err := dec.Decode(&el); err != nil {
			return nil, parseFail("type", "array element")
		}
		s, err := decodeStrictString(el)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	tok, err = dec.Token()
	if err != nil {
		return nil, parseFail("type", "array end")
	}
	if d, ok := tok.(json.Delim); !ok || d != ']' {
		return nil, parseFail("type", "array end")
	}
	return out, nil
}

// validateSingleJSONObject ensures one top-level object, no duplicate keys,
// and that nothing remains after the first value except EOF.
func validateSingleJSONObject(data []byte) error {
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var first any
	if err := dec.Decode(&first); err != nil {
		return parseFail("json", "decode failed")
	}
	// Second Decode must return exactly io.EOF. nil = second value;
	// syntax/token error = trailing non-JSON or malformed suffix.
	var extra any
	err := dec.Decode(&extra)
	if err == nil {
		return parseFail("json", "multiple JSON values")
	}
	if err != io.EOF {
		return parseFail("json", "trailing non-JSON content")
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return walkObjectKeys(dec, 0)
}

func walkObjectKeys(dec *json.Decoder, depth int) error {
	if depth > MaxNestedJSONDepth {
		return parseFail("json", "nesting too deep")
	}
	tok, err := dec.Token()
	if err != nil {
		return parseFail("json", "token")
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]struct{}{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return parseFail("json", "key")
			}
			key, ok := keyTok.(string)
			if !ok {
				return parseFail("json", "non-string key")
			}
			if _, dup := seen[key]; dup {
				return parseFail("duplicate_key", "duplicate key "+key)
			}
			seen[key] = struct{}{}
			if err := walkValue(dec, depth+1); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return parseFail("json", "object end")
		}
		return nil
	case '[':
		for dec.More() {
			if err := walkValue(dec, depth+1); err != nil {
				return err
			}
		}
		if _, err := dec.Token(); err != nil {
			return parseFail("json", "array end")
		}
		return nil
	default:
		return parseFail("json", "unexpected delim")
	}
}

func walkValue(dec *json.Decoder, depth int) error {
	if depth > MaxNestedJSONDepth {
		return parseFail("json", "nesting too deep")
	}
	tok, err := dec.Token()
	if err != nil {
		return parseFail("json", "value")
	}
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			seen := map[string]struct{}{}
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return parseFail("json", "key")
				}
				key, ok := keyTok.(string)
				if !ok {
					return parseFail("json", "non-string key")
				}
				if _, dup := seen[key]; dup {
					return parseFail("duplicate_key", "duplicate key "+key)
				}
				seen[key] = struct{}{}
				if err := walkValue(dec, depth+1); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return parseFail("json", "object end")
			}
			return nil
		case '[':
			for dec.More() {
				if err := walkValue(dec, depth+1); err != nil {
					return err
				}
			}
			if _, err := dec.Token(); err != nil {
				return parseFail("json", "array end")
			}
			return nil
		default:
			return parseFail("json", "unexpected delim")
		}
	}
	return nil
}

// AllowedEvidenceSet builds the evidence allowlist from ClassifierInput.
func AllowedEvidenceSet(in ClassifierInput) map[string]struct{} {
	m := make(map[string]struct{})
	for _, id := range in.RecentEventIDs {
		m[id] = struct{}{}
	}
	for _, id := range in.RelatedEventIDs {
		m[id] = struct{}{}
	}
	return m
}
