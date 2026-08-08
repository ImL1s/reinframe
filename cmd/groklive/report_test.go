package main

import "testing"

func TestEvaluateDisposition_MandatoryMatrix(t *testing.T) {
	t.Parallel()
	passAll := map[string]ScenarioResult{
		"HOOK-ALLOW-001":  {ID: "HOOK-ALLOW-001", Status: "PASS"},
		"HOOK-DENY-001":   {ID: "HOOK-DENY-001", Status: "PASS"},
		"ACP-INIT-001":    {ID: "ACP-INIT-001", Status: "PASS"},
		"ACP-AUTH-001":    {ID: "ACP-AUTH-001", Status: "PASS"},
		"ACP-SESSION-001": {ID: "ACP-SESSION-001", Status: "PASS"},
		"ACP-CLEANUP-001": {ID: "ACP-CLEANUP-001", Status: "PASS"},
	}
	disp, reasons := evaluateDisposition(passAll)
	if disp != "GO" {
		t.Fatalf("want GO got %s reasons=%v", disp, reasons)
	}

	// Missing scenarios → MORE_DATA
	disp, _ = evaluateDisposition(nil)
	if disp != "MORE_DATA" {
		t.Fatalf("empty want MORE_DATA got %s", disp)
	}

	// Auth hard fail
	badAuth := copyScenarios(passAll)
	badAuth["ACP-AUTH-001"] = ScenarioResult{ID: "ACP-AUTH-001", Status: "FAIL"}
	disp, reasons = evaluateDisposition(badAuth)
	if disp != "NO_GO" {
		t.Fatalf("auth fail want NO_GO got %s %v", disp, reasons)
	}

	// INCONCLUSIVE mandatory → LIMITED_GO
	inconclusive := copyScenarios(passAll)
	inconclusive["HOOK-DENY-001"] = ScenarioResult{ID: "HOOK-DENY-001", Status: "INCONCLUSIVE"}
	disp, reasons = evaluateDisposition(inconclusive)
	if disp != "LIMITED_GO" {
		t.Fatalf("inconclusive want LIMITED_GO got %s %v", disp, reasons)
	}

	// Missing mandatory → NO_GO
	missing := copyScenarios(passAll)
	delete(missing, "ACP-SESSION-001")
	disp, reasons = evaluateDisposition(missing)
	if disp != "NO_GO" {
		t.Fatalf("missing want NO_GO got %s %v", disp, reasons)
	}

	// FAIL mandatory → NO_GO
	failDeny := copyScenarios(passAll)
	failDeny["HOOK-ALLOW-001"] = ScenarioResult{ID: "HOOK-ALLOW-001", Status: "FAIL"}
	disp, _ = evaluateDisposition(failDeny)
	if disp != "NO_GO" {
		t.Fatalf("fail want NO_GO got %s", disp)
	}
}

func copyScenarios(in map[string]ScenarioResult) map[string]ScenarioResult {
	out := make(map[string]ScenarioResult, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
