package main

// Live control disposition evaluator (#167 / #199).
//
// Schema reinframe.grok_build_live_control.v2 requires a full mandatory matrix
// for GO. Historical v1 evidence that only filled a subset must demote.

// Schema versions.
const (
	LiveControlSchemaV1 = "reinframe.grok_build_live_control.v1"
	LiveControlSchemaV2 = "reinframe.grok_build_live_control.v2"
)

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

// evaluateDisposition ranks GO / LIMITED_GO / MORE_DATA / NO_GO from scenario map.
//
// GO requires:
//   - every goMandatoryID PASS;
//   - ACP-AUTH-001 PASS;
//   - HOOK-DENY-001 DenyDirectProof;
//   - each HOOK-FAIL-* FailOpenInvoked + host_fail_open;
//   - ACP-SESSION-001 SessionCorrelated + ack_layer=session_visible;
//   - ADVICE-DEDUP-001 DedupSuppressed (true business suppression, not "host accepted twice");
//   - no soft gaps.
//
// LIMITED_GO: core paths PASS with weaker correlation or some GO-only IDs INCONCLUSIVE.
// MORE_DATA: empty map, or only partial foundation without full core PASS.
// NO_GO: any core FAIL or auth not PASS.
func evaluateDisposition(scenarios map[string]ScenarioResult) (disp string, reasons []string) {
	reasons = []string{}
	if len(scenarios) == 0 {
		return "MORE_DATA", []string{"no scenarios recorded"}
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
		if !ok || sr.Status == "" || sr.Status == "NOT_RUN" {
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
		}
	}
	if coreMissing > 0 && corePass < len(coreMandatoryIDs)/2 {
		// Too incomplete for LIMITED_GO product claim.
		if !containsStr(reasons, "incomplete core matrix") {
			reasons = append(reasons, "incomplete core matrix")
		}
		return "MORE_DATA", reasons
	}

	// Start from GO and demote.
	disp = "GO"

	for _, id := range goMandatoryIDs {
		sr, ok := scenarios[id]
		if !ok || sr.Status == "" || sr.Status == "NOT_RUN" {
			disp = demote(disp, "LIMITED_GO")
			if id == "STATIC-PERM-001" {
				// static permission optional fragment: missing → LIMITED_GO for full GO claim
				reasons = append(reasons, id+" NOT_RUN")
			} else {
				reasons = append(reasons, id+" missing")
			}
			continue
		}
		switch sr.Status {
		case "FAIL":
			// Non-core already handled; GO-only FAIL demotes hard.
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
			// correlation gates for GO
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

// closedSchemaV2 is a machine-checkable evidence contract (#199).
func closedSchemaV2() map[string]any {
	statusEnum := []string{"PASS", "FAIL", "NOT_RUN", "INCONCLUSIVE"}
	dispEnum := []string{"GO", "LIMITED_GO", "MORE_DATA", "NO_GO"}
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
			"schema_version":            map[string]any{"const": LiveControlSchemaV2},
			"provenance":                map[string]any{"type": "object"},
			"entry_gates":               map[string]any{"type": "object"},
			"trust_results":             map[string]any{"type": "object"},
			"hook_results":              map[string]any{"type": "object"},
			"hook_failure_semantics":    map[string]any{"type": "object"},
			"static_permission_results": map[string]any{"type": "object"},
			"acp_negotiation":           map[string]any{"type": "object"},
			"auth_boundary":             map[string]any{"type": "object"},
			"session_results":           map[string]any{"type": "object"},
			"advice_results":            map[string]any{"type": "object"},
			"challenge_results":         map[string]any{"type": "object"},
			"ack_layers": map[string]any{
				"type":                 "object",
				"required":             []string{"strongest_proven", "explicit_claimed"},
				"additionalProperties": true,
				"properties": map[string]any{
					"strongest_proven":  map[string]any{"type": "string"},
					"explicit_claimed":  map[string]any{"const": false},
					"source_correlated": map[string]any{"type": "boolean"},
					"note":              map[string]any{"type": "string"},
				},
			},
			"process_cleanup":      map[string]any{"type": "object"},
			"capability_manifests": map[string]any{},
			"privacy_checks":       map[string]any{"type": "object"},
			"limitations":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"scenarios":            map[string]any{"type": "object"},
			"scenario_registry":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"final_disposition":    map[string]any{"enum": dispEnum},
			"scenario_status_enum": map[string]any{"enum": statusEnum},
		},
	}
}

// validateReportV2Basics checks closed invariants without external JSON-schema libs.
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
	disp, _ := report["final_disposition"].(string)
	if disp != "GO" && disp != "LIMITED_GO" && disp != "MORE_DATA" && disp != "NO_GO" {
		errs = append(errs, "invalid final_disposition")
	}
	ack, _ := report["ack_layers"].(map[string]any)
	if ack != nil {
		if ex, ok := ack["explicit_claimed"].(bool); ok && ex {
			errs = append(errs, "explicit_claimed must be false")
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
	return errs
}
