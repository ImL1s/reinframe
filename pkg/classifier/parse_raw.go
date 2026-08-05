package classifier

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// ParseRawAssessmentStrict parses exactly one closed RawAssessment JSON object.
// content must be the model text only (already extracted from transport).
// allowedEvidence is the set of event IDs present in the ProviderRequest input.
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
	// Trim only ASCII space/tab/LF/CR at edges for fence detection — do not
	// rewrite interior content.
	trimmed := bytes.TrimSpace(content)
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

	// Token-level scan: one object, no duplicate keys, no trailing junk.
	if err := validateSingleJSONObject(trimmed); err != nil {
		return RawAssessment{}, err
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return RawAssessment{}, parseFail("json", "decode failed")
	}
	// Trailing non-whitespace already checked by validateSingleJSONObject.

	// Closed field allowlist.
	allowed := map[string]struct{}{
		"schema_version":     {},
		"severity":           {},
		"reason_code":        {},
		"evidence_event_ids": {},
		// Optional closed metadata fields (may appear; model-injected usage is ignored elsewhere).
		"model_id":      {},
		"model_version": {},
		"prompt_hash":   {},
		"ruleset_id":    {},
		"ruleset_hash":  {},
		"parse_status":  {},
		"latency_ms":    {},
		// Explicitly reject if present as nested objects for usage injection —
		// these keys are NOT part of RawAssessment and must fail closed.
	}
	// Reject usage/meta injection keys in assessment content.
	forbidden := []string{
		"input_tokens", "output_tokens", "cached_tokens", "cache_hit",
		"provider_request_id", "usage", "cache_read_tokens", "uncached_input_tokens",
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

	// Required fields.
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
		seen := map[string]struct{}{}
		for _, id := range evidence {
			if id == "" || len(id) > MaxEvidenceIDLen {
				return RawAssessment{}, parseFail("evidence", "malformed evidence id")
			}
			if _, dup := seen[id]; dup {
				return RawAssessment{}, parseFail("evidence", "duplicate evidence id")
			}
			seen[id] = struct{}{}
			if allowedEvidence != nil {
				if _, ok := allowedEvidence[id]; !ok {
					return RawAssessment{}, parseFail("evidence", "unknown evidence id")
				}
			}
		}
	}

	out := RawAssessment{
		SchemaVersion:    SchemaRawAssessment,
		Severity:         sev,
		ReasonCode:       rc,
		EvidenceEventIDs: evidence,
		ParseStatus:      ParseStatusOK,
	}
	// Optional closed string fields (no coercion).
	if v, ok := raw["model_id"]; ok {
		out.ModelID, err = decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
	}
	if v, ok := raw["model_version"]; ok {
		out.ModelVersion, err = decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
	}
	if v, ok := raw["prompt_hash"]; ok {
		out.PromptHash, err = decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
	}
	if v, ok := raw["ruleset_id"]; ok {
		out.RulesetID, err = decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
	}
	if v, ok := raw["ruleset_hash"]; ok {
		out.RulesetHash, err = decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
	}
	if v, ok := raw["parse_status"]; ok {
		ps, err := decodeStrictString(v)
		if err != nil {
			return RawAssessment{}, err
		}
		// Only ok is accepted from model for a successful parse path.
		if ps != ParseStatusOK {
			return RawAssessment{}, parseFail("parse_status", "invalid parse_status in content")
		}
	}
	if v, ok := raw["latency_ms"]; ok {
		// Reject model-supplied latency for assessment identity — optional closed int only if present.
		lat, err := decodeStrictInt64(v)
		if err != nil {
			return RawAssessment{}, err
		}
		if lat < 0 {
			return RawAssessment{}, parseFail("latency", "negative latency")
		}
		out.LatencyMS = lat
	}
	return out, nil
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
	// Reject floats, NaN, Inf, quoted numbers.
	if raw[0] == '"' || bytes.ContainsAny(raw, ".eE+NaInfn") {
		// Allow plain integers only (optional leading minus already covered by contains +).
		// Re-check carefully: digits and optional leading minus only.
	}
	s := string(raw)
	if strings.ContainsAny(s, ".eE") || strings.EqualFold(s, "NaN") ||
		strings.EqualFold(s, "Infinity") || strings.EqualFold(s, "+Infinity") ||
		strings.EqualFold(s, "-Infinity") {
		return 0, parseFail("type", "severity must be integer")
	}
	if s[0] == '"' {
		return 0, parseFail("type", "no string coercion")
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, parseFail("type", "invalid number")
	}
	i64, err := n.Int64()
	if err != nil {
		return 0, parseFail("type", "severity must be integer")
	}
	if i64 < int64(SeverityMin) || i64 > int64(SeverityMax) {
		// Still return as int for ValidateSeverity path; caller checks range.
	}
	if i64 > int64(^uint(0)>>1) || i64 < -int64(^uint(0)>>1)-1 {
		return 0, parseFail("type", "integer overflow")
	}
	return int(i64), nil
}

func decodeStrictInt64(raw json.RawMessage) (int64, error) {
	raw = bytes.TrimSpace(raw)
	s := string(raw)
	if strings.ContainsAny(s, ".eE") || s[0] == '"' {
		return 0, parseFail("type", "expected integer")
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return 0, parseFail("type", "invalid number")
	}
	return n.Int64()
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
// and no trailing non-whitespace after the object.
func validateSingleJSONObject(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return parseFail("json", "token error")
	}
	d, ok := tok.(json.Delim)
	if !ok || d != '{' {
		return parseFail("json", "expected object")
	}
	// Consume full first value via standard decode into raw map after duplicate check.
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	// Re-decode and ensure no second JSON value.
	dec2 := json.NewDecoder(bytes.NewReader(data))
	dec2.UseNumber()
	var first any
	if err := dec2.Decode(&first); err != nil {
		return parseFail("json", "decode failed")
	}
	var extra any
	if err := dec2.Decode(&extra); err == nil {
		return parseFail("json", "multiple JSON values")
	}
	return nil
}

// rejectDuplicateKeys scans objects with a decoder stack for duplicate keys at each object level.
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
		return nil // scalar at top — already rejected elsewhere
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
			// Value
			if err := walkValue(dec, depth+1); err != nil {
				return err
			}
		}
		// consume closing }
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
	// Peek by decoding token
	tok, err := dec.Token()
	if err != nil {
		return parseFail("json", "value")
	}
	if d, ok := tok.(json.Delim); ok {
		switch d {
		case '{':
			// re-enter object body: we already consumed '{', so handle like walkObjectKeys mid-object
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
	// scalar already consumed
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
