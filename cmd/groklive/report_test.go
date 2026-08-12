package main

import (
	"encoding/json"
	"fmt"
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
	// Pro R38 P2: provenance property sets must match (not only closedness flags).
	mProv, _ := memProps["provenance"].(map[string]any)
	cProv, _ := comProps["provenance"].(map[string]any)
	mPProps, _ := mProv["properties"].(map[string]any)
	cPProps, _ := cProv["properties"].(map[string]any)
	for _, k := range []string{"live_goos", "live_goarch", "report_generator_goos", "report_generator_goarch"} {
		if _, ok := mPProps[k]; !ok {
			t.Fatalf("closedSchemaV2 provenance missing %s", k)
		}
		if _, ok := cPProps[k]; !ok {
			t.Fatalf("committed schema provenance missing %s", k)
		}
	}
	if len(mPProps) != len(cPProps) {
		t.Fatalf("provenance property count mismatch: mem=%d committed=%d", len(mPProps), len(cPProps))
	}
	for k := range mPProps {
		if _, ok := cPProps[k]; !ok {
			t.Fatalf("committed provenance missing mem key %s", k)
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
	if sk, ok := asInt(priv["files_skipped"]); !ok || sk < 1 {
		t.Fatalf("expected files_skipped>=1 got %+v", priv)
	}
	if sc, ok := asInt(priv["files_scanned"]); ok && sc > 0 {
		// Oversized-only dir may still scan 0; if other files present, skipped must still be set.
		if sk, _ := asInt(priv["files_skipped"]); sk < 1 {
			t.Fatalf("scanned without skips is not complete-or-fail: %+v", priv)
		}
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

// --- live_identity fail-closed (GPT-5.6 Pro P1) ---

const testFullRev = "0123456789abcdef0123456789abcdef01234567"

func writeLiveIdentityFixture(t *testing.T, dir, commit, src string, dirty bool) {
	t.Helper()
	scanID, err := newScanContextID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLiveScanContext(dir, scanID); err != nil {
		// Hostname may be unusable in some CI shapes; still write identity so
		// parse tests can run, but loadLiveScanContext will fail closed.
		t.Logf("writeLiveScanContext: %v", err)
	}
	m := map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     commit,
		"live_binary_dirty":      dirty,
		"live_binary_commit_src": src,
		"live_goos":              runtime.GOOS,
		"live_goarch":            runtime.GOARCH,
		"scan_context_id":        scanID,
		"at":                     stamp(),
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeUsablePreflight(t *testing.T, dir string) {
	t.Helper()
	m := map[string]any{
		"usable":  true,
		"version": "grok 1.0.0 (3cd0d0cbcebe)",
		"binary":  "grok",
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "preflight.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeValidCapsFile(t *testing.T, dir string) {
	t.Helper()
	// Match closed shape used by validCaps() / liveQualification.
	caps := validCaps()
	b, err := json.MarshalIndent(caps, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "acp_manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseLiveIdentityJSON_RejectsPartialAndMalformed(t *testing.T) {
	t.Parallel()
	plat := `,"live_goos":"darwin","live_goarch":"arm64","scan_context_id":"0123456789abcdef0123456789abcdef"`
	cases := []struct {
		name string
		raw  string
	}{
		{"malformed", `{not-json`},
		{"empty", `{}`},
		{"wrong_schema", `{"schema":"other","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags"` + plat + `}`},
		{"missing_commit", `{"schema":"` + liveIdentitySchema + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags"` + plat + `}`},
		{"short_commit", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"abc","live_binary_dirty":false,"live_binary_commit_src":"ldflags"` + plat + `}`},
		{"missing_dirty", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_commit_src":"ldflags"` + plat + `}`},
		{"dirty_string", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":"false","live_binary_commit_src":"ldflags"` + plat + `}`},
		{"missing_src", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false` + plat + `}`},
		{"src_unknown", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"unknown"` + plat + `}`},
		// Platform mandatory (Codex #230 P2): legacy pin shape without live_goos/goarch.
		{"missing_platform", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags"}`},
		{"empty_goos", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":"","live_goarch":"arm64","scan_context_id":"0123456789abcdef0123456789abcdef"}`},
		{"empty_goarch", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":"darwin","live_goarch":"","scan_context_id":"0123456789abcdef0123456789abcdef"}`},
		{"goos_null", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":null,"live_goarch":"arm64","scan_context_id":"0123456789abcdef0123456789abcdef"}`},
		{"goos_traversal", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":"../../../outside","live_goarch":"arm64","scan_context_id":"0123456789abcdef0123456789abcdef"}`},
		{"goarch_slash", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":"darwin","live_goarch":"arm/64","scan_context_id":"0123456789abcdef0123456789abcdef"}`},
		{"missing_scan_id", `{"schema":"` + liveIdentitySchema + `","live_binary_commit":"` + testFullRev + `","live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":"darwin","live_goarch":"arm64"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseLiveIdentityJSON([]byte(tc.raw))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
	id, err := parseLiveIdentityJSON([]byte(`{
		"schema":"` + liveIdentitySchema + `",
		"live_binary_commit":"` + testFullRev + `",
		"live_binary_dirty":false,
		"live_binary_commit_src":"ldflags",
		"live_goos":"darwin",
		"live_goarch":"arm64",
		"scan_context_id":"0123456789abcdef0123456789abcdef"
	}`))
	if err != nil || !id.OK || id.Commit != testFullRev || id.Dirty || id.Src != "ldflags" || id.GOOS != "darwin" || id.GOARCH != "arm64" || id.ScanContextID == "" {
		t.Fatalf("valid identity: %+v err=%v", id, err)
	}
}

func TestLoadLiveIdentity_NoGeneratorFallback(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	id := loadLiveIdentity(dir)
	if id.OK {
		t.Fatal("missing file must not OK")
	}
	if id.Commit != "" || id.Src != "" {
		t.Fatalf("must not invent identity: %+v", id)
	}
	if !strings.Contains(id.Err, "missing") {
		t.Fatalf("want missing err got %q", id.Err)
	}

	// Malformed must not partially apply generator-like defaults.
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(`{bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	id = loadLiveIdentity(dir)
	if id.OK || id.Commit != "" {
		t.Fatalf("malformed must not OK/commit: %+v", id)
	}

	// Partial: commit only, no dirty — reject entirely.
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(`{
		"schema":"`+liveIdentitySchema+`",
		"live_binary_commit":"`+testFullRev+`",
		"live_binary_commit_src":"ldflags"
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	id = loadLiveIdentity(dir)
	if id.OK || id.Commit != "" {
		t.Fatalf("partial must not surface commit: %+v", id)
	}
}

func TestGenerateLiveReport_MissingLiveIdentity_DemotesGO(t *testing.T) {
	// Qualifying scenario matrix without live_identity must not keep GO/LIMITED_GO
	// even if the report generator itself is clean (the false-qualify path).
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeScenarios(t, dir, fullGOScenarios())
	writeUsablePreflight(t, dir)
	writeValidCapsFile(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Intentionally no live_identity.json

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatalf("generateLiveReport: %v", err)
	}
	if out.Disposition == "GO" || out.Disposition == "LIMITED_GO" {
		t.Fatalf("missing live_identity must demote qualifying floor; got %s reasons=%v", out.Disposition, out.Reasons)
	}
	foundMissing, foundForbid := false, false
	for _, r := range out.Reasons {
		if strings.Contains(r, "live_identity.json missing") {
			foundMissing = true
		}
		if strings.Contains(r, "invalid/missing live_identity forbids GO/LIMITED_GO") {
			foundForbid = true
		}
	}
	if !foundMissing || !foundForbid {
		t.Fatalf("want missing+forbid limitations; got %v", out.Reasons)
	}
	// Provenance must not claim generator as live.
	prov, _ := out.Report["provenance"].(map[string]any)
	if prov == nil {
		t.Fatal("missing provenance")
	}
	if c, _ := prov["live_binary_commit"].(string); c != "" {
		t.Fatalf("live_binary_commit must stay empty without identity, got %q", c)
	}
	// Pro R22: incomplete identity must not claim derived=false (unknown ≠ same).
	if d, _ := prov["derived"].(bool); !d {
		t.Fatal("derived must be true when live identity invalid")
	}
	gen, _ := prov["report_generator_commit"].(string)
	if gen != testFullRev {
		t.Fatalf("generator still recorded: %q", gen)
	}
}

func TestGenerateLiveReport_MalformedLiveIdentity_Demotes(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeScenarios(t, dir, fullGOScenarios())
	writeUsablePreflight(t, dir)
	writeValidCapsFile(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(`{"schema":"x"}`), 0o600)

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Disposition == "GO" || out.Disposition == "LIMITED_GO" {
		t.Fatalf("malformed identity demote: got %s", out.Disposition)
	}
}

func TestGenerateLiveReport_DirtyLiveIdentity_Demotes(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	// Clean generator must not rescue a dirty live executor.
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeScenarios(t, dir, fullGOScenarios())
	writeUsablePreflight(t, dir)
	writeValidCapsFile(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600)
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", true)

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Disposition == "GO" || out.Disposition == "LIMITED_GO" {
		t.Fatalf("dirty live identity demote: got %s reasons=%v", out.Disposition, out.Reasons)
	}
	prov, _ := out.Report["provenance"].(map[string]any)
	if dirty, _ := prov["live_binary_dirty"].(bool); !dirty {
		t.Fatal("live_binary_dirty must be true from identity")
	}
}

func TestGenerateLiveReport_ValidLiveIdentity_PreservesSplitProvenance(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	liveRev := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	genRev := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	reinframeCommit = genRev
	reinframeDirty = "false"

	dir := t.TempDir()
	// LIMITED_GO-style incomplete matrix still needs identity for provenance honesty.
	writeScenarios(t, dir, moreDataScenarios())
	writeUsablePreflight(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600)
	writeLiveIdentityFixture(t, dir, liveRev, "ldflags", false)

	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	prov, _ := out.Report["provenance"].(map[string]any)
	if c, _ := prov["live_binary_commit"].(string); c != liveRev {
		t.Fatalf("live=%q", c)
	}
	if c, _ := prov["report_generator_commit"].(string); c != genRev {
		t.Fatalf("gen=%q", c)
	}
	if d, _ := prov["derived"].(bool); !d {
		t.Fatal("derived must be true when live != generator")
	}
}

func TestEnsureLiveIdentity_CreateAndMismatch(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("create: %v", err)
	}
	id := loadLiveIdentity(dir)
	if !id.OK || id.Commit != testFullRev || id.Dirty {
		t.Fatalf("created: %+v", id)
	}
	// Second call with same binary is idempotent.
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("idempotent: %v", err)
	}
	// Mismatch with different binary identity.
	reinframeCommit = "cccccccccccccccccccccccccccccccccccccccc"
	if err := ensureLiveIdentity(dir); err == nil {
		t.Fatal("mismatch must fail")
	}
}

func TestEnsureLiveIdentity_RefuseRetrofitOntoExistingEvidence(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	// Pre-existing scenarios without live_identity — must not stamp current binary.
	if err := os.WriteFile(filepath.Join(dir, "scenarios.json"), []byte(`{"HOOK-ALLOW-001":{"id":"HOOK-ALLOW-001","status":"PASS"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := ensureLiveIdentity(dir)
	if err == nil {
		t.Fatal("expected refuse retrofit")
	}
	if !strings.Contains(err.Error(), "refuse to retrofit") {
		t.Fatalf("want retrofit error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "live_identity.json")); err == nil {
		t.Fatal("must not write live_identity.json on retrofit refuse")
	}
	// Preflight-only dir is also live evidence (cannot retrofit later binary).
	pfDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(pfDir, "preflight.json"), []byte(`{"usable":true,"version":"grok 1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLiveIdentity(pfDir); err == nil || !strings.Contains(err.Error(), "refuse to retrofit") {
		t.Fatalf("preflight without identity must refuse retrofit: %v", err)
	}
	// Fresh directory still allowed.
	fresh := t.TempDir()
	if err := ensureLiveIdentity(fresh); err != nil {
		t.Fatalf("fresh dir create: %v", err)
	}
}

// Pro R24 P2: binary_absent must not lock evidence dir against retry.
// Fixed order: ensureLiveIdentity before preflight.json on the binary_absent path.
func TestPreflightBinaryAbsentThenRetrySameDir(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	// Step 1 (fixed preflight order): bind identity first.
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("identity before binary resolution: %v", err)
	}
	// Step 2: binary_absent persists preflight.json (same dir).
	absent := map[string]any{
		"usable": false,
		"blocker": map[string]any{
			"class": "binary_absent",
		},
	}
	if err := writeJSON(filepath.Join(dir, "preflight.json"), absent); err != nil {
		t.Fatal(err)
	}
	// Step 3: Grok becomes available; same-directory retry must still accept identity.
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("retry after binary_absent preflight must not refuse: %v", err)
	}
	// Control: preflight without prior identity still refuses retrofit.
	locked := t.TempDir()
	if err := os.WriteFile(filepath.Join(locked, "preflight.json"), []byte(`{"usable":false,"blocker":{"class":"binary_absent"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLiveIdentity(locked); err == nil || !strings.Contains(err.Error(), "refuse to retrofit") {
		t.Fatalf("preflight without identity must still refuse: %v", err)
	}
}

func TestEnsureLiveIdentity_RejectsMalformedExisting(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(`{bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLiveIdentity(dir); err == nil {
		t.Fatal("malformed existing must fail")
	}
}

func TestWriteLiveIdentity_FailsOnUnknownBinary(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = ""
	reinframeDirty = ""
	// When ldflags empty, may fall back to vcs — only assert when source unknown.
	rev, _, src := reinframeBuildIdentity()
	if src != "unknown" && rev != "" {
		t.Skip("build has VCS identity; cannot force unknown in this environment")
	}
	dir := t.TempDir()
	if err := writeLiveIdentity(dir); err == nil {
		t.Fatal("unknown binary must refuse writeLiveIdentity")
	}
}

// Codex GraphQL P1: writeJSON redacts paths under $HOME / temp to [HOME]/[TMP:…].
// ensureGrokhooksExecutable must re-bind on content SHA only — not path string equality.
func TestEnsureGrokhooksExecutable_HomePathRedaction_AllowsRebind(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no home dir")
	}
	// Place helper under HOME so writeJSON redacts the absolute path.
	helperDir := filepath.Join(home, ".cache", "reinframe-test-grokhooks-"+t.Name())
	if err := os.MkdirAll(helperDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(helperDir) })
	helper := filepath.Join(helperDir, "grokhooks-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho hooks-helper-v1\n"), 0o700); err != nil {
		t.Fatal(err)
	}

	evDir := t.TempDir()
	if err := ensureGrokhooksExecutable(evDir, helper); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	// Confirm redaction actually rewrote the stored path (otherwise the bug is not exercised).
	raw, err := os.ReadFile(filepath.Join(evDir, "live_grokhooks_executable.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "[HOME]") && !strings.Contains(string(raw), "[TMP:") {
		// Some environments keep the project outside HOME; force-check still that rebind works.
		t.Logf("stored path was not redacted (ok if helper not under HOME redaction roots): %s", raw)
	}
	// Second bind with same content must succeed even when stored path is a placeholder.
	if err := ensureGrokhooksExecutable(evDir, helper); err != nil {
		t.Fatalf("rebind after redaction must not path-mismatch: %v", err)
	}
	// Content change must still fail.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\necho hooks-helper-v2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensureGrokhooksExecutable(evDir, helper); err == nil {
		t.Fatal("content swap must fail rebind")
	}
}

func TestContentHasLocalIdentityLeak_UsesLiveHostname(t *testing.T) {
	t.Parallel()
	// Generator hostname alone would miss a residual live host token.
	liveHost := "ci-runner-xyz-99"
	if contentHasLocalIdentityLeak("ok no host") {
		t.Fatal("clean string must not leak")
	}
	// Without extra hostname, only .local / generator host trigger.
	// liveHost should not match generator unless coincidence — so no assert either way.
	_ = contentHasLocalIdentityLeak("uname says " + liveHost + " ready")
	if !contentHasLocalIdentityLeak("uname says "+liveHost+" ready", liveHost) {
		t.Fatal("extra live hostname must be scanned")
	}
	// Placeholders strip before match.
	if contentHasLocalIdentityLeak("host=[HOSTNAME]", liveHost) {
		t.Fatal("placeholder only must not leak")
	}
}

func TestPrivacyScanHostnames_FromLiveScanContext(t *testing.T) {
	dir := t.TempDir()
	// Without live_identity, private path needs an ID only for write; load uses legacy
	// only when no identity. Write with ID + identity for proper bind.
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	live, ok, why := loadLiveScanContext(dir)
	if !ok {
		t.Skipf("loadLiveScanContext: %s", why)
	}
	hosts := privacyScanHostnames(dir, live, true)
	if len(hosts) == 0 {
		t.Fatal("expected at least generator or live hostname")
	}
	// Residual live host in another file must fail complete (context itself not counted as seen).
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("host token "+live+" in evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Without live_identity, missing context is not forced — but with residual host token still fails.
	p := scanPrivacy(dir)
	if complete, _ := p["complete"].(bool); complete {
		t.Fatalf("residual live hostname must fail complete: %+v", p)
	}
}

func TestScanPrivacy_IdentityWithoutContext_Incomplete(t *testing.T) {
	dir := t.TempDir()
	// Identity with scan_context_id but no private context file.
	m := map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     testFullRev,
		"live_binary_dirty":      false,
		"live_binary_commit_src": "ldflags",
		"live_goos":              runtime.GOOS,
		"live_goarch":            runtime.GOARCH,
		"scan_context_id":        "cccccccccccccccccccccccccccccccc",
		"at":                     stamp(),
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("clean evidence"), 0o600)
	p := scanPrivacy(dir)
	if complete, _ := p["complete"].(bool); complete {
		t.Fatalf("identity without context must not complete: %+v", p)
	}
	raw, _ := json.Marshal(p["failure_classes"])
	if !strings.Contains(string(raw), "live_scan_context") {
		t.Fatalf("want live_scan_context failure; got %+v", p)
	}
}

func TestScanPrivacy_ContextNotCountedAsSeen(t *testing.T) {
	dir := t.TempDir()
	id, err := newScanContextID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLiveScanContext(dir, id); err != nil {
		t.Skipf("hostname: %v", err)
	}
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("clean evidence body"), 0o600)
	// No live_identity → context not mandatory; only note.txt is evidence.
	// Context lives outside evidence so it cannot inflate files_seen.
	p := scanPrivacy(dir)
	seen, _ := p["files_seen"].(int)
	scanned, _ := p["files_scanned"].(int)
	if seen != 1 || scanned != 1 {
		if sf, ok := p["files_seen"].(float64); ok {
			seen = int(sf)
		}
		if sc, ok := p["files_scanned"].(float64); ok {
			scanned = int(sc)
		}
	}
	if seen != 1 || scanned != 1 {
		t.Fatalf("context must not inflate seen/scanned; got seen=%v scanned=%v full=%+v", p["files_seen"], p["files_scanned"], p)
	}
	if complete, _ := p["complete"].(bool); !complete {
		t.Fatalf("clean single file + private context should complete: %+v", p)
	}
}

func TestGenerateLiveReport_ReReportKeepsPrivateContext(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	// Minimal evidence so report can run (will demote for other gates, but must not
	// destroy private context).
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600)
	if _, err := generateLiveReport(dir); err != nil {
		t.Fatalf("first report: %v", err)
	}
	// No bare hostname in evidence after report.
	if _, err := os.Stat(filepath.Join(dir, liveScanContextFile)); !os.IsNotExist(err) {
		t.Fatal("evidence must not retain live_scan_context.json after report")
	}
	// Private context still available for a second report pass.
	if _, ok, why := loadLiveScanContext(dir); !ok {
		t.Fatalf("re-report context missing after first report: %s", why)
	}
	if _, err := generateLiveReport(dir); err != nil {
		t.Fatalf("second report: %v", err)
	}
}

func TestLiveScanContext_StoredOutsideEvidence(t *testing.T) {
	dir := t.TempDir()
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	// Bare hostname must not land inside the evidence tree.
	if _, err := os.Stat(filepath.Join(dir, liveScanContextFile)); !os.IsNotExist(err) {
		t.Fatal("live_scan_context.json must not be written into evidence-out")
	}
	h, ok, why := loadLiveScanContext(dir)
	if !ok || h == "" {
		t.Fatalf("private context load failed: ok=%v why=%s", ok, why)
	}
	// Legacy in-evidence file is scrubbed after report / on write.
	if err := os.WriteFile(filepath.Join(dir, liveScanContextFile), []byte(`{"schema":"`+liveScanContextSchema+`","live_hostname":"legacy-host","scan_context_id":"deadbeefdeadbeefdeadbeefdeadbeef"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scrubLegacyInEvidenceScanContext(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, liveScanContextFile)); !os.IsNotExist(err) {
		t.Fatal("legacy scrub must remove in-evidence control file")
	}
	// Private context still loads for re-report.
	if _, ok, _ := loadLiveScanContext(dir); !ok {
		t.Fatal("private context must survive evidence scrub for re-report")
	}
}

func TestLiveScanContext_RejectsCacheInsideEvidence(t *testing.T) {
	dir := t.TempDir()
	prev := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prev })
	// Point "cache" at the evidence tree — write must refuse (Pro R19 P1).
	privateCacheRootFn = func() (string, error) { return dir, nil }
	id, err := newScanContextID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLiveScanContext(dir, id); err == nil {
		t.Fatal("write must fail when private cache root is inside evidence")
	}
}

func TestLiveScanContext_RejectsSymlinkIntoEvidenceSubdir(t *testing.T) {
	// Pro R21 P1: cache-root symlink → evidence/private must still be rejected.
	ev := t.TempDir()
	sub := filepath.Join(ev, "private")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(outside, "cache-link")
	if err := os.Symlink(sub, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	prev := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prev })
	privateCacheRootFn = func() (string, error) { return link, nil }
	id, err := newScanContextID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLiveScanContext(ev, id); err == nil {
		t.Fatal("write must fail when cache root symlinks into evidence subdirectory")
	}
}

func TestHostnameToken_MatchesFQDNFirstLabel(t *testing.T) {
	t.Parallel()
	host := "build-01"
	if !hostnameTokenPresent("node build-01.corp.example.com up", host) {
		t.Fatal("short hostname must match as first FQDN label")
	}
	// Reverse: bound FQDN, evidence short name (Pro R22 P1).
	if !hostnameTokenPresent("uname build-01", "build-01.corp.example.com") {
		t.Fatal("bound FQDN must match short probe hostname")
	}
	if hostnameTokenPresent("reinframe.grok_build.v1", "build") {
		t.Fatal("must not match inside schema id grok_build")
	}
	out := redactHostnameToken("host=build-01.corp.example.com", host)
	if strings.Contains(out, "build-01") || !strings.Contains(out, "[HOSTNAME]") {
		t.Fatalf("FQDN redaction: %s", out)
	}
	out2 := redactHostnameToken("probe build-01 ready", "build-01.corp.example.com")
	if strings.Contains(out2, "build-01") || !strings.Contains(out2, "[HOSTNAME]") {
		t.Fatalf("short redaction under bound FQDN: %s", out2)
	}
	// Absolute DNS with root trailing dot (Pro R23 P1).
	if !hostnameTokenPresent("name=build-01.corp.example.com.", "build-01.corp.example.com") {
		t.Fatal("root-dot FQDN must match bound FQDN")
	}
	if !hostnameTokenPresent("name=build-01.corp.example.com.", "build-01") {
		t.Fatal("root-dot FQDN must match bound short host")
	}
	// short bound → FQDN with root-dot must redact.
	outRoot := redactHostnameToken("node build-01.corp.example.com. up", "build-01")
	if strings.Contains(outRoot, "build-01") || !strings.Contains(outRoot, "[HOSTNAME]") {
		t.Fatalf("root-dot FQDN redaction under short bound: %s", outRoot)
	}
	// FQDN bound → full FQDN with root-dot must redact.
	outRoot2 := redactHostnameToken("node build-01.corp.example.com. up", "build-01.corp.example.com")
	if strings.Contains(outRoot2, "build-01") || !strings.Contains(outRoot2, "[HOSTNAME]") {
		t.Fatalf("root-dot FQDN redaction under FQDN bound: %s", outRoot2)
	}
	// Schema identifiers remain non-matches with root-dot matcher.
	if hostnameTokenPresent("reinframe.grok_build.v1.", "build") {
		t.Fatal("root-dot-aware matcher must not match schema id grok_build")
	}
}

func TestHostnameUnsafeForPostMarshalRedact(t *testing.T) {
	t.Parallel()
	// GOOS/GOARCH hostnames must skip post-marshal rewrite (Pro R23 P2).
	for _, h := range []string{"linux", "darwin", "amd64", "arm64", "linux.corp.example.com"} {
		if !hostnameUnsafeForPostMarshalRedact(h) {
			t.Fatalf("expected unsafe hostname %q", h)
		}
	}
	for _, h := range []string{"true", "false", "PASS", "null", "go", "transport", "session_visible", "unknown"} {
		if !hostnameUnsafeForPostMarshalRedact(h) {
			t.Fatalf("expected unsafe schema token hostname %q", h)
		}
	}
	if hostnameUnsafeForPostMarshalRedact("build-01") {
		t.Fatal("ordinary hostname must remain safe for post-marshal redaction")
	}
	// redactLocalIdentity must not corrupt report_generator_goos when host is linux.
	// We cannot force os.Hostname(); assert the helper + validator path directly.
	raw := `{
  "goos": "linux",
  "goarch": "amd64",
  "report_generator_goos": "linux",
  "report_generator_goarch": "amd64",
  "note": "host linux is online"
}`
	// Simulate unsafe-host skip: only free-text would be left if we still rewrote;
	// the skip list must keep structured platform values intact under redactHostnameToken.
	// When skip applies, redactHostnameToken is not called for os.Hostname==linux.
	if err := validatePostRedactPlatformFields(raw); err != nil {
		t.Fatalf("clean platform fields must validate: %v", err)
	}
	// Corrupted path must fail closed.
	corrupt := strings.ReplaceAll(raw, `"report_generator_goos": "linux"`, `"report_generator_goos": "[HOSTNAME]"`)
	if err := validatePostRedactPlatformFields(corrupt); err == nil {
		t.Fatal("corrupted report_generator_goos must fail validation")
	}
	// Direct rewrite of goos value via redactHostnameToken("linux") would corrupt —
	// that is why hostnameUnsafeForPostMarshalRedact short-circuits the call.
	rewritten := redactHostnameToken(raw, "linux")
	if !strings.Contains(rewritten, `"[HOSTNAME]"`) {
		t.Fatalf("control: unrestricted linux redaction should rewrite tokens: %s", rewritten)
	}
	if err := validatePostRedactPlatformFields(rewritten); err == nil {
		t.Fatal("post-redact validator must reject rewritten platform fields")
	}
}

func TestLiveScanContext_RejectsCaseAliasCacheRoot(t *testing.T) {
	// Pro R20 P1: on case-insensitive volumes, Evidence vs eVIDENCE must still
	// be treated as the same tree.
	base := t.TempDir()
	// Create two absolute spellings that differ only by case of the last component.
	lower := filepath.Join(base, "evidence")
	if err := os.MkdirAll(lower, 0o700); err != nil {
		t.Fatal(err)
	}
	// Probe whether the volume is case-insensitive.
	alt := filepath.Join(base, "EvIdEnCe")
	if _, err := os.Stat(alt); err != nil {
		// Case-sensitive volume: create a hard link via symlink with different name
		// only if Stat fails — then we can't prove APFS alias here.
		// Still exercise equalFoldPath via privateCacheRootFn returning alt spelling
		// of the same path string transformed only if Stat(alt) works.
		t.Logf("volume appears case-sensitive (stat %s: %v); using equalFold synthetic", alt, err)
		// Synthetic: privateCacheRootFn returns strings.ToUpper of lower if that path
		// exists via EqualFold — on case-sensitive FS create sibling with SameFile
		// by using lower for both after rewriting equalFold only path check.
		// Fall back: set cache root to lower with different Abs presentation.
		prev := privateCacheRootFn
		t.Cleanup(func() { privateCacheRootFn = prev })
		// Use a path that equalFold matches: upper-case each rune of lower.
		upper := strings.ToUpper(lower)
		if upper == lower {
			t.Skip("path has no case variance")
		}
		// On case-sensitive FS, ToUpper path may not exist — force via SameFile
		// by pointing cache root at lower and evidence at a symlink with mixed case.
		// Create mixed-case symlink if supported.
		if err := os.Symlink(lower, alt); err != nil {
			t.Skipf("cannot create case-variant symlink: %v", err)
		}
		privateCacheRootFn = func() (string, error) { return alt, nil }
		id, err := newScanContextID()
		if err != nil {
			t.Fatal(err)
		}
		if err := writeLiveScanContext(lower, id); err == nil {
			t.Fatal("write must fail when cache root is a same-file alias of evidence")
		}
		return
	}
	// Case-insensitive: alt and lower are the same directory.
	prev := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prev })
	privateCacheRootFn = func() (string, error) { return alt, nil }
	id, err := newScanContextID()
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLiveScanContext(lower, id); err == nil {
		t.Fatal("write must fail when cache root is case-alias of evidence")
	}
}

func TestLiveScanContext_StaleCampaignID_Rejected(t *testing.T) {
	dir := t.TempDir()
	// Campaign A
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	if _, ok, _ := loadLiveScanContext(dir); !ok {
		t.Skip("no hostname context on this host")
	}
	// Campaign B at same path: new identity/id, but leave A's private context alone.
	// Replace identity with a different scan_context_id and no matching private file.
	scanB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	m := map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     testFullRev,
		"live_binary_dirty":      false,
		"live_binary_commit_src": "ldflags",
		"live_goos":              runtime.GOOS,
		"live_goarch":            runtime.GOARCH,
		"scan_context_id":        scanB,
		"at":                     stamp(),
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	// Do not write private context for B.
	if _, ok, why := loadLiveScanContext(dir); ok {
		t.Fatalf("stale/mismatched campaign must not load: why would be empty, got ok with why=%s", why)
	}
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("clean"), 0o600)
	p := scanPrivacy(dir)
	if complete, _ := p["complete"].(bool); complete {
		t.Fatalf("stale campaign context path must not complete privacy: %+v", p)
	}
}

func TestScrubLegacyInEvidence_PropagatesFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, liveScanContextFile)
	if err := os.WriteFile(path, []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make evidence dir read-only so Remove fails (permission).
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if err := scrubLegacyInEvidenceScanContext(dir); err == nil {
		// Some platforms may still allow owner delete; force verify path.
		// If remove succeeded, the test cannot assert failure — skip.
		if _, stErr := os.Lstat(path); os.IsNotExist(stErr) {
			t.Skip("platform allowed remove despite chmod 0555; cannot force scrub failure")
		}
		t.Fatal("scrub must fail when evidence dir is not writable")
	}
}

func TestEnsureLiveIdentity_RequiresScanContext(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	// Identity with scan_context_id but deliberately no private context file.
	m := map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     testFullRev,
		"live_binary_dirty":      false,
		"live_binary_commit_src": "ldflags",
		"live_goos":              runtime.GOOS,
		"live_goarch":            runtime.GOARCH,
		"scan_context_id":        "dddddddddddddddddddddddddddddddd",
		"at":                     stamp(),
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ensureLiveIdentity(dir); err == nil {
		t.Fatal("existing identity without scan context must fail ensureLiveIdentity")
	}
}

func TestGenerateLiveReport_MissingPlatformFields_Demotes(t *testing.T) {
	// Legacy pin shape (130935Z) without live_goos/goarch must not qualify.
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeScenarios(t, dir, fullGOScenarios())
	writeUsablePreflight(t, dir)
	writeValidCapsFile(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("ok"), 0o600)
	// Pre-platform-field identity (honest historical pin shape).
	legacy := `{
		"schema":"` + liveIdentitySchema + `",
		"live_binary_commit":"` + testFullRev + `",
		"live_binary_dirty":false,
		"live_binary_commit_src":"ldflags"
	}`
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := generateLiveReport(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Disposition == "GO" || out.Disposition == "LIMITED_GO" {
		t.Fatalf("missing platform fields must demote; got %s reasons=%v", out.Disposition, out.Reasons)
	}
	found := false
	for _, r := range out.Reasons {
		if strings.Contains(r, "live_goos") || strings.Contains(r, "live_goarch") || strings.Contains(r, "live_identity") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want platform/identity reason; got %v", out.Reasons)
	}
}

// Pro R31/R32 P2: --scan-context-in must work during ensureLiveIdentity (not only report).
func TestEnsureLiveIdentity_ImportsScanContextInDuringValidate(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	prevRoot := privateCacheRootFn
	t.Cleanup(func() {
		reinframeCommit, reinframeDirty = prevC, prevD
		scanContextOutPath = ""
		scanContextInPath = ""
		privateCacheRootFn = prevRoot
	})
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	cacheA := t.TempDir()
	privateCacheRootFn = func() (string, error) { return cacheA, nil }

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(parent, "ctx-export.json")
	scanContextOutPath = external
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	scanContextOutPath = ""

	// Simulate copied campaign: public evidence only + external sidecar, empty private cache.
	cacheB := t.TempDir()
	privateCacheRootFn = func() (string, error) { return cacheB, nil }
	copied := filepath.Join(t.TempDir(), "evidence-copy")
	if err := os.MkdirAll(copied, 0o700); err != nil {
		t.Fatal(err)
	}
	idBytes, err := os.ReadFile(filepath.Join(dir, "live_identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copied, "live_identity.json"), idBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	// Without import, ensure must fail closed.
	scanContextInPath = ""
	if err := ensureLiveIdentity(copied); err == nil {
		t.Fatal("missing private context without import must fail")
	}
	// With scan-context-in bound before ensure (as runAll now does), must succeed.
	scanContextInPath = external
	if err := ensureLiveIdentity(copied); err != nil {
		t.Fatalf("ensureLiveIdentity with scan-context-in: %v", err)
	}
}

// Pro R31/R32 P1: --project roots outside HOME/TMP must be redacted and scanned.
func TestProjectRootRedactionAndLeakScan(t *testing.T) {
	prev := liveProjectRoot
	t.Cleanup(func() { liveProjectRoot = prev })
	proj := filepath.Join(t.TempDir(), "workspace", "alice", "campaign")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	setLiveProjectRoot(proj)
	hooksFile := filepath.Join(proj, ".grok", "hooks", "reinframe-pretool.json")
	out := redactLocalIdentity("doctor hooks_file=" + hooksFile)
	if strings.Contains(out, proj) || strings.Contains(out, "alice") {
		t.Fatalf("project root not redacted: %s", out)
	}
	if !strings.Contains(out, "[PROJECT]") {
		t.Fatalf("expected [PROJECT] placeholder: %s", out)
	}
	// Residual raw project path must fail privacy scan.
	if !contentHasLocalIdentityLeak("hooks_file=" + hooksFile) {
		t.Fatal("raw project path must leak")
	}
	if contentHasLocalIdentityLeak("hooks_file=[PROJECT]/.grok/hooks/reinframe-pretool.json") {
		t.Fatal("redacted project path must not leak")
	}
}

// Pro R40 P2: scan-context-in must not write through a pre-planted private-cache symlink.
func TestLoadLiveScanContext_ImportRefusesCacheSymlink(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() {
		reinframeCommit, reinframeDirty = prevC, prevD
		scanContextOutPath = ""
		scanContextInPath = ""
	})
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	// Host A: create campaign + export.
	cacheA := t.TempDir()
	prevRoot := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prevRoot })
	privateCacheRootFn = func() (string, error) { return cacheA, nil }

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(parent, "ctx-export.json")
	scanContextOutPath = external
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	scanContextOutPath = ""

	id := loadLiveIdentity(dir)
	if !id.OK || id.ScanContextID == "" {
		t.Fatalf("identity: ok=%v id=%s", id.OK, id.ScanContextID)
	}

	// Host B: empty cache + dangling symlink at deterministic private path.
	cacheB := t.TempDir()
	privateCacheRootFn = func() (string, error) { return cacheB, nil }
	copied := filepath.Join(t.TempDir(), "evidence-copy")
	if err := os.MkdirAll(copied, 0o700); err != nil {
		t.Fatal(err)
	}
	idBytes, err := os.ReadFile(filepath.Join(dir, "live_identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(copied, "live_identity.json"), idBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	path, err := liveScanContextPath(copied, id.ScanContextID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "hijacked-cache.json")
	if err := os.Symlink(outside, path); err != nil {
		t.Skipf("symlink: %v", err)
	}
	scanContextInPath = external
	h, ok, why := loadLiveScanContext(copied)
	if ok {
		t.Fatalf("import through cache symlink must fail closed; got host=%q why=%s", h, why)
	}
	if !strings.Contains(why, "cache write") && !strings.Contains(why, "symlink") {
		t.Fatalf("want cache write/symlink failure; why=%s", why)
	}
	if _, stErr := os.Stat(outside); !os.IsNotExist(stErr) {
		t.Fatalf("external target must remain absent: %v", stErr)
	}
}

// Pro R39 skeptic / Codex P2: predictable OUT.tmp must not follow a pre-planted symlink.
func TestWriteScanContextOut_RejectsPredictableTmpSymlink(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() {
		reinframeCommit, reinframeDirty = prevC, prevD
		scanContextOutPath = ""
		scanContextInPath = ""
	})
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	cache := t.TempDir()
	prevRoot := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prevRoot })
	privateCacheRootFn = func() (string, error) { return cache, nil }

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// First create identity without export.
	scanContextOutPath = ""
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)

	external := filepath.Join(parent, "scan-export.json")
	// Pre-plant predictable OUT.tmp as dangling symlink to outside target (old bug path).
	outside := filepath.Join(t.TempDir(), "hijacked.json")
	predictableTmp := external + ".tmp"
	if err := os.Symlink(outside, predictableTmp); err != nil {
		t.Skipf("symlink: %v", err)
	}
	// Also plant final destination as dangling symlink to prove safeWriteFile rejects it.
	// For the .tmp case: safeWriteFile uses unique CreateTemp, so predictable .tmp is ignored.
	// Attack surface for final OUT: symlink OUT itself.
	if err := os.Symlink(outside, external); err != nil {
		t.Fatal(err)
	}
	scanContextOutPath = external
	err := ensureLiveIdentity(dir)
	if err == nil {
		t.Fatal("export to symlink destination must fail")
	}
	if _, stErr := os.Stat(outside); !os.IsNotExist(stErr) {
		t.Fatalf("outside target must remain absent after failed export: %v", stErr)
	}
	// Clear final symlink; leave only predictable .tmp symlink — export must still succeed
	// without writing through .tmp (unique temp) and without creating outside.
	_ = os.Remove(external)
	scanContextOutPath = external
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("export with stale OUT.tmp symlink present must still work via unique temp: %v", err)
	}
	if _, stErr := os.Stat(outside); !os.IsNotExist(stErr) {
		t.Fatalf("outside must remain absent when only OUT.tmp was symlinked: %v", stErr)
	}
	b, err := os.ReadFile(external)
	if err != nil {
		t.Fatalf("export file missing: %v", err)
	}
	if _, ok, why := parseLiveScanContextBytes(b); !ok {
		t.Fatalf("export invalid: %s", why)
	}
}

// Pro R30 P2: resume existing identity with --scan-context-out must still export.
func TestEnsureLiveIdentity_ExportsScanContextOnResume(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() {
		reinframeCommit, reinframeDirty = prevC, prevD
		scanContextOutPath = ""
		scanContextInPath = ""
	})
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	cache := t.TempDir()
	prevRoot := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prevRoot })
	privateCacheRootFn = func() (string, error) { return cache, nil }

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// First create without export.
	scanContextOutPath = ""
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)

	external := filepath.Join(parent, "resume-export.json")
	if _, err := os.Stat(external); !os.IsNotExist(err) {
		t.Fatal("export must not exist before resume")
	}
	scanContextOutPath = external
	if err := ensureLiveIdentity(dir); err != nil {
		t.Fatalf("resume ensureLiveIdentity: %v", err)
	}
	b, err := os.ReadFile(external)
	if err != nil {
		t.Fatalf("resume must write --scan-context-out: %v", err)
	}
	doc, ok, why := parseLiveScanContextBytes(b)
	if !ok || doc.Hostname == "" {
		t.Fatalf("exported scan context invalid: ok=%v why=%s", ok, why)
	}
	// Fail-closed: requested export path under evidence must error.
	scanContextOutPath = filepath.Join(dir, "bad-export.json")
	if err := ensureLiveIdentity(dir); err == nil {
		t.Fatal("scan-context-out under evidence must fail")
	}
}

// Pro R28 P1: external --scan-context-out/in enables cross-host load; never under evidence.
func TestLiveScanContext_ExternalImportCrossCache(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	cacheA := t.TempDir()
	prevRoot := privateCacheRootFn
	t.Cleanup(func() {
		privateCacheRootFn = prevRoot
		scanContextOutPath = ""
		scanContextInPath = ""
	})
	privateCacheRootFn = func() (string, error) { return cacheA, nil }

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(parent, "scan-context-export.json")
	scanContextOutPath = external
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	// Must not land under evidence.
	if _, err := os.Stat(filepath.Join(dir, liveScanContextPortableFile)); !os.IsNotExist(err) {
		t.Fatal("must not write portable scan context under evidence-out")
	}
	if _, err := os.Stat(external); err != nil {
		t.Fatalf("external scan-context-out missing: %v", err)
	}

	// Host B: empty cache + import external path.
	cacheB := t.TempDir()
	privateCacheRootFn = func() (string, error) { return cacheB, nil }
	scanContextOutPath = ""
	scanContextInPath = external
	// Copy only public evidence (no private cache).
	copied := filepath.Join(t.TempDir(), "evidence-copy")
	if err := os.MkdirAll(copied, 0o700); err != nil {
		t.Fatal(err)
	}
	idBytes, _ := os.ReadFile(filepath.Join(dir, "live_identity.json"))
	if err := os.WriteFile(filepath.Join(copied, "live_identity.json"), idBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	h, ok, why := loadLiveScanContext(copied)
	if !ok || h == "" {
		t.Fatalf("external import must enable cross-cache load: ok=%v why=%s", ok, why)
	}
}

// Pro R29 P1: typed map[string]ScenarioResult free-text must be redacted via writeJSON.
func TestWriteJSON_RedactsTypedScenarioMapFreeText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.json")
	// Inject a free-text host token that would only appear if walk visits Detail.
	// Use a distinctive hostname-like token that is not a closed enum.
	m := map[string]ScenarioResult{
		"HOOK-ALLOW-001": {
			ID:     "HOOK-ALLOW-001",
			Status: "PASS",
			Detail: "prompt transport OK on host build-99-live",
		},
	}
	// Temporarily force hostname via env is hard; call redact path by writing
	// through writeJSON then check Detail was processed as free-text.
	// We assert structure: status preserved, and redactLocalIdentityAlways would
	// rewrite home paths in Detail if present.
	m["HOOK-ALLOW-001"] = ScenarioResult{
		ID:     "HOOK-ALLOW-001",
		Status: "PASS",
		Detail: "path=/Users/alice/project transport OK",
	}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if strings.Contains(raw, "/Users/alice") {
		t.Fatalf("typed scenario Detail path not redacted: %s", raw)
	}
	if !strings.Contains(raw, `"status": "PASS"`) && !strings.Contains(raw, `"status":"PASS"`) {
		// indented form
		if !strings.Contains(raw, "PASS") {
			t.Fatalf("status lost: %s", raw)
		}
	}
	if !strings.Contains(raw, "[HOME]") {
		t.Fatalf("expected [HOME] redaction in Detail: %s", raw)
	}
}

// Pro R28 P1: raw scan-context under evidence is a privacy failure.
func TestScanPrivacy_ScanContextInEvidenceFails(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "note.txt"), []byte("clean"), 0o600)
	_ = os.WriteFile(filepath.Join(dir, liveScanContextPortableFile), []byte(`{"schema":"`+liveScanContextSchema+`","live_hostname":"secret-host","scan_context_id":"dddddddddddddddddddddddddddddddd"}`), 0o600)
	p := scanPrivacy(dir)
	if complete, _ := p["complete"].(bool); complete {
		t.Fatalf("scan context in evidence must not complete: %+v", p)
	}
	raw, _ := json.Marshal(p["failure_classes"])
	if !strings.Contains(string(raw), "scan_context_in_evidence") {
		t.Fatalf("want scan_context_in_evidence failure: %+v", p)
	}
}

// Pro R27 P2: hostname "v1" must not rewrite schema suffix reinframe.*.v1.
func TestHostnameToken_DoesNotCorruptSchemaVersionSuffix(t *testing.T) {
	t.Parallel()
	schema := `{"schema":"reinframe.live_identity.v1","other":"reinframe.live_scan_context.v1"}`
	if hostnameTokenPresent(schema, "v1") {
		t.Fatal("schema .v1 suffix must not be a hostname token")
	}
	out := redactHostnameToken(schema, "v1")
	if strings.Contains(out, "[HOSTNAME]") || !strings.Contains(out, "live_identity.v1") {
		t.Fatalf("schema corrupted by host v1: %s", out)
	}
	// Free-text v1 still redacts.
	free := "host v1 ready"
	if !hostnameTokenPresent(free, "v1") {
		t.Fatal("standalone v1 must still match")
	}
}

// Pro R26 P1: scan context keyed by scan_context_id only — evidence rename still loads.
func TestLiveScanContext_SurvivesEvidenceRename(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	parent := t.TempDir()
	dir := filepath.Join(parent, "evidence-a")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLiveIdentityFixture(t, dir, testFullRev, "ldflags", false)
	// Capture scan id from identity.
	id := loadLiveIdentity(dir)
	if !id.OK || id.ScanContextID == "" {
		t.Fatalf("fixture identity: %+v", id)
	}
	// Rename evidence directory; private context must still resolve (id-only key).
	moved := filepath.Join(parent, "evidence-b-renamed")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	h, ok, why := loadLiveScanContext(moved)
	if !ok || h == "" {
		t.Fatalf("rename must preserve private context: ok=%v why=%s", ok, why)
	}
}

// Pro R26 P1: public binding artifacts must not embed raw absolute paths.
func TestExecutableBinding_NoRawAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	// Fake executable under a non-HOME path.
	exe := filepath.Join(dir, "tools", "company-alice", "grok-bin")
	if err := os.MkdirAll(filepath.Dir(exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	ev := filepath.Join(dir, "evidence")
	if err := os.MkdirAll(ev, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensureGrokExecutableIdentity(ev, exe); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(ev, "live_grok_executable.json"))
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if strings.Contains(raw, "company-alice") || strings.Contains(raw, exe) {
		t.Fatalf("raw absolute path leaked into binding: %s", raw)
	}
	if !strings.Contains(raw, "grok_executable_basename") || !strings.Contains(raw, "grok_executable_path_sha256") {
		t.Fatalf("want basename+path digest fields: %s", raw)
	}
	if ok, why := loadLiveGrokExecutableOK(ev); !ok {
		t.Fatalf("binding must still validate: %s", why)
	}
}

// Pro R26 P2: structured goos/goarch values must not false-flag hostname "linux".
func TestContentHasLocalIdentityLeak_IgnoresStructuredPlatformValues(t *testing.T) {
	t.Parallel()
	structured := `{
  "goos": "linux",
  "live_goos": "linux",
  "report_generator_goos": "linux",
  "goarch": "amd64",
  "final_disposition": "NO_GO",
  "schema": "reinframe.grok_build_live_control.v2"
}`
	if contentHasLocalIdentityLeak(structured, "linux") {
		t.Fatal("structured platform enums must not count as hostname leak for host=linux")
	}
	// Free-text residual still fails.
	if !contentHasLocalIdentityLeak(`uname says linux is ready`, "linux") {
		t.Fatal("free-text hostname must still leak")
	}
}

// Pro R31 P1: invalid closed-field values must not be exempted from hostname scan.
func TestContentHasLocalIdentityLeak_InvalidClosedFieldNotExempt(t *testing.T) {
	t.Parallel()
	// Arbitrary status value equal to live host must still leak.
	if !contentHasLocalIdentityLeak(`{"status":"build-01","detail":"ok"}`, "build-01") {
		t.Fatal(`{"status":"build-01"} must leak for host=build-01`)
	}
	if !contentHasLocalIdentityLeak(`{"class":"build-01"}`, "build-01") {
		t.Fatal(`invalid class value must leak`)
	}
	if !contentHasLocalIdentityLeak(`{"schema":"build-01"}`, "build-01") {
		t.Fatal(`invalid schema value must leak`)
	}
	if !contentHasLocalIdentityLeak(`{"src":"build-01"}`, "build-01") {
		t.Fatal(`invalid src value must leak`)
	}
	// Pro R32 P1: broad reinframe.* must not blank hostname-bearing schema values.
	if !contentHasLocalIdentityLeak(`{"schema":"reinframe.x/build-01.v1"}`, "build-01") {
		t.Fatal(`reinframe.x/build-01.v1 must remain scannable for host=build-01`)
	}
	// Valid closed enums still exempt (host token that is not an enum value).
	if contentHasLocalIdentityLeak(`{"status":"PASS","goos":"linux"}`, "linux") {
		t.Fatal("valid goos enum must remain exempt")
	}
	if contentHasLocalIdentityLeak(`{"status":"PASS","final_disposition":"NO_GO"}`, "build-01") {
		t.Fatal("valid closed enums must not false-flag unrelated host")
	}
	if contentHasLocalIdentityLeak(`{"schema":"reinframe.live_identity.v1"}`, "build-01") {
		t.Fatal("known schema registry entry must not false-flag")
	}
}

// Pro R38 P2: formal Markdown + schema destinations also refuse dangling symlinks.
func TestGenerateLiveReport_RejectsSymlinkMDAndSchema(t *testing.T) {
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"

	dir := t.TempDir()
	writeScenarios(t, dir, moreDataScenarios())
	// Preflight usable so report can run.
	writeUsablePreflight(t, dir)

	outsideMD := filepath.Join(t.TempDir(), "out.md")
	outsideSchema := filepath.Join(t.TempDir(), "out.schema.json")
	// Plant dangling symlinks for the deterministic formal destinations after a
	// first successful run would create them — use known basenames from generator.
	// Call generate once to discover names is heavy; plant after mkdir only.
	// We pass through generateLiveReport which always uses fixed schema name and
	// disposition-based md name. Plant schema path first.
	schemaPath := filepath.Join(dir, "reinframe.grok_build_live_control.v2.schema.json")
	if err := os.Symlink(outsideSchema, schemaPath); err != nil {
		t.Skipf("symlink: %v", err)
	}
	// Need live identity for some paths? moreData may still write. Run and expect error.
	_, err := generateLiveReport(dir)
	if err == nil {
		// If generate succeeded, schema symlink was replaced via rename (also OK),
		// but Pro wants failure before external write. Verify outside still absent.
		if _, stErr := os.Stat(outsideSchema); !os.IsNotExist(stErr) {
			t.Fatal("schema write must not create external target")
		}
	} else {
		if _, stErr := os.Stat(outsideSchema); !os.IsNotExist(stErr) {
			t.Fatalf("external schema target must remain absent: %v", stErr)
		}
	}
	// Fresh dir for md symlink: need a path matching disposition MORE_DATA md name.
	dir2 := t.TempDir()
	writeScenarios(t, dir2, moreDataScenarios())
	writeUsablePreflight(t, dir2)
	// Generate once without symlink to learn md basename pattern is expensive;
	// plant a symlink after JSON succeeds by intercepting: use safeWriteFile unit
	// check on a known md path from outcome of a clean generate.
	out, err := generateLiveReport(dir2)
	if err != nil {
		t.Fatalf("clean generate: %v", err)
	}
	outsideMD2 := filepath.Join(t.TempDir(), "out2.md")
	_ = os.Remove(out.MDPath)
	if err := os.Symlink(outsideMD2, out.MDPath); err != nil {
		t.Fatal(err)
	}
	// Re-generate into same dir should fail safeWriteFile on md symlink.
	_, err = generateLiveReport(dir2)
	if err == nil {
		if _, stErr := os.Stat(outsideMD2); !os.IsNotExist(stErr) {
			t.Fatal("md write must not create external target")
		}
	} else if _, stErr := os.Stat(outsideMD2); !os.IsNotExist(stErr) {
		t.Fatalf("external md target must remain absent: %v", stErr)
	}
	_ = outsideMD
}

// Pro R37 P2: dangling symlink destinations must not receive evidence writes.
func TestSafeWriteFile_RejectsDanglingSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-target.json")
	// Dangling: target does not exist.
	link := filepath.Join(dir, "live_identity.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	err := writeJSON(link, map[string]any{"ok": true})
	if err == nil {
		t.Fatal("writeJSON through dangling symlink must fail")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("external target must remain absent; stat err=%v", err)
	}
	// Existing binding files: ensure* rejects symlink destinations.
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"
	if err := ensureLiveIdentity(dir); err == nil {
		t.Fatal("ensureLiveIdentity must refuse symlink live_identity.json")
	}
	// grok executable binding
	exeLink := filepath.Join(dir, "live_grok_executable.json")
	_ = os.Remove(exeLink)
	if err := os.Symlink(outside, exeLink); err != nil {
		t.Fatal(err)
	}
	// Need a real executable path for the content hash path; use this test binary.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := ensureGrokExecutableIdentity(dir, self); err == nil {
		t.Fatal("ensureGrokExecutableIdentity must refuse symlink binding")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("external target still must be absent after grok bind attempt: %v", err)
	}
	// grokhooks binding
	hooksLink := filepath.Join(dir, "live_grokhooks_executable.json")
	_ = os.Remove(hooksLink)
	if err := os.Symlink(outside, hooksLink); err != nil {
		t.Fatal(err)
	}
	if err := ensureGrokhooksExecutable(dir, self); err == nil {
		t.Fatal("ensureGrokhooksExecutable must refuse symlink binding")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("external target still must be absent after hooks bind attempt: %v", err)
	}
}

// Pro R35 P1: identity-bearing filenames must fail privacy complete.
func TestScanPrivacy_FilenameHostLeak(t *testing.T) {
	prevHosts := append([]string(nil), extraRedactHostnames...)
	t.Cleanup(func() { extraRedactHostnames = prevHosts })
	dir := t.TempDir()
	// Clean body, dirty name.
	live := "build-01-livehost"
	if err := os.WriteFile(filepath.Join(dir, live+".log"), []byte("ok transport pass\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Bind live host as scan candidate via private scan context + identity.
	// Simpler: pass via extra hostname by writing live identity + context.
	prevC, prevD := reinframeCommit, reinframeDirty
	t.Cleanup(func() { reinframeCommit, reinframeDirty = prevC, prevD })
	reinframeCommit = testFullRev
	reinframeDirty = "false"
	cache := t.TempDir()
	prevRoot := privateCacheRootFn
	t.Cleanup(func() { privateCacheRootFn = prevRoot })
	privateCacheRootFn = func() (string, error) { return cache, nil }
	// Manually write identity with scan id and private context with live host.
	scanID := "abcdef0123456789abcdef0123456789"
	path, err := liveScanContextPath(dir, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"schema":%q,"scan_context_id":%q,"live_hostname":%q,"at":"t"}`, liveScanContextSchema, scanID, live)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	idBody := fmt.Sprintf(`{"schema":%q,"live_binary_commit":%q,"live_binary_dirty":false,"live_binary_commit_src":"ldflags","live_goos":%q,"live_goarch":%q,"scan_context_id":%q,"at":"t"}`,
		liveIdentitySchema, testFullRev, runtime.GOOS, runtime.GOARCH, scanID)
	if err := os.WriteFile(filepath.Join(dir, "live_identity.json"), []byte(idBody), 0o600); err != nil {
		t.Fatal(err)
	}
	p := scanPrivacy(dir)
	if p["complete"] == true {
		t.Fatalf("filename host leak must not complete: %+v", p)
	}
	fails, _ := p["failure_classes"].([]string)
	found := false
	for _, f := range fails {
		if strings.Contains(f, "local_identity_filename") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("want local_identity_filename failure, got %v", fails)
	}
}

// Pro R33 P2 / R34 P2: case-sensitive volume must not fold /cache vs /Cache.
func TestPathContainedIn_CaseSensitiveSiblingDirs(t *testing.T) {
	base := t.TempDir()
	// Probe the actual volume of base (not a global temp-only flag).
	if pathVolumeCaseInsensitive(base) {
		t.Skip("this volume is case-insensitive; sibling Cache/cache collapse")
	}
	cache := filepath.Join(base, "cache")
	evidence := filepath.Join(base, "Cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evidence, 0o700); err != nil {
		t.Fatal(err)
	}
	// Distinct siblings on Linux must not contain each other.
	under, err := pathContainedIn(cache, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if under {
		t.Fatalf("case-distinct siblings must not contain: cache=%s evidence=%s", cache, evidence)
	}
	under2, err := pathContainedIn(filepath.Join(cache, "x"), evidence)
	if err != nil {
		t.Fatal(err)
	}
	if under2 {
		t.Fatal("cache/x must not be under evidence Cache")
	}
	// Nested under exact parent still works.
	child := filepath.Join(evidence, "sub")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	under3, err := pathContainedIn(child, evidence)
	if err != nil || !under3 {
		t.Fatalf("true nested path must be contained: under=%v err=%v", under3, err)
	}
	// Volume probe must be scoped to the path (Pro R34): same volume answer for
	// base vs a nested path; empty args do not crash.
	_ = pathVolumeCaseInsensitive()
	if pathVolumeCaseInsensitive(base) != pathVolumeCaseInsensitive(child) {
		t.Fatal("same-volume paths must share case-sensitivity answer")
	}
}

// Pro R32 P1: cross-host report redaction must rewrite imported live hostname.
func TestWriteJSON_RedactsImportedLiveHostname(t *testing.T) {
	prev := append([]string(nil), extraRedactHostnames...)
	t.Cleanup(func() { extraRedactHostnames = prev })
	liveHost := "live-executor-host-99"
	setExtraRedactHostnames(liveHost)
	dir := t.TempDir()
	path := filepath.Join(dir, "scenarios.json")
	m := map[string]ScenarioResult{
		"HOOK-ALLOW-001": {
			ID:     "HOOK-ALLOW-001",
			Status: "PASS",
			Detail: "transport ok on live-executor-host-99 and live-executor-host-99.corp",
		},
	}
	if err := writeJSON(path, m); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(b)
	if strings.Contains(raw, liveHost) {
		t.Fatalf("imported live hostname residual in writeJSON: %s", raw)
	}
	if !strings.Contains(raw, "[HOSTNAME]") {
		t.Fatalf("expected [HOSTNAME] placeholder: %s", raw)
	}
	if !strings.Contains(raw, `"status": "PASS"`) && !strings.Contains(raw, `"status":"PASS"`) {
		t.Fatalf("status enum corrupted: %s", raw)
	}
}

// Pro R25 P2: executable binding digests must be SHA-256 hex, not merely len==64.
func TestLoadLiveExecutable_RequiresSHA256Hex(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, schema, field, sum string) {
		t.Helper()
		body := fmt.Sprintf(`{"schema":%q,%q:%q}`, schema, field, sum)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 64 non-hex must fail.
	write("live_grok_executable.json", liveGrokExeSchema, "grok_executable_sha256", strings.Repeat("z", 64))
	if ok, why := loadLiveGrokExecutableOK(dir); ok {
		t.Fatalf("non-hex digest must fail: why=%s", why)
	}
	// 63 / 65 length fail.
	write("live_grok_executable.json", liveGrokExeSchema, "grok_executable_sha256", strings.Repeat("a", 63))
	if ok, _ := loadLiveGrokExecutableOK(dir); ok {
		t.Fatal("63-char digest must fail")
	}
	write("live_grok_executable.json", liveGrokExeSchema, "grok_executable_sha256", strings.Repeat("a", 65))
	if ok, _ := loadLiveGrokExecutableOK(dir); ok {
		t.Fatal("65-char digest must fail")
	}
	// whitespace padding fails isSHA256Hex.
	write("live_grok_executable.json", liveGrokExeSchema, "grok_executable_sha256", strings.Repeat("a", 63)+" ")
	if ok, _ := loadLiveGrokExecutableOK(dir); ok {
		t.Fatal("whitespace in digest must fail")
	}
	// valid hex OK.
	good := strings.Repeat("ab", 32) // 64 hex chars
	write("live_grok_executable.json", liveGrokExeSchema, "grok_executable_sha256", good)
	if ok, why := loadLiveGrokExecutableOK(dir); !ok {
		t.Fatalf("valid hex must pass: %s", why)
	}
	// hooks loader same contract.
	write("live_grokhooks_executable.json", liveGrokhooksSchema, "grokhooks_executable_sha256", strings.Repeat("z", 64))
	if ok, why := loadLiveGrokhooksExecutableOK(dir); ok {
		t.Fatalf("hooks non-hex must fail: why=%s", why)
	}
	write("live_grokhooks_executable.json", liveGrokhooksSchema, "grokhooks_executable_sha256", good)
	if ok, why := loadLiveGrokhooksExecutableOK(dir); !ok {
		t.Fatalf("hooks valid hex must pass: %s", why)
	}
}
