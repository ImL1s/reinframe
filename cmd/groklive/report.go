package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// liveReportOutcome is the result of the shipped report path (#219).
// Tests drive generateLiveReport without process exit.
type liveReportOutcome struct {
	Disposition   string
	Reasons       []string
	Report        map[string]any
	JSONPath      string
	MDPath        string
	SchemaPath    string
	MandatoryOK   bool
	ArtifactValid bool
	ExitCode      int // 0 when disposition is not NO_GO; 1 for NO_GO
}

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence directory")
	_ = fs.Parse(args)
	evDir := mustAbs(*out, "--evidence-out")
	outcome, err := generateLiveReport(evDir)
	if err != nil {
		fail(err)
	}
	printJSON(map[string]any{
		"ok":             true,
		"disposition":    outcome.Disposition,
		"json":           outcome.JSONPath,
		"md":             outcome.MDPath,
		"reasons":        outcome.Reasons,
		"mandatory_ok":   outcome.MandatoryOK,
		"artifact_valid": outcome.ArtifactValid,
	})
	if outcome.ExitCode != 0 {
		os.Exit(outcome.ExitCode)
	}
}

// generateLiveReport is the shipped report generator used by runReport and tests (#219).
func generateLiveReport(evDir string) (liveReportOutcome, error) {
	scenarios := loadScenarioMap(evDir)

	// Normalize scenario registry BEFORE first disposition evaluation so later
	// recomputation cannot re-promote a demoted disposition (#215).
	if _, ok := scenarios["STATIC-PERM-001"]; !ok {
		scenarios["STATIC-PERM-001"] = ScenarioResult{
			ID:     "STATIC-PERM-001",
			Status: "NOT_RUN",
			Detail: "optional Reinframe-owned static permission fragment not exercised",
			At:     stamp(),
		}
	}

	disp, reasons := evaluateDisposition(scenarios)
	// Floor: disposition may only demote after this point.
	floor := disp

	// Privacy scan: complete-or-fail (#215/#219).
	privacy := scanPrivacy(evDir)

	ver := "unknown"
	verFull := ""
	preflightPresent := false
	preflightValid := false
	preflightUsable := false
	if b, err := os.ReadFile(filepath.Join(evDir, "preflight.json")); err == nil {
		preflightPresent = true
		var pf map[string]any
		if json.Unmarshal(b, &pf) != nil {
			// Always surface diagnostic (#219); demote only qualifying floors (#217).
			reasons = append(reasons, "preflight.json malformed")
			if floor == "GO" || floor == "LIMITED_GO" {
				floor = demoteFloor(floor, "NO_GO")
				reasons = append(reasons, "malformed preflight forbids GO/LIMITED_GO")
			}
		} else {
			preflightValid = true
			if v, ok := pf["version"].(string); ok && v != "" {
				verFull = strings.TrimSpace(v)
				ver = sanitizeVersion(v)
			}
			if usable, ok := pf["usable"].(bool); ok {
				preflightUsable = usable
				if !usable {
					reasons = append(reasons, "preflight usable=false")
					if floor == "GO" || floor == "LIMITED_GO" {
						floor = demoteFloor(floor, "NO_GO")
						reasons = append(reasons, "preflight usable=false forbids GO/LIMITED_GO")
					}
				}
			} else {
				reasons = append(reasons, "preflight missing usable field")
				if floor == "GO" || floor == "LIMITED_GO" {
					floor = demoteFloor(floor, "NO_GO")
					reasons = append(reasons, "preflight missing usable forbids GO/LIMITED_GO")
				}
			}
		}
	} else {
		// Always record missing preflight (#219). Demote only GO/LIMITED_GO (#217).
		reasons = append(reasons, "preflight.json missing")
		if floor == "GO" || floor == "LIMITED_GO" {
			floor = demoteFloor(floor, "NO_GO")
			reasons = append(reasons, "missing preflight forbids GO/LIMITED_GO")
		}
	}

	osName := runtime.GOOS
	day := time.Now().UTC().Format("2006-01-02")
	commit, dirty, commitSrc := reinframeBuildIdentity()

	ack := "transport"
	if sr, ok := scenarios["ACP-SESSION-001"]; ok && sr.ACKLayer != "" {
		ack = sr.ACKLayer
	}

	sessCorr := false
	if sr, ok := scenarios["ACP-SESSION-001"]; ok {
		sessCorr = sr.SessionCorrelated && sr.ACKLayer == "session_visible"
	}

	caps := loadOptionalJSON(filepath.Join(evDir, "acp_manifest.json"))

	// Apply evaluator disposition under the qualification floor (monotonic).
	disp = demoteFloor(floor, disp)

	// Live qualification for GO and LIMITED_GO alike (#215).
	if demote, msgs := liveQualification(disp, privacy, caps, scenarios, preflightPresent, preflightValid, preflightUsable, ver, commit, commitSrc, dirty); demote != disp {
		disp = demote
		reasons = append(reasons, msgs...)
	}
	// Re-assert floor cannot be promoted past.
	disp = demoteFloor(floor, disp)

	report := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue":                 167,
			"generated_at":          stamp(),
			"goos":                  osName,
			"goarch":                runtime.GOARCH,
			"grok_version":          ver,
			"grok_version_full":     verFull,
			"reinframe_commit":      commit,
			"reinframe_dirty":       dirty,
			"reinframe_commit_src":  commitSrc,
			"starting_main_sha":     "62889cb59916fa2dd412a2c5d511c7ac7c4b23c6",
			"main_tip_note":         "evidence produced against live host using shipped #165/#166 APIs; v2 gates (#199/#215)",
			"harness":               "cmd/groklive",
			"evidence_binding_note": "generated solely by cmd/groklive report; no post-hoc privacy rewrites",
			"schema_note":           "v2 closed disposition matrix; historical v1 evidence is immutable under HISTORICAL_v1.md",
		},
		"entry_gates": map[string]any{
			"live_flag_required": true,
			"auth_json_read":     false,
			"credential_print":   false,
		},
		"trust_results":             pick(scenarios, "TRUST-001", "TRUST-STALE-001", "TRUST-RESTORE-001"),
		"hook_results":              pick(scenarios, "HOOK-ALLOW-001", "HOOK-DENY-001", "HOOK-MAP-001", "HOOK-UNINSTALL-001"),
		"hook_failure_semantics":    pick(scenarios, "HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004"),
		"static_permission_results": pick(scenarios, "STATIC-PERM-001"),
		"acp_negotiation":           pick(scenarios, "ACP-INIT-001"),
		"auth_boundary":             pick(scenarios, "ACP-AUTH-001"),
		"session_results":           pick(scenarios, "ACP-SESSION-001", "ACP-OPTIONAL-001"),
		"advice_results":            pick(scenarios, "ADVICE-DEDUP-001"),
		"challenge_results":         pick(scenarios, "CHALLENGE-001"),
		"ack_layers": map[string]any{
			"strongest_proven":  ack,
			"explicit_claimed":  false,
			"source_correlated": sessCorr,
			"note":              "JSON-RPC success is transport; session_visible only when SessionCorrelated; explicit never from transport alone",
		},
		"process_cleanup":   pick(scenarios, "ACP-CLEANUP-001"),
		"privacy_checks":    privacy,
		"limitations":       reasons,
		"scenarios":         scenarios,
		"scenario_registry": append([]string{}, goMandatoryIDs...),
		"final_disposition": disp,
	}
	// Only emit capability_manifests when structurally schema-ready (#219).
	// Invalid optional evidence is omitted with an explicit limitation (not embedded invalid).
	if caps != nil {
		if err := capabilityManifestEmitOK(caps); err != nil {
			reasons = append(reasons, "capability_manifests omitted: "+err.Error())
			report["limitations"] = reasons
		} else {
			report["capability_manifests"] = caps
		}
	}

	if verrs := validateReportV2Basics(report, scenarios); len(verrs) > 0 {
		// Semantic mismatch: demote only — never re-promote past floor.
		disp2, reasons2 := evaluateDisposition(scenarios)
		disp = demoteFloor(floor, disp2)
		reasons = append(reasons, reasons2...)
		reasons = append(reasons, verrs...)
		if demote, msgs := liveQualification(disp, privacy, caps, scenarios, preflightPresent, preflightValid, preflightUsable, ver, commit, commitSrc, dirty); demote != disp {
			disp = demote
			reasons = append(reasons, msgs...)
		}
		disp = demoteFloor(floor, disp)
		if disp == "GO" || disp == "LIMITED_GO" {
			disp = "NO_GO"
			reasons = append(reasons, "validation errors forbid GO/LIMITED_GO")
		}
		report["final_disposition"] = disp
		report["limitations"] = reasons
	}

	// Committed schema gate for ALL dispositions (#219): never write invalid success.
	if err := ensureReportSchemaValid(report, &reasons); err != nil {
		return liveReportOutcome{}, err
	}
	report["limitations"] = reasons
	report["final_disposition"] = disp

	if disp == "GO" || disp == "LIMITED_GO" {
		// Belt: qualification must still hold against emitted report body.
		if err := validateReportAgainstCommittedSchema(report); err != nil {
			disp = "NO_GO"
			reasons = append(reasons, "pre-write schema gate: "+err.Error())
			report["final_disposition"] = disp
			report["limitations"] = reasons
			if err := ensureReportSchemaValid(report, &reasons); err != nil {
				return liveReportOutcome{}, err
			}
			report["limitations"] = reasons
		}
	}

	base := fmt.Sprintf("issue-167-live-v2-%s-%s-%s", sanitizeVersion(ver), osName, day)
	jsonPath := filepath.Join(evDir, base+".json")
	mdPath := filepath.Join(evDir, base+".md")
	if err := writeJSON(jsonPath, report); err != nil {
		return liveReportOutcome{}, err
	}
	md := renderMD(report, disp, ack, ver, osName, reasons, scenarios)
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		return liveReportOutcome{}, err
	}

	schemaPath := filepath.Join(evDir, "reinframe.grok_build_live_control.v2.schema.json")
	if err := os.WriteFile(schemaPath, EmbeddedV2SchemaJSON(), 0o600); err != nil {
		return liveReportOutcome{}, fmt.Errorf("write schema: %w", err)
	}

	// Final proof: on-disk JSON validates.
	onDisk, err := os.ReadFile(jsonPath)
	if err != nil {
		return liveReportOutcome{}, err
	}
	var diskReport map[string]any
	if err := json.Unmarshal(onDisk, &diskReport); err != nil {
		return liveReportOutcome{}, fmt.Errorf("written report not JSON: %w", err)
	}
	if err := validateReportAgainstCommittedSchema(diskReport); err != nil {
		// Fail closed: remove invalid artifact claim path.
		_ = os.Remove(jsonPath)
		return liveReportOutcome{}, fmt.Errorf("written report fails committed schema: %w", err)
	}

	exit := 0
	if disp == "NO_GO" {
		exit = 1
	}
	return liveReportOutcome{
		Disposition:   disp,
		Reasons:       reasons,
		Report:        report,
		JSONPath:      jsonPath,
		MDPath:        mdPath,
		SchemaPath:    schemaPath,
		MandatoryOK:   disp == "GO" || disp == "LIMITED_GO",
		ArtifactValid: true,
		ExitCode:      exit,
	}, nil
}

// ensureReportSchemaValid drops optional bad capability_manifests once, then requires schema pass.
func ensureReportSchemaValid(report map[string]any, reasons *[]string) error {
	if err := validateReportAgainstCommittedSchema(report); err == nil {
		return nil
	} else {
		*reasons = append(*reasons, "committed_schema: "+err.Error())
		if _, ok := report["capability_manifests"]; ok {
			delete(report, "capability_manifests")
			*reasons = append(*reasons, "capability_manifests omitted: fails committed schema")
			report["limitations"] = *reasons
			if err2 := validateReportAgainstCommittedSchema(report); err2 == nil {
				return nil
			} else {
				return fmt.Errorf("report fails committed schema after omit: %w", err2)
			}
		}
		return fmt.Errorf("report fails committed schema: %w", err)
	}
}

// capabilityManifestEmitOK checks closed structural readiness for JSON emit (#219).
// Does not require ACP scenario PASS (qualification still does via liveQualification).
func capabilityManifestEmitOK(caps any) error {
	if caps == nil {
		return fmt.Errorf("missing")
	}
	m, ok := caps.(map[string]any)
	if !ok {
		return fmt.Errorf("must be object (got %T)", caps)
	}
	for _, req := range []string{"pre_handshake", "post_handshake", "auth_methods", "caps_digest"} {
		if _, ok := m[req]; !ok {
			return fmt.Errorf("missing required field %s", req)
		}
	}
	pre, err := decodeClosedFoundation(m["pre_handshake"])
	if err != nil {
		return fmt.Errorf("pre_handshake: %w", err)
	}
	post, err := decodeClosedFoundation(m["post_handshake"])
	if err != nil {
		return fmt.Errorf("post_handshake: %w", err)
	}
	if err := validatePreHandshake(pre); err != nil {
		return fmt.Errorf("pre_handshake: %w", err)
	}
	if err := validatePostHandshake(post); err != nil {
		return fmt.Errorf("post_handshake: %w", err)
	}
	authIDs, err := parseClosedAuthMethods(m["auth_methods"])
	if err != nil {
		return fmt.Errorf("auth_methods: %w", err)
	}
	if !stringSlicesEqual(authIDs, post.AuthMethods) {
		return fmt.Errorf("auth_methods disagree with post_handshake.auth_methods")
	}
	if len(authIDs) == 0 {
		return fmt.Errorf("auth_methods empty")
	}
	dig, _ := m["caps_digest"].(string)
	dig = strings.TrimSpace(dig)
	if dig != adapter.CapsDigestFromFoundation(post) {
		return fmt.Errorf("caps_digest forged or stale")
	}
	return nil
}

// demoteFloor never promotes: returns the worse of floor and candidate.
func demoteFloor(floor, candidate string) string {
	rank := map[string]int{"GO": 3, "LIMITED_GO": 2, "MORE_DATA": 1, "NO_GO": 0}
	if rank[candidate] < rank[floor] {
		return candidate
	}
	return floor
}

// liveQualification applies hard live-qualification gates for GO and LIMITED_GO (#215).
func liveQualification(disp string, privacy map[string]any, caps any, scenarios map[string]ScenarioResult, preflightPresent, preflightValid, preflightUsable bool, ver, commit, commitSrc string, dirty bool) (string, []string) {
	if disp != "GO" && disp != "LIMITED_GO" {
		return disp, nil
	}
	var msgs []string
	if !preflightPresent || !preflightValid {
		msgs = append(msgs, "qualification requires valid preflight.json")
	}
	if !preflightUsable {
		msgs = append(msgs, "qualification requires preflight.usable=true")
	}
	if ver == "" || ver == "unknown" {
		msgs = append(msgs, "qualification requires non-empty grok_version")
	}
	if commit == "" || commitSrc == "unknown" {
		msgs = append(msgs, "qualification requires binary-bound reinframe_commit")
	}
	if commit != "" && !isFullVCSRevision(commit) {
		msgs = append(msgs, "qualification requires full VCS revision (40/64 hex)")
	}
	if dirty {
		msgs = append(msgs, "qualification forbids dirty (modified) binary worktree")
	}
	// Privacy complete-or-fail
	if privacy == nil {
		msgs = append(msgs, "qualification requires privacy scan")
	} else {
		if complete, ok := privacy["complete"].(bool); !ok || !complete {
			msgs = append(msgs, "qualification requires privacy scan complete=true")
		}
		if v, ok := privacy["secret_pattern_hits"].(int); ok && v > 0 {
			msgs = append(msgs, "qualification requires secret_pattern_hits==0")
		} else if vf, ok := privacy["secret_pattern_hits"].(float64); ok && vf > 0 {
			msgs = append(msgs, "qualification requires secret_pattern_hits==0")
		}
		if b, ok := privacy["auth_json_path_leak_suspected"].(bool); ok && b {
			msgs = append(msgs, "qualification forbids auth_json_path_leak_suspected")
		}
		if b, ok := privacy["token_fields_in_auth_envelope"].(bool); ok && b {
			msgs = append(msgs, "qualification forbids token_fields_in_auth_envelope")
		}
		if b, ok := privacy["raw_thoughts_stored"].(bool); ok && b {
			msgs = append(msgs, "qualification forbids raw_thoughts_stored")
		}
		if _, ok := privacy["error"]; ok {
			msgs = append(msgs, "qualification requires privacy scan without error")
		}
		if n, ok := asInt(privacy["files_scanned"]); ok && n == 0 {
			msgs = append(msgs, "qualification requires privacy scan of at least one evidence file")
		}
	}
	if err := validateCapabilityManifest(caps, scenarios); err != nil {
		msgs = append(msgs, "capability_manifests: "+err.Error())
	}
	if len(msgs) > 0 {
		return "NO_GO", msgs
	}
	return disp, nil
}

func asInt(v any) (int, bool) {
	switch t := v.(type) {
	case int:
		return t, true
	case float64:
		return int(t), true
	default:
		return 0, false
	}
}

// validateCapabilityManifest requires the closed ACP harness object shape (#215/#218).
// Empty handshake objects, arbitrary nested fields, and forged caps_digest are rejected.
func validateCapabilityManifest(caps any, scenarios map[string]ScenarioResult) error {
	if caps == nil {
		return fmt.Errorf("missing")
	}
	m, ok := caps.(map[string]any)
	if !ok {
		return fmt.Errorf("must be object (got %T)", caps)
	}
	if len(m) == 0 {
		return fmt.Errorf("empty object")
	}
	allowed := map[string]struct{}{
		"pre_handshake": {}, "post_handshake": {}, "auth_methods": {}, "caps_digest": {},
	}
	for k := range m {
		if _, ok := allowed[k]; !ok {
			return fmt.Errorf("unknown field %q", k)
		}
	}
	for _, req := range []string{"pre_handshake", "post_handshake", "auth_methods", "caps_digest"} {
		if _, ok := m[req]; !ok {
			return fmt.Errorf("missing required field %s", req)
		}
	}

	pre, err := decodeClosedFoundation(m["pre_handshake"])
	if err != nil {
		return fmt.Errorf("pre_handshake: %w", err)
	}
	post, err := decodeClosedFoundation(m["post_handshake"])
	if err != nil {
		return fmt.Errorf("post_handshake: %w", err)
	}
	if err := validatePreHandshake(pre); err != nil {
		return fmt.Errorf("pre_handshake: %w", err)
	}
	if err := validatePostHandshake(post); err != nil {
		return fmt.Errorf("post_handshake: %w", err)
	}

	authIDs, err := parseClosedAuthMethods(m["auth_methods"])
	if err != nil {
		return fmt.Errorf("auth_methods: %w", err)
	}
	// Top-level auth_methods must always match post-handshake (including empty).
	// omitempty on post must not allow inventing top-level auth alone (#218 review).
	if !stringSlicesEqual(authIDs, post.AuthMethods) {
		return fmt.Errorf("auth_methods disagree with post_handshake.auth_methods")
	}

	dig, _ := m["caps_digest"].(string)
	dig = strings.TrimSpace(dig)
	if dig == "" {
		return fmt.Errorf("caps_digest empty")
	}
	// Recompute from negotiated post-handshake facts — never trust the supplied string alone.
	want := adapter.CapsDigestFromFoundation(post)
	if dig != want {
		return fmt.Errorf("caps_digest forged or stale (want recomputed from post_handshake)")
	}

	// Cross-check: valid manifest requires proven ACP init/auth scenarios.
	if sr, ok := scenarios["ACP-INIT-001"]; !ok || sr.Status != "PASS" {
		return fmt.Errorf("ACP-INIT-001 must be PASS for capability manifest")
	}
	if sr, ok := scenarios["ACP-AUTH-001"]; !ok || sr.Status != "PASS" {
		return fmt.Errorf("ACP-AUTH-001 must be PASS for capability manifest")
	}
	// Non-empty advertised auth is required for AUTH PASS path consistency with harness.
	if len(authIDs) == 0 || len(post.AuthMethods) == 0 {
		return fmt.Errorf("auth_methods empty contradicts ACP-AUTH-001 PASS")
	}
	return nil
}

// decodeClosedFoundation unmarshals pre/post handshake with unknown fields rejected.
func decodeClosedFoundation(v any) (adapter.GrokACPFoundationManifest, error) {
	var zero adapter.GrokACPFoundationManifest
	if v == nil {
		return zero, fmt.Errorf("missing")
	}
	// Empty object {} is invalid for qualification (zero values fail validate*).
	if m, ok := v.(map[string]any); ok && len(m) == 0 {
		return zero, fmt.Errorf("empty object")
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return zero, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var out adapter.GrokACPFoundationManifest
	if err := dec.Decode(&out); err != nil {
		return zero, err
	}
	return out, nil
}

func validatePreHandshake(m adapter.GrokACPFoundationManifest) error {
	if m.Profile != adapter.GrokACPProfileV1 {
		return fmt.Errorf("profile must be %s", adapter.GrokACPProfileV1)
	}
	if m.ProtocolVersion != adapter.GrokACPProtocolVersion {
		return fmt.Errorf("protocol_version must be %d", adapter.GrokACPProtocolVersion)
	}
	if m.NegotiatedLevel != -1 {
		return fmt.Errorf("negotiated_level must be -1 pre-handshake")
	}
	if strings.TrimSpace(m.HonestyNote) == "" {
		return fmt.Errorf("honesty_note required")
	}
	// Pre-handshake must not claim achieved capabilities.
	if m.CapEventStream || m.CapToolInspection || m.CapAdviceDelivery || m.CapDiffInspection ||
		m.CapPause || m.CapCancel || m.CapResume || m.CapInterventionAck || m.ExplicitAck || m.LoadSession {
		return fmt.Errorf("pre-handshake must not claim achieved capabilities")
	}
	if len(m.AuthMethods) > 0 {
		return fmt.Errorf("pre-handshake must not advertise auth_methods")
	}
	return nil
}

func validatePostHandshake(m adapter.GrokACPFoundationManifest) error {
	if m.Profile != adapter.GrokACPProfileV1 {
		return fmt.Errorf("profile must be %s", adapter.GrokACPProfileV1)
	}
	if m.ProtocolVersion != adapter.GrokACPProtocolVersion {
		return fmt.Errorf("protocol_version must be %d", adapter.GrokACPProtocolVersion)
	}
	if m.NegotiatedLevel < -1 || m.NegotiatedLevel > 3 {
		return fmt.Errorf("negotiated_level out of range")
	}
	if strings.TrimSpace(m.HonestyNote) == "" {
		return fmt.Errorf("honesty_note required")
	}
	// Explicit agent ACK / intervention-ack overclaims are never allowed from harness alone.
	// CapPause may be true only when advertised in post caps; negotiated_level must still recompute.
	if m.ExplicitAck {
		return fmt.Errorf("explicit_ack must remain false")
	}
	if m.CapInterventionAck {
		return fmt.Errorf("cap_intervention_ack must remain false without proof")
	}
	// Recompute level from the same boolean mapping as ManifestFromNegotiated (#218).
	pm := protocol.CapabilityManifest{
		AgentID:                "grok_build_acp",
		Version:                adapter.GrokACPProfileV1,
		SupportsEventStream:    m.CapEventStream,
		SupportsAdviceDelivery: m.CapAdviceDelivery,
		SupportsToolInspection: m.CapToolInspection,
		SupportsDiffInspection: m.CapDiffInspection,
		SupportsPause:          m.CapPause,
		SupportsCancel:         m.CapCancel,
		SupportsResume:         m.CapResume,
	}
	wantLevel := protocol.EvaluateAchievableLevel(&pm)
	if m.NegotiatedLevel != wantLevel {
		return fmt.Errorf("negotiated_level forged or stale (want %d from caps)", wantLevel)
	}
	return nil
}

// parseClosedAuthMethods accepts harness []string or []{id: string} objects; rejects dups.
func parseClosedAuthMethods(v any) ([]string, error) {
	if v == nil {
		return nil, fmt.Errorf("missing")
	}
	var items []any
	switch t := v.(type) {
	case []any:
		items = t
	case []string:
		out := append([]string(nil), t...)
		return normalizeAuthIDs(out)
	default:
		return nil, fmt.Errorf("must be array (got %T)", v)
	}
	if len(items) > adapter.MaxGrokACPAuthMethods {
		return nil, fmt.Errorf("too many methods")
	}
	var ids []string
	for _, item := range items {
		switch x := item.(type) {
		case string:
			ids = append(ids, x)
		case map[string]any:
			// Only closed {id: "..."} (or methodId) — no credential keys.
			for k := range x {
				lk := strings.ToLower(k)
				if lk != "id" && lk != "methodid" {
					return nil, fmt.Errorf("auth method object allows only id")
				}
			}
			id, _ := x["id"].(string)
			if id == "" {
				id, _ = x["methodId"].(string)
			}
			if id == "" {
				return nil, fmt.Errorf("auth method object missing id")
			}
			ids = append(ids, id)
		default:
			return nil, fmt.Errorf("auth method entry unsupported type")
		}
	}
	return normalizeAuthIDs(ids)
}

func normalizeAuthIDs(ids []string) ([]string, error) {
	if len(ids) > adapter.MaxGrokACPAuthMethods {
		return nil, fmt.Errorf("too many methods")
	}
	seen := make(map[string]struct{}, len(ids))
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("empty auth method id")
		}
		if len(id) > adapter.MaxGrokACPAuthMethodID {
			return nil, fmt.Errorf("auth method id too long")
		}
		if _, dup := seen[id]; dup {
			return nil, fmt.Errorf("duplicate auth method %q", id)
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

const (
	maxPrivacyFileBytes  = 1 << 20 // 1 MiB per file
	maxPrivacyFiles      = 256
	maxPrivacyTotalBytes = 8 << 20 // 8 MiB aggregate scanned content
)

// Closed substrings that indicate private reasoning envelopes were persisted (#219).
var rawThoughtsMarkers = []string{
	`"raw_thoughts"`,
	`"private_reasoning"`,
	`"thinking_blocks"`,
	`"reasoning_content"`,
	`"encrypted_content"`,
}

// scanPrivacy is a complete-or-fail privacy scan of the flat evidence directory (#215/#219).
// Only regular files are content-scanned; size is checked before full load via Stat + LimitReader.
func scanPrivacy(evDir string) map[string]any {
	out := map[string]any{
		"method":                                    "complete_or_fail_flat_scan",
		"complete":                                  false,
		"files_seen":                                0,
		"files_scanned":                             0,
		"files_skipped":                             0,
		"bytes_scanned":                             0,
		"auth_json_read":                            false,
		"auth_json_path_seen_in_honesty_notes_only": false,
		"auth_json_path_leak_suspected":             false,
		"token_fields_in_auth_envelope":             false,
		"raw_thoughts_stored":                       false,
		"secret_pattern_hits":                       0,
		"failure_classes":                           []string{},
	}
	entries, err := os.ReadDir(evDir)
	if err != nil {
		out["error"] = err.Error()
		out["failure_classes"] = []string{"readdir"}
		return out
	}
	hits := 0
	honestyOnly := false
	leak := false
	rawThoughts := false
	seen := 0
	scanned := 0
	skipped := 0
	totalBytes := 0
	var fails []string
	for _, e := range entries {
		// Evidence dir is flat: nested directories forbid complete qualification.
		if e.IsDir() {
			seen++
			skipped++
			fails = append(fails, "nested_directory:"+e.Name())
			continue
		}
		full := filepath.Join(evDir, e.Name())
		fi, err := os.Lstat(full)
		if err != nil {
			seen++
			skipped++
			fails = append(fails, "unreadable:"+e.Name())
			continue
		}
		seen++
		mode := fi.Mode()
		if mode&os.ModeSymlink != 0 {
			skipped++
			fails = append(fails, "symlink:"+e.Name())
			continue
		}
		if !mode.IsRegular() {
			// FIFO, socket, device, etc. — never open for content (#219).
			skipped++
			fails = append(fails, "non_regular:"+e.Name())
			continue
		}
		if fi.Size() > int64(maxPrivacyFileBytes) {
			skipped++
			fails = append(fails, "oversized:"+e.Name())
			continue
		}
		if scanned >= maxPrivacyFiles {
			skipped++
			fails = append(fails, "file_count_cap:"+e.Name())
			continue
		}
		// Bounded read: Stat size already checked; LimitReader is a hard cap.
		f, err := os.Open(full)
		if err != nil {
			skipped++
			fails = append(fails, "unreadable:"+e.Name())
			continue
		}
		lr := io.LimitReader(f, int64(maxPrivacyFileBytes)+1)
		b, err := io.ReadAll(lr)
		_ = f.Close()
		if err != nil {
			skipped++
			fails = append(fails, "unreadable:"+e.Name())
			continue
		}
		if len(b) > maxPrivacyFileBytes {
			skipped++
			fails = append(fails, "oversized:"+e.Name())
			continue
		}
		if totalBytes+len(b) > maxPrivacyTotalBytes {
			skipped++
			fails = append(fails, "total_bytes_cap:"+e.Name())
			continue
		}
		scanned++
		totalBytes += len(b)
		s := string(b)
		if strings.Contains(s, "auth.json") || strings.Contains(s, ".grok/auth") {
			if strings.Contains(strings.ToLower(s), "never read") || strings.Contains(s, "honesty_note") ||
				strings.Contains(s, "auth_json_read") {
				honestyOnly = true
			} else {
				leak = true
			}
		}
		if strings.Contains(s, `"token"`) && strings.Contains(s, "authenticate") &&
			!strings.Contains(s, "no token") && !strings.Contains(s, "token field") {
			out["token_fields_in_auth_envelope"] = true
		}
		for _, pat := range []string{"sk-", "xai-", "Bearer ", "eyJ"} {
			if strings.Contains(s, pat) {
				if strings.Contains(s, "[REDACTED]") && pat != "eyJ" {
					continue
				}
				hits++
			}
		}
		for _, m := range rawThoughtsMarkers {
			if strings.Contains(s, m) {
				rawThoughts = true
				break
			}
		}
	}
	out["files_seen"] = seen
	out["files_scanned"] = scanned
	out["files_skipped"] = skipped
	out["bytes_scanned"] = totalBytes
	out["secret_pattern_hits"] = hits
	out["raw_thoughts_stored"] = rawThoughts
	out["auth_json_path_seen_in_honesty_notes_only"] = honestyOnly && !leak
	out["auth_json_path_leak_suspected"] = leak
	if len(fails) > 0 {
		out["failure_classes"] = fails
	}
	// Complete only when every seen entry was scanned and at least one file scanned.
	out["complete"] = skipped == 0 && scanned > 0 && len(fails) == 0
	return out
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func pick(m map[string]ScenarioResult, ids ...string) map[string]ScenarioResult {
	out := map[string]ScenarioResult{}
	for _, id := range ids {
		if sr, ok := m[id]; ok {
			out[id] = sr
		}
	}
	return out
}

func loadOptionalJSON(path string) any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return v
}

func sanitizeVersion(v string) string {
	v = strings.TrimSpace(v)
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '(' || r == ')' || r == '[' || r == ']':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 48 {
		out = out[:48]
	}
	if out == "" {
		return "unknown"
	}
	return out
}

func renderMD(report map[string]any, disp, ack, ver, osName string, reasons []string, scenarios map[string]ScenarioResult) string {
	var b strings.Builder
	b.WriteString("# Grok Build live control evidence (#167)\n\n")
	fmt.Fprintf(&b, "- **Disposition:** `%s`\n", disp)
	fmt.Fprintf(&b, "- **Grok version:** %s\n", ver)
	fmt.Fprintf(&b, "- **OS:** %s\n", osName)
	fmt.Fprintf(&b, "- **Strongest ACK proven:** `%s`\n", ack)
	b.WriteString("- **Auth.json read:** no\n")
	b.WriteString("- **Explicit ACK claimed:** no\n\n")
	b.WriteString("## Scenarios\n\n| ID | Status | Detail |\n|----|--------|--------|\n")
	for id, sr := range scenarios {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", id, sr.Status, strings.ReplaceAll(boundStr(sr.Detail, 80), "|", "/"))
	}
	if len(reasons) > 0 {
		b.WriteString("\n## Limitations\n\n")
		for _, r := range reasons {
			fmt.Fprintf(&b, "- %s\n", r)
		}
	}
	b.WriteString("\n## Non-claims\n\n")
	b.WriteString("- No Level 2 / CapPause from hooks alone\n")
	b.WriteString("- No cross-host ranking\n")
	b.WriteString("- No credential material intentionally stored\n")
	_ = report
	return b.String()
}

