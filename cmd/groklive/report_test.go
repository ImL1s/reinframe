package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
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

func cleanPrivacyScan() map[string]any {
	return map[string]any{
		"method":                                    "complete_or_fail_flat_scan",
		"complete":                                  true,
		"files_seen":                                2,
		"files_scanned":                             2,
		"files_skipped":                             0,
		"bytes_scanned":                             100,
		"auth_json_read":                            false,
		"auth_json_path_leak_suspected":             false,
		"token_fields_in_auth_envelope":             false,
		"raw_thoughts_stored":                       false,
		"secret_pattern_hits":                       0,
		"failure_classes":                           []string{},
	}
}

func validCaps() map[string]any {
	// Canonical harness shape from adapter (#218).
	pre := mustFoundationMap(adapter.NewGrokACPFoundationManifest())
	neg := adapter.GrokACPNegotiatedCaps{
		ProtocolVersion: adapter.GrokACPProtocolVersion,
		LoadSession:     true,
		Cancel:          true,
		AuthMethods:     []string{"cached_token"},
	}
	neg.CapsDigest = adapter.FormatGrokACPCapsDigest(
		neg.LoadSession, neg.Pause, neg.Cancel, neg.Resume, neg.ToolInspection, neg.DiffInspection)
	post := mustFoundationMap(adapter.ManifestFromNegotiated(neg))
	return map[string]any{
		"pre_handshake":  pre,
		"post_handshake": post,
		"auth_methods":   []any{"cached_token"},
		"caps_digest":    neg.CapsDigest,
	}
}

func mustFoundationMap(m adapter.GrokACPFoundationManifest) map[string]any {
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

func mustDecodeFoundation(t *testing.T, m map[string]any) adapter.GrokACPFoundationManifest {
	t.Helper()
	out, err := decodeClosedFoundation(m)
	if err != nil {
		t.Fatalf("decode foundation: %v", err)
	}
	return out
}

// TestLiveQualification_PrivacyAndPreflight gates GO and LIMITED_GO (#215).
func TestLiveQualification_PrivacyAndPreflight(t *testing.T) {
	t.Parallel()
	sc := fullGOScenarios()
	priv := cleanPrivacyScan()
	caps := validCaps()

	fullRev := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	got, msgs := liveQualification("GO", priv, caps, sc, true, true, true, "1.0.0", fullRev, "vcs", false)
	if got != "GO" {
		t.Fatalf("clean want GO got %s msgs=%v", got, msgs)
	}

	// LIMITED_GO also gated.
	got, _ = liveQualification("LIMITED_GO", priv, caps, sc, true, false, false, "1.0.0", fullRev, "vcs", false)
	if got != "NO_GO" {
		t.Fatalf("invalid preflight on LIMITED_GO want NO_GO got %s", got)
	}

	// Incomplete privacy.
	inc := cleanPrivacyScan()
	inc["complete"] = false
	inc["files_skipped"] = 1
	got, _ = liveQualification("GO", inc, caps, sc, true, true, true, "1.0.0", fullRev, "vcs", false)
	if got != "NO_GO" {
		t.Fatalf("incomplete privacy want NO_GO got %s", got)
	}

	// Scalar caps rejected.
	got, _ = liveQualification("GO", priv, "", sc, true, true, true, "1.0.0", fullRev, "vcs", false)
	if got != "NO_GO" {
		t.Fatalf("scalar caps want NO_GO got %s", got)
	}

	// Ambient commit unknown source rejected.
	got, _ = liveQualification("GO", priv, caps, sc, true, true, true, "1.0.0", fullRev, "unknown", false)
	if got != "NO_GO" {
		t.Fatalf("unknown commit src want NO_GO got %s", got)
	}

	// Dirty binary rejected.
	got, _ = liveQualification("GO", priv, caps, sc, true, true, true, "1.0.0", fullRev, "vcs", true)
	if got != "NO_GO" {
		t.Fatalf("dirty binary want NO_GO got %s", got)
	}
}

func TestDemoteFloor_NeverPromotes(t *testing.T) {
	t.Parallel()
	if demoteFloor("NO_GO", "LIMITED_GO") != "NO_GO" {
		t.Fatal("must not promote NO_GO to LIMITED_GO")
	}
	if demoteFloor("MORE_DATA", "GO") != "MORE_DATA" {
		t.Fatal("must not promote MORE_DATA to GO")
	}
	if demoteFloor("GO", "LIMITED_GO") != "LIMITED_GO" {
		t.Fatal("must demote GO to LIMITED_GO")
	}
}

func TestScanPrivacy_NestedDirIncomplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["complete"] == true {
		t.Fatalf("nested dir must make complete=false: %+v", p)
	}
}

func TestScanPrivacy_OversizedIncomplete(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	big := make([]byte, maxPrivacyFileBytes+1)
	for i := range big {
		big[i] = 'x'
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["complete"] == true {
		t.Fatalf("oversized must make complete=false: %+v", p)
	}
}

func TestValidateCapabilityManifest_RejectsArbitrary(t *testing.T) {
	t.Parallel()
	sc := fullGOScenarios()
	if err := validateCapabilityManifest(map[string]any{"x": 1}, sc); err == nil {
		t.Fatal("arbitrary object must fail")
	}
	if err := validateCapabilityManifest(false, sc); err == nil {
		t.Fatal("scalar must fail")
	}
	if err := validateCapabilityManifest(validCaps(), sc); err != nil {
		t.Fatalf("valid caps: %v", err)
	}
}

// TestValidateCapabilityManifest_Issue218 covers false-GO provenance closure.
func TestValidateCapabilityManifest_Issue218(t *testing.T) {
	t.Parallel()
	sc := fullGOScenarios()

	// 1. Empty pre/post handshake objects cannot qualify.
	empty := validCaps()
	empty["pre_handshake"] = map[string]any{}
	empty["post_handshake"] = map[string]any{}
	if err := validateCapabilityManifest(empty, sc); err == nil {
		t.Fatal("empty handshake objects must fail")
	}
	got, _ := liveQualification("GO", cleanPrivacyScan(), empty, sc, true, true, true, "1.0.0", strings.Repeat("a", 40), "vcs", false)
	if got != "NO_GO" {
		t.Fatalf("empty handshake want NO_GO got %s", got)
	}

	// 2. Arbitrary post-handshake fields cannot qualify.
	arb := validCaps()
	post := mustFoundationMap(adapter.ManifestFromNegotiated(adapter.GrokACPNegotiatedCaps{
		ProtocolVersion: 1, LoadSession: true, Cancel: true, AuthMethods: []string{"cached_token"},
	}))
	post["agentName"] = "forged"
	arb["post_handshake"] = post
	if err := validateCapabilityManifest(arb, sc); err == nil {
		t.Fatal("arbitrary post fields must fail")
	}

	// 3. Invalid/duplicate auth methods cannot qualify.
	dup := validCaps()
	dup["auth_methods"] = []any{"cached_token", "cached_token"}
	if err := validateCapabilityManifest(dup, sc); err == nil {
		t.Fatal("duplicate auth methods must fail")
	}

	// 4. Forged caps_digest cannot qualify.
	forged := validCaps()
	forged["caps_digest"] = "arbitrary"
	if err := validateCapabilityManifest(forged, sc); err == nil {
		t.Fatal("forged digest must fail")
	}
	// Also reject digest that does not match recomputed post facts.
	forged2 := validCaps()
	forged2["caps_digest"] = adapter.FormatGrokACPCapsDigest(false, false, false, false, false, false)
	if err := validateCapabilityManifest(forged2, sc); err == nil {
		t.Fatal("stale digest must fail")
	}

	// 5. Manifest/scenario contradiction cannot qualify.
	scFail := fullGOScenarios()
	scFail["ACP-INIT-001"] = ScenarioResult{ID: "ACP-INIT-001", Status: "FAIL"}
	if err := validateCapabilityManifest(validCaps(), scFail); err == nil {
		t.Fatal("INIT FAIL must contradict manifest")
	}
	scNR := fullGOScenarios()
	scNR["ACP-AUTH-001"] = ScenarioResult{ID: "ACP-AUTH-001", Status: "NOT_RUN"}
	if err := validateCapabilityManifest(validCaps(), scNR); err == nil {
		t.Fatal("AUTH NOT_RUN must contradict manifest")
	}

	// Auth invent: post omits auth while top-level invents methods.
	invent := validCaps()
	postNoAuth := mustFoundationMap(adapter.ManifestFromNegotiated(adapter.GrokACPNegotiatedCaps{
		ProtocolVersion: 1, LoadSession: true, Cancel: true,
	}))
	// Ensure post has empty auth_methods key omitted → empty after decode.
	delete(postNoAuth, "auth_methods")
	invent["post_handshake"] = postNoAuth
	invent["auth_methods"] = []any{"forged_method"}
	invent["caps_digest"] = adapter.CapsDigestFromFoundation(mustDecodeFoundation(t, postNoAuth))
	if err := validateCapabilityManifest(invent, sc); err == nil {
		t.Fatal("top-level auth invent with empty post auth must fail")
	}

	// Inflated negotiated_level with sparse caps must fail.
	levelForge := validCaps()
	postL := mustFoundationMap(adapter.ManifestFromNegotiated(adapter.GrokACPNegotiatedCaps{
		ProtocolVersion: 1, LoadSession: true, Cancel: true, AuthMethods: []string{"cached_token"},
	}))
	postL["negotiated_level"] = float64(3)
	levelForge["post_handshake"] = postL
	levelForge["caps_digest"] = adapter.CapsDigestFromFoundation(mustDecodeFoundation(t, postL))
	if err := validateCapabilityManifest(levelForge, sc); err == nil {
		t.Fatal("forged negotiated_level=3 must fail")
	}

	// 6. Valid canonical manifest passes and digest is reproducible.
	v := validCaps()
	if err := validateCapabilityManifest(v, sc); err != nil {
		t.Fatalf("valid: %v", err)
	}
	postM, err := decodeClosedFoundation(v["post_handshake"])
	if err != nil {
		t.Fatal(err)
	}
	if v["caps_digest"] != adapter.CapsDigestFromFoundation(postM) {
		t.Fatal("digest not reproducible from post_handshake")
	}
	got, _ = liveQualification("GO", cleanPrivacyScan(), v, sc, true, true, true, "1.0.0", strings.Repeat("b", 40), "vcs", false)
	if got != "GO" {
		t.Fatalf("valid canonical want GO got %s", got)
	}
}

func TestBuildIdentity_Issue218(t *testing.T) {
	t.Parallel()
	// 7. Arbitrary short ldflags commit cannot qualify.
	got, _ := liveQualification("GO", cleanPrivacyScan(), validCaps(), fullGOScenarios(),
		true, true, true, "1.0.0", "foo", "ldflags", false)
	if got != "NO_GO" {
		t.Fatalf("short ldflags commit want NO_GO got %s", got)
	}
	// isFullVCSRevision rejects non-hex and short.
	if isFullVCSRevision("foo") || isFullVCSRevision("deadbeef") || isFullVCSRevision(strings.Repeat("g", 40)) {
		t.Fatal("malformed revisions must not pass isFullVCSRevision")
	}
	// 8. Dirty ldflags/build cannot qualify.
	full := strings.Repeat("c", 40)
	got, _ = liveQualification("GO", cleanPrivacyScan(), validCaps(), fullGOScenarios(),
		true, true, true, "1.0.0", full, "ldflags", true)
	if got != "NO_GO" {
		t.Fatalf("dirty ldflags want NO_GO got %s", got)
	}
	// 9. Valid clean full VCS revision can qualify.
	got, _ = liveQualification("GO", cleanPrivacyScan(), validCaps(), fullGOScenarios(),
		true, true, true, "1.0.0", full, "vcs", false)
	if got != "GO" {
		t.Fatalf("clean full rev want GO got %s", got)
	}
	// SHA-256 full length also accepted.
	sha256 := strings.Repeat("d", 64)
	if !isFullVCSRevision(sha256) {
		t.Fatal("64-hex should be accepted")
	}
}

func TestReinframeBuildIdentity_LdflagsDirtyDefault(t *testing.T) {
	// Not parallel: mutates package-level ldflags vars (avoid race with other tests).
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() {
		reinframeCommit, reinframeDirty = prevC, prevD
	})

	// -X main.reinframeCommit=foo → dirty + non-full, cannot qualify.
	reinframeCommit = "foo"
	reinframeDirty = ""
	rev, dirty, src := reinframeBuildIdentity()
	if rev != "foo" || !dirty || src != "ldflags" {
		t.Fatalf("foo ldflags: rev=%q dirty=%v src=%s", rev, dirty, src)
	}

	// Full SHA without dirty attestation → dirty.
	full := "0123456789abcdef0123456789abcdef01234567"
	reinframeCommit = full
	reinframeDirty = ""
	rev, dirty, src = reinframeBuildIdentity()
	if rev != full || !dirty || src != "ldflags" {
		t.Fatalf("unattested clean: rev=%q dirty=%v src=%s", rev, dirty, src)
	}

	// Explicit clean attestation.
	reinframeDirty = "false"
	rev, dirty, src = reinframeBuildIdentity()
	if rev != full || dirty || src != "ldflags" {
		t.Fatalf("attested clean: rev=%q dirty=%v src=%s", rev, dirty, src)
	}

	// Explicit dirty.
	reinframeDirty = "true"
	_, dirty, _ = reinframeBuildIdentity()
	if !dirty {
		t.Fatal("explicit dirty must be dirty")
	}
}

func TestGitHEAD_IsEmptyNotAmbient(t *testing.T) {
	t.Parallel()
	// gitHEAD must not expose ambient CWD HEAD for qualification.
	if gitHEAD() != "" {
		t.Fatalf("gitHEAD must be empty for binary-bound provenance; got %q", gitHEAD())
	}
	rev, _, src := reinframeBuildIdentity()
	// In tests, VCS info may be present when built with -buildvcs.
	if src == "unknown" && rev != "" {
		t.Fatal("unknown source must not invent revision")
	}
}

// TestMonotonicFloor_StaticPermRecomputeCannotPromote models the #214 bug:
// preflight demotion then STATIC-PERM recompute must not revive LIMITED_GO.
func TestMonotonicFloor_StaticPermRecomputeCannotPromote(t *testing.T) {
	t.Parallel()
	// Historical weak matrix → LIMITED_GO or MORE_DATA from scenarios alone.
	hist := map[string]ScenarioResult{
		"HOOK-ALLOW-001":  {ID: "HOOK-ALLOW-001", Status: "PASS"},
		"HOOK-DENY-001":   {ID: "HOOK-DENY-001", Status: "PASS", DenyDirectProof: false},
		"HOOK-FAIL-001":   {ID: "HOOK-FAIL-001", Status: "PASS", FailOpenInvoked: false, HostOutcome: "host_fail_open"},
		"HOOK-FAIL-002":   {ID: "HOOK-FAIL-002", Status: "PASS", FailOpenInvoked: false, HostOutcome: "host_fail_open"},
		"HOOK-FAIL-003":   {ID: "HOOK-FAIL-003", Status: "PASS", FailOpenInvoked: false, HostOutcome: "host_fail_open"},
		"HOOK-FAIL-004":   {ID: "HOOK-FAIL-004", Status: "PASS", FailOpenInvoked: false, HostOutcome: "host_fail_open"},
		"ACP-INIT-001":    {ID: "ACP-INIT-001", Status: "PASS"},
		"ACP-AUTH-001":    {ID: "ACP-AUTH-001", Status: "PASS"},
		"ACP-SESSION-001": {ID: "ACP-SESSION-001", Status: "PASS", SessionCorrelated: false, ACKLayer: "session_visible"},
		"ACP-CLEANUP-001": {ID: "ACP-CLEANUP-001", Status: "PASS"},
	}
	// Simulate: first evaluate, then preflight missing demotes to NO_GO
	disp, _ := evaluateDisposition(hist)
	floor := demoteFloor(disp, "NO_GO") // missing preflight
	// Then STATIC-PERM insert + re-evaluate (old bug path)
	hist["STATIC-PERM-001"] = ScenarioResult{ID: "STATIC-PERM-001", Status: "NOT_RUN"}
	disp2, _ := evaluateDisposition(hist)
	// Without floor, disp2 might be LIMITED_GO; with floor must stay NO_GO
	final := demoteFloor(floor, disp2)
	if final == "LIMITED_GO" || final == "GO" {
		t.Fatalf("must not re-promote to %s after preflight floor NO_GO", final)
	}
	// liveQualification also blocks LIMITED_GO without valid preflight
	got, _ := liveQualification(disp2, cleanPrivacyScan(), validCaps(), hist, false, false, false, "unknown", "", "unknown", false)
	if got == "LIMITED_GO" || got == "GO" {
		t.Fatalf("qualification must demote LIMITED_GO/GO without preflight; got %s", got)
	}
	// mandatory_ok would be false for NO_GO
	if final != "NO_GO" && got != "NO_GO" {
		t.Fatalf("expected NO_GO path floor=%s qual=%s", final, got)
	}
}

// TestEmbeddedSchema_MatchesDocsArtifact prevents embed/docs drift.
func TestEmbeddedSchema_MatchesDocsArtifact(t *testing.T) {
	t.Parallel()
	path, err := committedV2SchemaFSPath()
	if err != nil {
		t.Fatal(err)
	}
	docs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	emb := EmbeddedV2SchemaJSON()
	if string(docs) != string(emb) {
		t.Fatalf("embedded schema drifts from docs artifact (%d vs %d bytes)", len(emb), len(docs))
	}
	// Must compile via load path (embedded).
	if err := validateReportAgainstCommittedSchema(map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue": 167, "generated_at": "t", "goos": "darwin", "harness": "cmd/groklive",
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
	}); err != nil {
		t.Fatalf("valid minimal report against embed: %v", err)
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

// TestEvaluateDisposition_V2_EmptyEmbeddedIDNeverGO: schema requires id minLength 1.
func TestEvaluateDisposition_V2_EmptyEmbeddedIDNeverGO(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	// Full matrix otherwise perfect — only empty embedded id.
	s := m["HOOK-ALLOW-001"]
	s.ID = ""
	m["HOOK-ALLOW-001"] = s
	disp, reasons := evaluateDisposition(m)
	if disp == "GO" || disp == "LIMITED_GO" {
		t.Fatalf("empty embedded id must not yield GO/LIMITED_GO; got %s %v", disp, reasons)
	}
	if disp != "NO_GO" {
		t.Fatalf("want NO_GO got %s %v", disp, reasons)
	}
}

// TestValidateReportAgainstCommittedSchema_RejectsEmptyID drives the same gate
// used before report disk write.
func TestValidateReportAgainstCommittedSchema_RejectsEmptyID(t *testing.T) {
	t.Parallel()
	m := fullGOScenarios()
	// Build a report-shaped document with empty id in scenarios (schema violation).
	scenarios := map[string]any{}
	for id, sr := range m {
		scenarios[id] = map[string]any{
			"id":     "", // violates minLength:1
			"status": sr.Status,
		}
	}
	// Put one valid-looking entry that is empty id
	report := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue": 167, "generated_at": "t", "goos": "darwin", "harness": "cmd/groklive",
		},
		"entry_gates": map[string]any{
			"live_flag_required": true, "auth_json_read": false, "credential_print": false,
		},
		"scenarios":         scenarios,
		"ack_layers":        map[string]any{"strongest_proven": "session_visible", "explicit_claimed": false},
		"privacy_checks":    map[string]any{"method": "best_effort_scan"},
		"limitations":       []string{},
		"scenario_registry": append([]string{}, goMandatoryIDs...),
		"final_disposition": "GO",
	}
	if err := validateReportAgainstCommittedSchema(report); err == nil {
		t.Fatal("committed schema must reject empty scenario id")
	}
}

// TestClosedSchemaV2_MatchesCommittedArtifact pins in-memory schema SoT against repo file.
func TestClosedSchemaV2_MatchesCommittedCriticalClosedness(t *testing.T) {
	t.Parallel()
	path := committedV2SchemaPath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var committed map[string]any
	if err := json.Unmarshal(b, &committed); err != nil {
		t.Fatal(err)
	}
	mem := closedSchemaV2()
	// Critical closedness flags must agree (full deep equal is brittle on key order).
	if mem["additionalProperties"] != false || committed["additionalProperties"] != false {
		t.Fatal("top-level must be closed in both sources")
	}
	memProps, _ := mem["properties"].(map[string]any)
	comProps, _ := committed["properties"].(map[string]any)
	for _, key := range []string{"ack_layers", "provenance", "entry_gates"} {
		mObj, _ := memProps[key].(map[string]any)
		cObj, _ := comProps[key].(map[string]any)
		if mObj["additionalProperties"] != false || cObj["additionalProperties"] != false {
			t.Fatalf("%s must be nested-closed in memory and committed schema", key)
		}
	}
}

// --- #219 full shipped report path ---

// moreDataScenarios yields MORE_DATA after STATIC-PERM inject (AUTH PASS + incomplete core).
func moreDataScenarios() map[string]ScenarioResult {
	return map[string]ScenarioResult{
		"ACP-AUTH-001": {ID: "ACP-AUTH-001", Status: "PASS", Detail: "auth ok for MORE_DATA fixture", At: stamp()},
	}
}

func writeScenarios(t *testing.T, dir string, m map[string]ScenarioResult) {
	t.Helper()
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scenarios.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestGenerateLiveReport_MoreDataMissingPreflight_LimitationAndSchema(t *testing.T) {
	dir := t.TempDir()
	writeScenarios(t, dir, moreDataScenarios())
	// No preflight.json — diagnostic must appear; disposition stays non-qualifying.
	// One regular evidence file so privacy can complete if other files ok.
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok evidence"), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatalf("generateLiveReport: %v", err)
	}
	if out.Disposition != "MORE_DATA" {
		t.Fatalf("want MORE_DATA (not forced NO_GO), got %s reasons=%v", out.Disposition, out.Reasons)
	}
	if out.MandatoryOK {
		t.Fatal("mandatory_ok must be false")
	}
	if !out.ArtifactValid {
		t.Fatal("artifact_valid must be true for written report")
	}
	found := false
	for _, r := range out.Reasons {
		if strings.Contains(r, "preflight.json missing") {
			found = true
		}
		if strings.Contains(r, "missing preflight forbids GO/LIMITED_GO") {
			t.Fatalf("MORE_DATA must not get GO-only preflight demote phrase: %v", out.Reasons)
		}
	}
	if !found {
		t.Fatalf("limitations must include preflight.json missing; got %v", out.Reasons)
	}
	// On-disk JSON must validate.
	b, err := os.ReadFile(out.JSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if err := validateReportAgainstCommittedSchema(report); err != nil {
		t.Fatalf("written report schema: %v", err)
	}
	if d, _ := report["final_disposition"].(string); d != out.Disposition {
		t.Fatalf("disposition mismatch disk=%s out=%s", d, out.Disposition)
	}
}

func TestGenerateLiveReport_MalformedCaps_OmittedAndSchemaValid(t *testing.T) {
	dir := t.TempDir()
	writeScenarios(t, dir, moreDataScenarios())
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-null structurally invalid optional manifest (pre-#219 embedded as-is).
	bad := []byte(`{"pre_handshake":{},"post_handshake":{},"auth_methods":[],"caps_digest":"arbitrary"}`)
	if err := os.WriteFile(filepath.Join(dir, "acp_manifest.json"), bad, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatalf("generateLiveReport: %v", err)
	}
	if out.Disposition == "GO" || out.Disposition == "LIMITED_GO" {
		t.Fatalf("want non-qualifying got %s", out.Disposition)
	}
	if _, ok := out.Report["capability_manifests"]; ok {
		t.Fatal("invalid capability_manifests must be omitted from report")
	}
	omitted := false
	for _, r := range out.Reasons {
		if strings.Contains(r, "capability_manifests omitted") {
			omitted = true
			break
		}
	}
	if !omitted {
		t.Fatalf("expected omit limitation; got %v", out.Reasons)
	}
	b, err := os.ReadFile(out.JSONPath)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if _, ok := report["capability_manifests"]; ok {
		t.Fatal("disk report must not embed invalid capability_manifests")
	}
	if err := validateReportAgainstCommittedSchema(report); err != nil {
		t.Fatalf("disk report must pass schema: %v", err)
	}
}

func TestGenerateLiveReport_OversizedPrivacyIncomplete(t *testing.T) {
	dir := t.TempDir()
	writeScenarios(t, dir, moreDataScenarios())
	big := make([]byte, maxPrivacyFileBytes+64)
	for i := range big {
		big[i] = 'z'
	}
	if err := os.WriteFile(filepath.Join(dir, "huge.bin"), big, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatalf("generateLiveReport: %v", err)
	}
	priv, _ := out.Report["privacy_checks"].(map[string]any)
	if priv == nil {
		t.Fatal("missing privacy_checks")
	}
	if priv["complete"] == true {
		t.Fatalf("oversized must be incomplete: %+v", priv)
	}
	// Must not have scanned the oversized file as complete content.
	if sc, ok := asInt(priv["files_scanned"]); ok && sc > 0 && priv["files_skipped"] == 0 {
		// if only huge file, scanned should be 0
	}
	if sk, ok := asInt(priv["files_skipped"]); !ok || sk < 1 {
		t.Fatalf("expected files_skipped>=1 got %+v", priv)
	}
	// Qualification cannot GO with incomplete privacy — disposition non-GO already.
	if out.MandatoryOK {
		t.Fatal("mandatory_ok false expected")
	}
	if err := validateReportAgainstCommittedSchema(out.Report); err != nil {
		t.Fatalf("schema: %v", err)
	}
}

func TestScanPrivacy_StatBeforeReadOversized(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// Size beyond cap; scanPrivacy must Stat-skip without relying on whole-file success path.
	big := make([]byte, maxPrivacyFileBytes+1)
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["complete"] == true {
		t.Fatal("oversized incomplete")
	}
	fails, _ := p["failure_classes"].([]string)
	if len(fails) == 0 {
		// may be []any from map
		if fa, ok := p["failure_classes"].([]any); ok {
			for _, x := range fa {
				if s, ok := x.(string); ok && strings.HasPrefix(s, "oversized:") {
					return
				}
			}
		}
		t.Fatalf("want oversized failure class: %+v", p)
	}
	ok := false
	for _, f := range fails {
		if strings.HasPrefix(f, "oversized:") {
			ok = true
		}
	}
	if !ok {
		t.Fatalf("want oversized: got %v", fails)
	}
}

func TestScanPrivacy_RawThoughtsDetector(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "thoughts.json"), []byte(`{"raw_thoughts":"secret chain"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["raw_thoughts_stored"] != true {
		t.Fatalf("raw_thoughts marker must set raw_thoughts_stored: %+v", p)
	}
}

func TestGenerateLiveReport_MoreDataMalformedPreflight(t *testing.T) {
	dir := t.TempDir()
	writeScenarios(t, dir, moreDataScenarios())
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "preflight.json"), []byte(`{not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatalf("generateLiveReport: %v", err)
	}
	if out.Disposition != "MORE_DATA" {
		t.Fatalf("want MORE_DATA got %s %v", out.Disposition, out.Reasons)
	}
	found := false
	for _, r := range out.Reasons {
		if strings.Contains(r, "preflight.json malformed") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want malformed preflight limitation: %v", out.Reasons)
	}
	if err := validateReportAgainstCommittedSchema(out.Report); err != nil {
		t.Fatalf("schema: %v", err)
	}
}

func TestScanPrivacy_NonRegularFIFO(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fifo not portable on windows")
	}
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe.fifo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// syscall.Mkfifo
	if err := mkfifo(fifo); err != nil {
		t.Skipf("mkfifo unavailable: %v", err)
	}
	// Also a normal file so the loop runs.
	_ = os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o600)
	p := scanPrivacy(dir)
	if p["complete"] == true {
		t.Fatalf("fifo must make incomplete: %+v", p)
	}
}
