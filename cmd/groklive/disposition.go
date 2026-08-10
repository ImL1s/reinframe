package main

import "fmt"

// Live control disposition evaluator (#167 / #199 / #209).
//
// Schema reinframe.grok_build_live_control.v2 requires a full mandatory matrix
// for GO. Historical v1 evidence that only filled a subset must demote.
// Unknown scenario statuses never yield GO or LIMITED_GO (#209).

// Schema versions.
const (
	LiveControlSchemaV1 = "reinframe.grok_build_live_control.v1"
	LiveControlSchemaV2 = "reinframe.grok_build_live_control.v2"
)

// Closed scenario status enum (#209).
var closedScenarioStatuses = map[string]struct{}{
	"PASS":         {},
	"FAIL":         {},
	"NOT_RUN":      {},
	"INCONCLUSIVE": {},
}

// goMandatoryIDs must all PASS (and pass correlation gates) for disposition GO.
var goMandatoryIDs = []string{
	"TRUST-001", "TRUST-STALE-001", "TRUST-RESTORE-001",
	"HOOK-ALLOW-001", "HOOK-DENY-001", "HOOK-MAP-001", "HOOK-UNINSTALL-001",
	"HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004",
	"ACP-INIT-001", "ACP-AUTH-001", "ACP-SESSION-001", "ACP-CLEANUP-001",
	"ADVICE-DEDUP-001", "CHALLENGE-001",
	"STATIC-PERM-001",
}

// coreMandatoryIDs: FAIL → NO_GO; INCONCLUSIVE/missing → at best LIMITED_GO or MORE_DATA.
var coreMandatoryIDs = []string{
	"HOOK-ALLOW-001", "HOOK-DENY-001",
	"HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004",
	"ACP-INIT-001", "ACP-AUTH-001", "ACP-SESSION-001", "ACP-CLEANUP-001",
}

// validScenarioStatus reports whether status is in the closed enum.
func validScenarioStatus(status string) bool {
	_, ok := closedScenarioStatuses[status]
	return ok
}

// evaluateDisposition ranks GO / LIMITED_GO / MORE_DATA / NO_GO from scenario map.
//
// GO requires:
//   - every goMandatoryID PASS with closed enum status only;
//   - ACP-AUTH-001 PASS;
//   - HOOK-DENY-001 DenyDirectProof;
//   - each HOOK-FAIL-* FailOpenInvoked + host_fail_open;
//   - ACP-SESSION-001 SessionCorrelated + ack_layer=session_visible;
//   - ADVICE-DEDUP-001 DedupSuppressed;
//   - no soft gaps.
//
// Unknown/non-enum statuses (PASSX, UNKNOWN, pass, empty on present key) → NO_GO.
// LIMITED_GO: core paths PASS with weaker correlation or some GO-only IDs INCONCLUSIVE.
// MORE_DATA: empty map, or only partial foundation without full core PASS.
// NO_GO: any core FAIL, auth not PASS, or any invalid status.
func evaluateDisposition(scenarios map[string]ScenarioResult) (disp string, reasons []string) {
	reasons = []string{}
	if len(scenarios) == 0 {
		return "MORE_DATA", []string{"no scenarios recorded"}
	}

	// Closed status enum + non-empty id==key gate (#209 residuals).
	for id, sr := range scenarios {
		// Empty status on a present entry is invalid (distinct from missing key).
		if sr.Status == "" || !validScenarioStatus(sr.Status) {
			return "NO_GO", []string{fmt.Sprintf("%s invalid status %q", id, sr.Status)}
		}
		// Committed schema requires id minLength:1 and equal to map key — empty id is not GO.
		if sr.ID == "" {
			return "NO_GO", []string{fmt.Sprintf("%s missing embedded id", id)}
		}
		if sr.ID != id {
			return "NO_GO", []string{fmt.Sprintf("scenario key %s mismatches embedded id %s", id, sr.ID)}
		}
	}

	// Auth hard gate.
	if sr, ok := scenarios["ACP-AUTH-001"]; !ok || sr.Status != "PASS" {
		return "NO_GO", []string{"ACP-AUTH-001 not PASS"}
	}

	// Core FAIL → NO_GO.
	for _, id := range coreMandatoryIDs {
		sr, ok := scenarios[id]
		if ok && sr.Status == "FAIL" {
			return "NO_GO", []string{id + " FAIL"}
		}
	}

	// Assess core completeness.
	coreMissing := 0
	coreInconclusive := 0
	corePass := 0
	for _, id := range coreMandatoryIDs {
		sr, ok := scenarios[id]
		if !ok || sr.Status == "NOT_RUN" {
			coreMissing++
			reasons = append(reasons, id+" missing")
			continue
		}
		switch sr.Status {
		case "PASS":
			corePass++
		case "INCONCLUSIVE":
			coreInconclusive++
			reasons = append(reasons, id+" INCONCLUSIVE")
		case "FAIL":
			// already handled
		default:
			// Unreachable after enum gate; belt.
			return "NO_GO", append(reasons, id+" invalid status "+sr.Status)
		}
	}
	if coreMissing > 0 && corePass < len(coreMandatoryIDs)/2 {
		if !containsStr(reasons, "incomplete core matrix") {
			reasons = append(reasons, "incomplete core matrix")
		}
		return "MORE_DATA", reasons
	}

	// Start from GO and demote.
	disp = "GO"

	for _, id := range goMandatoryIDs {
		sr, ok := scenarios[id]
		if !ok || sr.Status == "NOT_RUN" {
			disp = demote(disp, "LIMITED_GO")
			if id == "STATIC-PERM-001" {
				reasons = append(reasons, id+" NOT_RUN")
			} else {
				reasons = append(reasons, id+" missing")
			}
			continue
		}
		switch sr.Status {
		case "FAIL":
			if isCore(id) {
				return "NO_GO", append(reasons, id+" FAIL")
			}
			disp = demote(disp, "LIMITED_GO")
			reasons = append(reasons, id+" FAIL")
		case "INCONCLUSIVE":
			disp = demote(disp, "LIMITED_GO")
			if !containsStr(reasons, id+" INCONCLUSIVE") {
				reasons = append(reasons, id+" INCONCLUSIVE")
			}
		case "PASS":
			if id == "HOOK-DENY-001" && !sr.DenyDirectProof {
				disp = demote(disp, "LIMITED_GO")
				reasons = append(reasons, "HOOK-DENY-001 lacks deny_direct_proof")
			}
			if stringsHasPrefix(id, "HOOK-FAIL-") {
				if !sr.FailOpenInvoked || sr.HostOutcome != "host_fail_open" {
					disp = demote(disp, "LIMITED_GO")
					reasons = append(reasons, id+" lacks fail_open_invoked proof")
				}
			}
			if id == "ACP-SESSION-001" {
				if !sr.SessionCorrelated || sr.ACKLayer != "session_visible" {
					disp = demote(disp, "LIMITED_GO")
					reasons = append(reasons, "ACP-SESSION-001 not source-correlated session_visible")
				}
			}
			if id == "ADVICE-DEDUP-001" && !sr.DedupSuppressed {
				disp = demote(disp, "LIMITED_GO")
				reasons = append(reasons, "ADVICE-DEDUP-001 no durable/business suppression proven")
			}
		case "NOT_RUN":
			// handled above
		default:
			return "NO_GO", append(reasons, id+" invalid status "+sr.Status)
		}
	}

	// loadSession negotiated but failed/inconclusive cannot stay full GO.
	if sr, ok := scenarios["ACP-OPTIONAL-001"]; ok {
		if sr.Status == "INCONCLUSIVE" || sr.Status == "FAIL" {
			disp = demote(disp, "LIMITED_GO")
			if !containsStr(reasons, "ACP-OPTIONAL-001 "+sr.Status) {
				reasons = append(reasons, "ACP-OPTIONAL-001 "+sr.Status)
			}
		}
	}

	if coreInconclusive > 0 && disp == "GO" {
		disp = "LIMITED_GO"
	}

	// Surface remaining INCONCLUSIVE for operators.
	for id, sr := range scenarios {
		if sr.Status == "INCONCLUSIVE" && !containsStr(reasons, id+" INCONCLUSIVE") {
			reasons = append(reasons, id+" INCONCLUSIVE")
		}
	}

	return disp, reasons
}

func isCore(id string) bool {
	for _, c := range coreMandatoryIDs {
		if c == id {
			return true
		}
	}
	return false
}

func demote(cur, floor string) string {
	rank := map[string]int{"GO": 3, "LIMITED_GO": 2, "MORE_DATA": 1, "NO_GO": 0}
	if rank[floor] < rank[cur] {
		return floor
	}
	return cur
}

func stringsHasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func scenarioResultSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"id", "status"},
		"properties": map[string]any{
			"id":                  map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"status":              map[string]any{"enum": []string{"PASS", "FAIL", "NOT_RUN", "INCONCLUSIVE"}},
			"detail":              map[string]any{"type": "string", "maxLength": 2000},
			"tool_name":           map[string]any{"type": "string", "maxLength": 256},
			"ack_layer":           map[string]any{"type": "string", "maxLength": 64},
			"host_outcome":        map[string]any{"type": "string", "maxLength": 256},
			"at":                  map[string]any{"type": "string", "maxLength": 64},
			"deny_direct_proof":   map[string]any{"type": "boolean"},
			"fail_open_invoked":   map[string]any{"type": "boolean"},
			"session_correlated":  map[string]any{"type": "boolean"},
			"intervention_id":     map[string]any{"type": "string", "maxLength": 256},
			"target_session_id":   map[string]any{"type": "string", "maxLength": 256},
			"dedup_suppressed":    map[string]any{"type": "boolean"},
		},
	}
}

// foundationManifestSchema is the closed pre/post handshake object (#218).
func foundationManifestSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"profile", "protocol_version", "cap_event_stream", "cap_tool_inspection",
			"cap_advice_delivery", "cap_diff_inspection", "cap_pause", "cap_cancel",
			"cap_resume", "cap_intervention_ack", "explicit_ack", "load_session",
			"negotiated_level", "honesty_note",
		},
		"properties": map[string]any{
			"profile":              map[string]any{"type": "string", "const": "reinframe.grok_build_acp.v1"},
			"protocol_version":     map[string]any{"type": "integer", "const": 1},
			"cap_event_stream":     map[string]any{"type": "boolean"},
			"cap_tool_inspection":  map[string]any{"type": "boolean"},
			"cap_advice_delivery":  map[string]any{"type": "boolean"},
			"cap_diff_inspection":  map[string]any{"type": "boolean"},
			"cap_pause":            map[string]any{"type": "boolean"},
			"cap_cancel":           map[string]any{"type": "boolean"},
			"cap_resume":           map[string]any{"type": "boolean"},
			"cap_intervention_ack": map[string]any{"type": "boolean"},
			"explicit_ack":         map[string]any{"type": "boolean"},
			"load_session":         map[string]any{"type": "boolean"},
			"auth_methods": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			},
			"negotiated_level": map[string]any{"type": "integer", "minimum": -1, "maximum": 3},
			"honesty_note":     map[string]any{"type": "string", "minLength": 1},
		},
	}
}

// closedSchemaV2 is the machine-checkable evidence contract (#199/#209).
// Nested objects use additionalProperties:false; scenario status is a closed enum.
func closedSchemaV2() map[string]any {
	statusEnum := []string{"PASS", "FAIL", "NOT_RUN", "INCONCLUSIVE"}
	dispEnum := []string{"GO", "LIMITED_GO", "MORE_DATA", "NO_GO"}
	sr := scenarioResultSchema()
	// Group maps: values are ScenarioResult objects (closed).
	scenarioMap := map[string]any{
		"type":                 "object",
		"additionalProperties": sr,
	}
	closedObj := func(props map[string]any, required []string) map[string]any {
		m := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           props,
		}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  LiveControlSchemaV2,
		"title":                "Reinframe Grok Build live control evidence v2",
		"type":                 "object",
		"additionalProperties": false,
		"required": []string{
			"schema_version", "provenance", "entry_gates", "scenarios",
			"ack_layers", "privacy_checks", "limitations", "final_disposition",
			"scenario_registry",
		},
		"properties": map[string]any{
			"schema_version": map[string]any{"const": LiveControlSchemaV2},
			"provenance": closedObj(map[string]any{
				"issue":                 map[string]any{"type": "integer"},
				"generated_at":          map[string]any{"type": "string"},
				"goos":                  map[string]any{"type": "string"},
				"goarch":                map[string]any{"type": "string"},
				"grok_version":          map[string]any{"type": "string"},
				"grok_version_full":     map[string]any{"type": "string"},
				"reinframe_commit":      map[string]any{"type": "string"},
				"reinframe_dirty":       map[string]any{"type": "boolean"},
				"reinframe_commit_src":  map[string]any{"type": "string"},
				"starting_main_sha":     map[string]any{"type": "string"},
				"main_tip_note":         map[string]any{"type": "string"},
				"harness":               map[string]any{"type": "string"},
				"evidence_binding_note": map[string]any{"type": "string"},
				"schema_note":           map[string]any{"type": "string"},
			}, []string{"issue", "generated_at", "goos", "harness"}),
			"entry_gates": closedObj(map[string]any{
				"live_flag_required": map[string]any{"type": "boolean"},
				"auth_json_read":     map[string]any{"type": "boolean"},
				"credential_print":   map[string]any{"type": "boolean"},
			}, []string{"live_flag_required", "auth_json_read", "credential_print"}),
			"trust_results":             scenarioMap,
			"hook_results":              scenarioMap,
			"hook_failure_semantics":    scenarioMap,
			"static_permission_results": scenarioMap,
			"acp_negotiation":           scenarioMap,
			"auth_boundary":             scenarioMap,
			"session_results":           scenarioMap,
			"advice_results":            scenarioMap,
			"challenge_results":         scenarioMap,
			"ack_layers": closedObj(map[string]any{
				"strongest_proven":  map[string]any{"type": "string"},
				"explicit_claimed":  map[string]any{"const": false},
				"source_correlated": map[string]any{"type": "boolean"},
				"note":              map[string]any{"type": "string"},
			}, []string{"strongest_proven", "explicit_claimed"}),
			"process_cleanup": scenarioMap,
			// capability_manifests: closed ACP foundation shape (#215/#218).
			"capability_manifests": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"required":             []string{"pre_handshake", "post_handshake", "auth_methods", "caps_digest"},
				"properties": map[string]any{
					"pre_handshake":  foundationManifestSchema(),
					"post_handshake": foundationManifestSchema(),
					"auth_methods": map[string]any{
						"type":     "array",
						"minItems": 1,
						"maxItems": 16,
						"items": map[string]any{
							"oneOf": []any{
								map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
								map[string]any{
									"type":                 "object",
									"additionalProperties": false,
									"required":             []string{"id"},
									"properties": map[string]any{
										"id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
									},
								},
							},
						},
					},
					// Canonical digest: load=… pause=… cancel=… resume=… tool=… diff=…
					"caps_digest": map[string]any{
						"type":    "string",
						"pattern": `^load=(true|false) pause=(true|false) cancel=(true|false) resume=(true|false) tool=(true|false) diff=(true|false)$`,
					},
				},
			},
			// privacy_checks: closed keys matching complete-or-fail scan (#215).
			"privacy_checks": map[string]any{
				"type":                 "object",
				"additionalProperties": false,
				"properties": map[string]any{
					"method":                    map[string]any{"type": "string"},
					"complete":                  map[string]any{"type": "boolean"},
					"files_seen":                map[string]any{"type": "integer"},
					"files_scanned":             map[string]any{"type": "integer"},
					"files_skipped":             map[string]any{"type": "integer"},
					"bytes_scanned":             map[string]any{"type": "integer"},
					"failure_classes":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"auth_json_read":            map[string]any{"type": "boolean"},
					"auth_json_path_seen_in_honesty_notes_only": map[string]any{"type": "boolean"},
					"auth_json_path_leak_suspected":             map[string]any{"type": "boolean"},
					"token_fields_in_auth_envelope":             map[string]any{"type": "boolean"},
					"raw_thoughts_stored":                       map[string]any{"type": "boolean"},
					"secret_pattern_hits":                       map[string]any{"type": "integer"},
					"error":                                     map[string]any{"type": "string"},
				},
			},
			"limitations": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"scenarios": map[string]any{
				"type":                 "object",
				"additionalProperties": sr,
			},
			"scenario_registry": map[string]any{
				"type":     "array",
				"items":    map[string]any{"type": "string"},
				"minItems": 1,
			},
			"final_disposition":    map[string]any{"enum": dispEnum},
			"scenario_status_enum": map[string]any{"enum": statusEnum},
		},
	}
}

// validateReportV2Basics checks closed invariants without external JSON-schema libs.
// Semantic checks complement schema validation (#209).
func validateReportV2Basics(report map[string]any, scenarios map[string]ScenarioResult) []string {
	var errs []string
	if report["schema_version"] != LiveControlSchemaV2 {
		errs = append(errs, "schema_version must be v2")
	}
	// Reject unknown top-level keys (closed schema).
	allowed := map[string]struct{}{
		"schema_version": {}, "provenance": {}, "entry_gates": {}, "trust_results": {},
		"hook_results": {}, "hook_failure_semantics": {}, "static_permission_results": {},
		"acp_negotiation": {}, "auth_boundary": {}, "session_results": {}, "advice_results": {},
		"challenge_results": {}, "ack_layers": {}, "process_cleanup": {}, "capability_manifests": {},
		"privacy_checks": {}, "limitations": {}, "scenarios": {}, "scenario_registry": {},
		"final_disposition": {}, "scenario_status_enum": {},
	}
	for k := range report {
		if _, ok := allowed[k]; !ok {
			errs = append(errs, "unknown top-level field: "+k)
		}
	}
	for _, req := range []string{"schema_version", "provenance", "entry_gates", "scenarios", "ack_layers", "privacy_checks", "limitations", "final_disposition", "scenario_registry"} {
		if _, ok := report[req]; !ok {
			errs = append(errs, "missing required field: "+req)
		}
	}

	// Nested closed checks: provenance/entry_gates unknown keys.
	if prov, ok := report["provenance"].(map[string]any); ok {
		provAllowed := map[string]struct{}{
			"issue": {}, "generated_at": {}, "goos": {}, "goarch": {}, "grok_version": {},
			"grok_version_full": {}, "reinframe_commit": {}, "reinframe_dirty": {}, "reinframe_commit_src": {},
			"starting_main_sha": {},
			"main_tip_note": {}, "harness": {}, "evidence_binding_note": {}, "schema_note": {},
		}
		for k := range prov {
			if _, ok := provAllowed[k]; !ok {
				errs = append(errs, "unknown provenance field: "+k)
			}
		}
		for _, req := range []string{"issue", "generated_at", "goos", "harness"} {
			if _, ok := prov[req]; !ok {
				errs = append(errs, "missing provenance field: "+req)
			}
		}
	} else {
		errs = append(errs, "provenance must be object")
	}
	if eg, ok := report["entry_gates"].(map[string]any); ok {
		egAllowed := map[string]struct{}{"live_flag_required": {}, "auth_json_read": {}, "credential_print": {}}
		for k := range eg {
			if _, ok := egAllowed[k]; !ok {
				errs = append(errs, "unknown entry_gates field: "+k)
			}
		}
	}

	// Scenario status enum + key/id consistency (id required minLength 1).
	for id, sr := range scenarios {
		if !validScenarioStatus(sr.Status) {
			errs = append(errs, fmt.Sprintf("scenario %s invalid status %q", id, sr.Status))
		}
		if sr.ID == "" {
			errs = append(errs, fmt.Sprintf("scenario %s missing embedded id", id))
		} else if sr.ID != id {
			errs = append(errs, fmt.Sprintf("scenario key %s mismatches id %s", id, sr.ID))
		}
	}

	// Registry uniqueness + coverage of goMandatoryIDs when disposition is GO.
	if reg, ok := report["scenario_registry"].([]string); ok {
		seen := map[string]struct{}{}
		for _, id := range reg {
			if _, dup := seen[id]; dup {
				errs = append(errs, "duplicate scenario_registry entry: "+id)
			}
			seen[id] = struct{}{}
		}
	} else if regAny, ok := report["scenario_registry"].([]any); ok {
		seen := map[string]struct{}{}
		for _, v := range regAny {
			id, _ := v.(string)
			if id == "" {
				errs = append(errs, "empty scenario_registry entry")
				continue
			}
			if _, dup := seen[id]; dup {
				errs = append(errs, "duplicate scenario_registry entry: "+id)
			}
			seen[id] = struct{}{}
		}
	}

	disp, _ := report["final_disposition"].(string)
	if disp != "GO" && disp != "LIMITED_GO" && disp != "MORE_DATA" && disp != "NO_GO" {
		errs = append(errs, "invalid final_disposition")
	}
	ack, _ := report["ack_layers"].(map[string]any)
	if ack != nil {
		if ex, ok := ack["explicit_claimed"].(bool); ok && ex {
			errs = append(errs, "explicit_claimed must be false")
		}
		// source_correlated=false cannot claim session_visible as strongest for product GO.
		if sc, ok := ack["source_correlated"].(bool); ok && !sc {
			if sp, _ := ack["strongest_proven"].(string); sp == "session_visible" && disp == "GO" {
				errs = append(errs, "GO forbids strongest_proven=session_visible when source_correlated=false")
			}
		}
		ackAllowed := map[string]struct{}{
			"strongest_proven": {}, "explicit_claimed": {}, "source_correlated": {}, "note": {},
		}
		for k := range ack {
			if _, ok := ackAllowed[k]; !ok {
				errs = append(errs, "unknown ack_layers field: "+k)
			}
		}
	}
	// Recompute disposition and require match.
	want, _ := evaluateDisposition(scenarios)
	if disp != want {
		errs = append(errs, "final_disposition mismatches evaluateDisposition want="+want+" got="+disp)
	}
	// GO forbidden without correlation proofs.
	if disp == "GO" {
		if sr, ok := scenarios["HOOK-DENY-001"]; !ok || !sr.DenyDirectProof {
			errs = append(errs, "GO requires HOOK-DENY deny_direct_proof")
		}
		if sr, ok := scenarios["ACP-SESSION-001"]; !ok || !sr.SessionCorrelated {
			errs = append(errs, "GO requires ACP-SESSION session_correlated")
		}
		for _, id := range []string{"HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004"} {
			if sr, ok := scenarios[id]; !ok || !sr.FailOpenInvoked {
				errs = append(errs, "GO requires "+id+" fail_open_invoked")
			}
		}
	}
	// Never allow GO/LIMITED_GO when any status is invalid (belt after recompute).
	if disp == "GO" || disp == "LIMITED_GO" {
		for id, sr := range scenarios {
			if !validScenarioStatus(sr.Status) {
				errs = append(errs, "GO/LIMITED_GO forbidden with invalid status on "+id)
			}
		}
	}
	return errs
}
