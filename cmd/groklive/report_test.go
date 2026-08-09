package main

import "testing"

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
}
