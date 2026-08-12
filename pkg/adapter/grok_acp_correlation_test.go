package adapter

import (
	"strings"
	"testing"
)

func TestUpdateStrongCorrelation_RequiresIdentity(t *testing.T) {
	t.Parallel()
	// Session-only update must not strongly correlate.
	u := map[string]any{"sessionId": "s1", "sessionUpdate": "agent_message_chunk"}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("session-only update must not strong-correlate: %s", why)
	}
	// Matching interventionId alone is only strong when delivery has no request id
	// (requestID==0). With a delivery request id, intervention-only is insufficient.
	u2 := map[string]any{
		"sessionId": "s1",
		"update": map[string]any{
			"sessionUpdate":  "agent_message_chunk",
			"interventionId": "iv-1",
		},
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 0)
	if !ok || why != "interventionId" {
		t.Fatalf("want interventionId match (no delivery request id), got ok=%v why=%s", ok, why)
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if ok {
		t.Fatalf("intervention-only must not strong-correlate when delivery has request id: %s", why)
	}
	// Matching requestId.
	u3 := map[string]any{"sessionId": "s1", "requestId": float64(42), "sessionUpdate": "tool_call"}
	ok, why = UpdateStrongCorrelation(u3, "iv-1", "", 42)
	if !ok || why != "requestId" {
		t.Fatalf("want requestId match, got ok=%v why=%s", ok, why)
	}
	// Mismatched requestId.
	ok, _ = UpdateStrongCorrelation(u3, "iv-1", "", 99)
	if ok {
		t.Fatal("mismatched requestId must not correlate")
	}
}

func TestUpdateStrongCorrelation_RejectsConflictingIdentities(t *testing.T) {
	t.Parallel()
	// Matching interventionId but wrong requestId must not correlate.
	u := map[string]any{
		"sessionId":      "s1",
		"requestId":      float64(99),
		"interventionId": "iv-1",
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("conflicting requestId must not correlate: why=%s", why)
	}
	if !strings.Contains(why, "conflict") {
		t.Fatalf("want conflict reason, got %s", why)
	}
	// All matching identities ok.
	u2 := map[string]any{
		"sessionId": "s1",
		"update": map[string]any{
			"sessionUpdate":  "agent_message_chunk",
			"interventionId": "iv-1",
			"requestId":      float64(42),
		},
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if !ok {
		t.Fatalf("matching identities should correlate: %s", why)
	}
	// Cross-layer duplicate identity with unequal values must not correlate (Codex P1).
	u3 := map[string]any{
		"sessionId":     "s1",
		"requestId":     float64(42),
		"sessionUpdate": "agent_message_chunk",
		"params": map[string]any{
			"update": map[string]any{
				"sessionUpdate": "agent_message_chunk",
				"requestId":     float64(99),
			},
		},
	}
	ok, why = UpdateStrongCorrelation(u3, "iv-1", "", 42)
	if ok {
		t.Fatalf("cross-layer requestId conflict must not correlate: why=%s", why)
	}
	if !strings.Contains(why, "conflict") {
		t.Fatalf("want layer conflict reason, got %s", why)
	}
}

func TestUpdateStrongCorrelation_RejectsPartialRequestIDString(t *testing.T) {
	t.Parallel()
	// fmt.Sscan-style prefix parse must not accept "42junk".
	u := map[string]any{
		"sessionId":     "s1",
		"sessionUpdate": "agent_message_chunk",
		"requestId":     "42junk",
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("partial requestId string must not correlate: why=%s", why)
	}
	u2 := map[string]any{
		"sessionId":     "s1",
		"sessionUpdate": "agent_message_chunk",
		"requestId":     "42",
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if !ok || why != "requestId" {
		t.Fatalf("canonical requestId string should match: ok=%v why=%s", ok, why)
	}
}

// Pro exact-head P1 (e97bc6c): matching interventionId + present-but-unparseable
// requestId must not upgrade to session_visible.
func TestUpdateStrongCorrelation_RejectsUnparseableRequestWithMatchingIntervention(t *testing.T) {
	t.Parallel()
	u := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": "iv-1",
		"requestId":      "42junk",
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("malformed requestId must not be discarded when intervention matches: why=%s", why)
	}
	if !strings.Contains(why, "unparseable") && !strings.Contains(why, "conflict") {
		t.Fatalf("want unparseable/conflict reason, got %s", why)
	}
	// Fractional JSON number is also unparseable as request identity.
	u3 := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": "iv-1",
		"requestId":      42.5,
	}
	ok, why = UpdateStrongCorrelation(u3, "iv-1", "", 42)
	if ok {
		t.Fatalf("fractional requestId must not correlate: why=%s", why)
	}
}

// Pro R6 P1: rpcId must participate in envelope extraction and conflict checks.
func TestUpdateStrongCorrelation_RejectsRPCIdConflicts(t *testing.T) {
	t.Parallel()
	// Malformed rpcId with matching intervention must not correlate.
	u := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": "iv-1",
		"rpcId":          "42junk",
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("malformed rpcId must not be discarded: why=%s", why)
	}
	// Conflicting numeric rpcId vs expected requestId.
	u2 := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": "iv-1",
		"rpcId":          float64(99),
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if ok {
		t.Fatalf("conflicting rpcId must not correlate: why=%s", why)
	}
	// Matching rpcId alone correlates as requestId.
	u3 := map[string]any{
		"_method":       "session/update",
		"sessionId":     "s1",
		"sessionUpdate": "agent_message_chunk",
		"rpcId":         float64(42),
	}
	ok, why = UpdateStrongCorrelation(u3, "iv-1", "", 42)
	if !ok || why != "requestId" {
		t.Fatalf("matching rpcId should correlate as requestId: ok=%v why=%s", ok, why)
	}
}

// Pro R6 P1: non-string interventionId must fail closed when another identity matches.
func TestUpdateStrongCorrelation_RejectsMalformedInterventionType(t *testing.T) {
	t.Parallel()
	u := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": 123, // wrong type
		"requestId":      float64(42),
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("numeric interventionId must not be ignored: why=%s", why)
	}
	if !strings.Contains(why, "malformed") && !strings.Contains(why, "conflict") {
		t.Fatalf("want malformed reason, got %s", why)
	}
}

// Pro R7 P1: string interventionId in one layer + numeric in another must not
// equal via request-id coercion and must not upgrade to session_visible.
func TestUpdateStrongCorrelation_RejectsCrossLayerStringNumberIntervention(t *testing.T) {
	t.Parallel()
	u := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": "42",
		"params": map[string]any{
			"update": map[string]any{
				"sessionUpdate":  "agent_message_chunk",
				"interventionId": float64(42),
			},
		},
	}
	ok, why := UpdateStrongCorrelation(u, "42", "", 0)
	if ok {
		t.Fatalf("cross-layer string/number interventionId must not correlate: why=%s", why)
	}
	// null intervention with matching request must not correlate.
	u2 := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"sessionUpdate":  "agent_message_chunk",
		"interventionId": nil,
		"requestId":      float64(42),
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if ok {
		t.Fatalf("null interventionId must not be ignored: why=%s", why)
	}
}

func TestUpdateStrongCorrelation_RejectsAliasKeyConflicts(t *testing.T) {
	t.Parallel()
	// requestId matches delivery but request_id conflicts (alias family).
	u := map[string]any{
		"sessionId":     "s1",
		"sessionUpdate": "agent_message_chunk",
		"requestId":     float64(42),
		"request_id":    float64(99),
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("alias requestId conflict must not correlate: why=%s", why)
	}
	if !strings.Contains(why, "alias") && !strings.Contains(why, "conflict") {
		t.Fatalf("want alias conflict reason, got %s", why)
	}
	// Equal aliases are fine.
	u2 := map[string]any{
		"sessionId":     "s1",
		"sessionUpdate": "agent_message_chunk",
		"requestId":     float64(42),
		"request_id":    "42",
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if !ok || why != "requestId" {
		t.Fatalf("equal aliases should match: ok=%v why=%s", ok, why)
	}
}

func TestUpdateStrongCorrelation_RequiresSessionUpdateMethod(t *testing.T) {
	t.Parallel()
	// Unrelated notification with matching identity must not upgrade.
	u := map[string]any{
		"_method":        "session/request_permission",
		"sessionId":      "s1",
		"interventionId": "iv-1",
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("non-session/update method must not correlate: why=%s", why)
	}
	if !strings.Contains(why, "session/update") {
		t.Fatalf("want method reason, got %s", why)
	}
	u2 := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 0)
	if !ok || why != "interventionId" {
		t.Fatalf("session/update with identity should match (no delivery request id): ok=%v why=%s", ok, why)
	}
	// With delivery request id, intervention-only is insufficient (stale retry race).
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if ok {
		t.Fatalf("intervention-only must not correlate when delivery has request id: %s", why)
	}
	u3 := map[string]any{
		"_method":        "session/update",
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"requestId":      float64(42),
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why = UpdateStrongCorrelation(u3, "iv-1", "", 42)
	if !ok {
		t.Fatalf("session/update with matching requestId should correlate: why=%s", why)
	}
	// Canonical JSON-RPC method is authoritative (Pro R24 P2): session/request_permission
	// with a sessionUpdate-shaped body must not upgrade via shape fallback.
	u4 := map[string]any{
		"method": "session/request_permission",
		"params": map[string]any{
			"sessionUpdate":  "agent_message_chunk",
			"interventionId": "iv-1",
			"requestId":      float64(42),
		},
		// Also flatten identity for UpdateStrongCorrelation readers.
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"requestId":      float64(42),
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why = UpdateStrongCorrelation(u4, "iv-1", "", 42)
	if ok {
		t.Fatalf("canonical method session/request_permission must not correlate via shape: why=%s", why)
	}
	// method vs _method disagreement → fail closed.
	u5 := map[string]any{
		"method":         "session/update",
		"_method":        "session/request_permission",
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"requestId":      float64(42),
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why = UpdateStrongCorrelation(u5, "iv-1", "", 42)
	if ok {
		t.Fatalf("method/_method disagreement must fail closed: why=%s", why)
	}
	// Canonical method session/update alone is enough.
	u6 := map[string]any{
		"method":         "session/update",
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"requestId":      float64(42),
		"sessionUpdate":  "agent_message_chunk",
	}
	ok, why = UpdateStrongCorrelation(u6, "iv-1", "", 42)
	if !ok {
		t.Fatalf("canonical method session/update should correlate: why=%s", why)
	}
	// Pro R25 P2: present-but-malformed method must not fall through to shape.
	for _, bad := range []any{123, nil, "", " ", " session/update ", "session/request_permission"} {
		uBad := map[string]any{
			"method":         bad,
			"sessionId":      "s1",
			"interventionId": "iv-1",
			"requestId":      float64(42),
			"sessionUpdate":  "agent_message_chunk",
		}
		ok, why = UpdateStrongCorrelation(uBad, "iv-1", "", 42)
		if ok {
			t.Fatalf("malformed method %v must not correlate: why=%s", bad, why)
		}
	}
	// malformed top-level _method fails even if params._method is valid.
	uMix := map[string]any{
		"_method":        99,
		"sessionId":      "s1",
		"interventionId": "iv-1",
		"requestId":      float64(42),
		"sessionUpdate":  "agent_message_chunk",
		"params": map[string]any{
			"_method": "session/update",
		},
	}
	ok, why = UpdateStrongCorrelation(uMix, "iv-1", "", 42)
	if ok {
		t.Fatalf("malformed top-level _method must fail closed even with valid params._method: why=%s", why)
	}
}

func TestUpdateStrongCorrelation_IgnoresAgentContentSpoof(t *testing.T) {
	t.Parallel()
	// Agent-controlled nested content echoes InterventionID — must NOT upgrade.
	u := map[string]any{
		"sessionId": "s1",
		"sessionUpdate": "tool_call",
		"toolCall": map[string]any{
			"title": "echo",
			"rawInput": map[string]any{
				"interventionId": "iv-1", // spoof inside tool args
			},
		},
		"content": map[string]any{
			"type": "text",
			"text": "re InterventionID iv-1 from advice prompt",
			"meta": map[string]any{"interventionId": "iv-1"},
		},
	}
	ok, why := UpdateStrongCorrelation(u, "iv-1", "", 42)
	if ok {
		t.Fatalf("agent content must not strong-correlate: why=%s", why)
	}
	// Envelope meta still works when delivery has no request id.
	u2 := map[string]any{
		"sessionId": "s1",
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"meta": map[string]any{
				"interventionId": "iv-1",
			},
		},
	}
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 0)
	if !ok || why != "interventionId" {
		t.Fatalf("envelope meta interventionId should match: ok=%v why=%s", ok, why)
	}
	// With delivery request id, require requestId (or challenge) — not intervention alone.
	ok, why = UpdateStrongCorrelation(u2, "iv-1", "", 42)
	if ok {
		t.Fatalf("intervention-only insufficient when delivery has request id: %s", why)
	}
}

func TestDrainUpdates_NonBlocking(t *testing.T) {
	t.Parallel()
	// Synthetic client with filled queue.
	c := &GrokACPClient{updates: make(chan map[string]any, 4)}
	c.updates <- map[string]any{"sessionId": "a"}
	c.updates <- map[string]any{"sessionId": "b"}
	n := c.DrainUpdates()
	if n != 2 {
		t.Fatalf("drained=%d want 2", n)
	}
	if c.DrainUpdates() != 0 {
		t.Fatal("second drain must be empty")
	}
}
