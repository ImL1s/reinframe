package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

func fullGOScenarios() map[string]ScenarioResult {
	m := map[string]ScenarioResult{}
	pass := func(id string) ScenarioResult {
		return ScenarioResult{ID: id, Status: "PASS"}
	}
	for _, id := range goMandatoryIDs {
		m[id] = pass(id)
	}
	// Correlation proofs required for GO.
	d := m["HOOK-DENY-001"]
	d.DenyDirectProof = true
	d.HostOutcome = "enforced_deny"
	m["HOOK-DENY-001"] = d
	for _, id := range []string{"HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004"} {
		f := m[id]
		f.FailOpenInvoked = true
		f.HostOutcome = "host_fail_open"
		m[id] = f
	}
	s := m["ACP-SESSION-001"]
	s.SessionCorrelated = true
	s.ACKLayer = "session_visible"
	m["ACP-SESSION-001"] = s
	a := m["ADVICE-DEDUP-001"]
	a.DedupSuppressed = true
	m["ADVICE-DEDUP-001"] = a
	return m
}

func TestEvaluateDisposition_V2_FullGO(t *testing.T) {
	t.Parallel()
	disp, reasons := evaluateDisposition(fullGOScenarios())
	if disp != "GO" {
		t.Fatalf("want GO got %s reasons=%v", disp, reasons)
	}
}

func TestEvaluateDisposition_V2_HistoricalMatrixNotGO(t *testing.T) {
	t.Parallel()
	// Shape of historical #167 v1 evidence: core PASS but weak correlation + missing trust/static.
	hist := map[string]ScenarioResult{
		"HOOK-ALLOW-001":   {ID: "HOOK-ALLOW-001", Status: "PASS"},
		"HOOK-DENY-001":    {ID: "HOOK-DENY-001", Status: "PASS", HostOutcome: "side_effect_absent_with_pretool_invoke", DenyDirectProof: false},
		"HOOK-FAIL-001":    {ID: "HOOK-FAIL-001", Status: "PASS", HostOutcome: "host_fail_open", FailOpenInvoked: false},
		"HOOK-FAIL-002":    {ID: "HOOK-FAIL-002", Status: "PASS", HostOutcome: "host_fail_open", FailOpenInvoked: false},
		"HOOK-FAIL-003":    {ID: "HOOK-FAIL-003", Status: "PASS", HostOutcome: "host_fail_open", FailOpenInvoked: false},
		"HOOK-FAIL-004":    {ID: "HOOK-FAIL-004", Status: "PASS", HostOutcome: "host_fail_open", FailOpenInvoked: false},
		"ACP-INIT-001":     {ID: "ACP-INIT-001", Status: "PASS"},
		"ACP-AUTH-001":     {ID: "ACP-AUTH-001", Status: "PASS"},
		"ACP-SESSION-001":  {ID: "ACP-SESSION-001", Status: "PASS", ACKLayer: "session_visible", SessionCorrelated: false},
		"ACP-CLEANUP-001":  {ID: "ACP-CLEANUP-001", Status: "PASS"},
		"TRUST-STALE-001":  {ID: "TRUST-STALE-001", Status: "INCONCLUSIVE"},
		"ACP-OPTIONAL-001": {ID: "ACP-OPTIONAL-001", Status: "INCONCLUSIVE"},
	}
	disp, reasons := evaluateDisposition(hist)
	if disp == "GO" {
		t.Fatalf("historical weak matrix must not be GO; reasons=%v", reasons)
	}
	if disp != "LIMITED_GO" && disp != "MORE_DATA" {
		t.Fatalf("want LIMITED_GO or MORE_DATA got %s %v", disp, reasons)
	}
}

func TestEvaluateDisposition_V2_WeakDenyDemotes(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	d := m["HOOK-DENY-001"]
	d.DenyDirectProof = false
	d.HostOutcome = "side_effect_absent_with_pretool_invoke"
	m["HOOK-DENY-001"] = d
	disp, reasons := evaluateDisposition(m)
	if disp == "GO" {
		t.Fatalf("weak deny must demote GO; %v", reasons)
	}
}

func TestEvaluateDisposition_V2_UncorrelatedSessionDemotes(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	s := m["ACP-SESSION-001"]
	s.SessionCorrelated = false
	m["ACP-SESSION-001"] = s
	disp, reasons := evaluateDisposition(m)
	if disp == "GO" {
		t.Fatalf("uncorrelated session must demote; %v", reasons)
	}
}

func TestEvaluateDisposition_V2_MissingTrustDemotes(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	delete(m, "TRUST-STALE-001")
	disp, reasons := evaluateDisposition(m)
	if disp == "GO" {
		t.Fatalf("missing TRUST-STALE must demote; %v", reasons)
	}
}

func TestEvaluateDisposition_V2_AuthFailNO_GO(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	m["ACP-AUTH-001"] = ScenarioResult{ID: "ACP-AUTH-001", Status: "FAIL"}
	disp, _ := evaluateDisposition(m)
	if disp != "NO_GO" {
		t.Fatalf("want NO_GO got %s", disp)
	}
}

func TestEvaluateDisposition_V2_EmptyMoreData(t *testing.T) {
	t.Parallel()
	disp, _ := evaluateDisposition(nil)
	if disp != "MORE_DATA" {
		t.Fatalf("got %s", disp)
	}
}

func TestValidateReportV2Basics_RejectsFalseGO(t *testing.T) {
	t.Parallel()
	hist := map[string]ScenarioResult{
		"HOOK-ALLOW-001":  {Status: "PASS"},
		"HOOK-DENY-001":   {Status: "PASS"},
		"HOOK-FAIL-001":   {Status: "PASS"},
		"HOOK-FAIL-002":   {Status: "PASS"},
		"HOOK-FAIL-003":   {Status: "PASS"},
		"HOOK-FAIL-004":   {Status: "PASS"},
		"ACP-INIT-001":    {Status: "PASS"},
		"ACP-AUTH-001":    {Status: "PASS"},
		"ACP-SESSION-001": {Status: "PASS"},
		"ACP-CLEANUP-001": {Status: "PASS"},
	}
	disp, _ := evaluateDisposition(hist)
	report := map[string]any{
		"schema_version":    LiveControlSchemaV2,
		"final_disposition": "GO", // deliberately wrong
		"ack_layers":        map[string]any{"strongest_proven": "session_visible", "explicit_claimed": false},
	}
	errs := validateReportV2Basics(report, hist)
	if len(errs) == 0 {
		t.Fatal("expected validation errors for false GO")
	}
	_ = disp
}

func TestClosedSchemaV2_ForbidsAdditionalPropsFlag(t *testing.T) {
	t.Parallel()
	s := closedSchemaV2()
	if s["additionalProperties"] != false {
		t.Fatalf("v2 schema must set additionalProperties=false, got %v", s["additionalProperties"])
	}
	if s["$id"] != LiveControlSchemaV2 {
		t.Fatalf("id=%v", s["$id"])
	}
	req, _ := s["required"].([]string)
	if len(req) < 5 {
		t.Fatalf("required fields too few: %v", req)
	}
	// Nested ack_layers must be closed (#209).
	props, _ := s["properties"].(map[string]any)
	ack, _ := props["ack_layers"].(map[string]any)
	if ack["additionalProperties"] != false {
		t.Fatalf("ack_layers must set additionalProperties=false, got %v", ack["additionalProperties"])
	}
	sc, _ := props["scenarios"].(map[string]any)
	if sc["additionalProperties"] == true {
		t.Fatal("scenarios must not allow unconstrained additionalProperties")
	}
}

// TestEvaluateDisposition_V2_UnknownStatusNeverGO: illegal statuses cannot yield GO/LIMITED_GO (#209).
func TestEvaluateDisposition_V2_UnknownStatusNeverGO(t *testing.T) {
	t.Parallel()
	for _, bad := range []string{"PASSX", "UNKNOWN", "pass", "Pass", "ok", " "} {
		m := fullGOScenarios()
		m["HOOK-ALLOW-001"] = ScenarioResult{ID: "HOOK-ALLOW-001", Status: bad}
		disp, reasons := evaluateDisposition(m)
		if disp == "GO" || disp == "LIMITED_GO" {
			t.Fatalf("status %q must not yield GO/LIMITED_GO; got %s reasons=%v", bad, disp, reasons)
		}
		if disp != "NO_GO" {
			t.Fatalf("status %q want NO_GO got %s %v", bad, disp, reasons)
		}
	}
}

func TestEvaluateDisposition_V2_EmptyStatusNO_GO(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	m["HOOK-ALLOW-001"] = ScenarioResult{ID: "HOOK-ALLOW-001", Status: ""}
	disp, _ := evaluateDisposition(m)
	if disp != "NO_GO" {
		t.Fatalf("empty status want NO_GO got %s", disp)
	}
}

func TestEvaluateDisposition_V2_KeyIDMismatchNO_GO(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	m["HOOK-ALLOW-001"] = ScenarioResult{ID: "WRONG-ID", Status: "PASS"}
	disp, reasons := evaluateDisposition(m)
	if disp != "NO_GO" {
		t.Fatalf("key/id mismatch want NO_GO got %s %v", disp, reasons)
	}
}

func TestValidateReportV2Basics_RejectsNestedUnknownProvenance(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	disp, _ := evaluateDisposition(m)
	report := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue":        167,
			"generated_at": "t",
			"goos":         "darwin",
			"harness":      "cmd/groklive",
			"evil_extra":   true,
		},
		"entry_gates": map[string]any{
			"live_flag_required": true,
			"auth_json_read":     false,
			"credential_print":   false,
		},
		"scenarios":         m,
		"ack_layers":        map[string]any{"strongest_proven": "session_visible", "explicit_claimed": false},
		"privacy_checks":    map[string]any{"auth_json_read": false},
		"limitations":       []string{},
		"scenario_registry": append([]string{}, goMandatoryIDs...),
		"final_disposition": disp,
	}
	errs := validateReportV2Basics(report, m)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "evil_extra") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown provenance field error; errs=%v", errs)
	}
}

func committedV2SchemaPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	p := filepath.Join(root, "docs", "evidence", "grok_build", "reinframe.grok_build_live_control.v2.schema.json")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("committed schema missing: %s: %v", p, err)
	}
	return p
}

// TestCommittedV2Schema_RejectsNestedUnknownAndInvalidStatus validates against
// the real committed schema artifact (#209), not a hand-built subset.
func TestCommittedV2Schema_RejectsNestedUnknownAndInvalidStatus(t *testing.T) {
	t.Parallel()
	path := committedV2SchemaPath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Sanity: committed file must claim nested closed ack_layers.
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	props, _ := raw["properties"].(map[string]any)
	ack, _ := props["ack_layers"].(map[string]any)
	if ack["additionalProperties"] != false {
		t.Fatalf("committed schema ack_layers.additionalProperties want false got %v", ack["additionalProperties"])
	}

	c := jsonschema.NewCompiler()
	c.Draft = jsonschema.Draft2020
	url := "https://reinframe.dev/schemas/reinframe.grok_build_live_control.v2.json"
	if err := c.AddResource(url, strings.NewReader(string(b))); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile(url)
	if err != nil {
		t.Fatalf("compile committed schema: %v", err)
	}

	// Illegal scenario status.
	bad := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue": 167, "generated_at": "t", "goos": "darwin", "harness": "cmd/groklive",
		},
		"entry_gates": map[string]any{
			"live_flag_required": true, "auth_json_read": false, "credential_print": false,
		},
		"scenarios": map[string]any{
			"HOOK-ALLOW-001": map[string]any{"id": "HOOK-ALLOW-001", "status": "PASSX"},
		},
		"ack_layers":        map[string]any{"strongest_proven": "transport", "explicit_claimed": false},
		"privacy_checks":    map[string]any{"auth_json_read": false, "method": "best_effort_scan"},
		"limitations":       []string{"x"},
		"scenario_registry": []string{"HOOK-ALLOW-001"},
		"final_disposition": "GO",
	}
	if err := sch.Validate(bad); err == nil {
		t.Fatal("committed schema must reject status PASSX")
	}

	// Nested unknown field on provenance.
	bad2 := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue": 167, "generated_at": "t", "goos": "darwin", "harness": "cmd/groklive",
			"not_a_real_field": 1,
		},
		"entry_gates": map[string]any{
			"live_flag_required": true, "auth_json_read": false, "credential_print": false,
		},
		"scenarios": map[string]any{
			"HOOK-ALLOW-001": map[string]any{"id": "HOOK-ALLOW-001", "status": "PASS"},
		},
		"ack_layers":        map[string]any{"strongest_proven": "transport", "explicit_claimed": false},
		"privacy_checks":    map[string]any{"method": "best_effort_scan"},
		"limitations":       []string{},
		"scenario_registry": []string{"HOOK-ALLOW-001"},
		"final_disposition": "MORE_DATA",
	}
	if err := sch.Validate(bad2); err == nil {
		t.Fatal("committed schema must reject nested unknown provenance field")
	}

	// Nested unknown on ack_layers.
	bad3 := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue": 167, "generated_at": "t", "goos": "darwin", "harness": "cmd/groklive",
		},
		"entry_gates": map[string]any{
			"live_flag_required": true, "auth_json_read": false, "credential_print": false,
		},
		"scenarios":         map[string]any{},
		"ack_layers":        map[string]any{"strongest_proven": "transport", "explicit_claimed": false, "extra_ack": true},
		"privacy_checks":    map[string]any{},
		"limitations":       []string{},
		"scenario_registry": []string{"HOOK-ALLOW-001"},
		"final_disposition": "MORE_DATA",
	}
	if err := sch.Validate(bad3); err == nil {
		t.Fatal("committed schema must reject unknown ack_layers field")
	}
}

// TestEvaluateDisposition_V2_IllegalStatusOnMandatoryInFullMatrix proves shipped
// evaluateDisposition is the path under test (not a reimplementation).
func TestEvaluateDisposition_V2_IllegalStatusOnMandatoryInFullMatrix(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	// All correlation proofs present; only one illegal status should still kill GO/LIMITED_GO.
	m["STATIC-PERM-001"] = ScenarioResult{ID: "STATIC-PERM-001", Status: "PASSX"}
	disp, reasons := evaluateDisposition(m)
	if disp == "GO" || disp == "LIMITED_GO" {
		t.Fatalf("illegal mandatory status must not be GO/LIMITED_GO; got %s %v", disp, reasons)
	}
}
