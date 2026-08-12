package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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
		// Always record missing/unreadable preflight (#219). Demote only GO/LIMITED_GO (#217).
		if os.IsNotExist(err) {
			reasons = append(reasons, "preflight.json missing")
		} else {
			reasons = append(reasons, "preflight.json unreadable: "+err.Error())
		}
		if floor == "GO" || floor == "LIMITED_GO" {
			floor = demoteFloor(floor, "NO_GO")
			reasons = append(reasons, "missing preflight forbids GO/LIMITED_GO")
		}
	}

	day := time.Now().UTC().Format("2006-01-02")
	commit, dirty, commitSrc := reinframeBuildIdentity()
	// live_identity.json is mandatory for qualifying reports — never fall back to the
	// report-generator identity (GPT-5.6 Pro P1: false-qualify via silent fallback).
	liveID := loadLiveIdentity(evDir)
	liveBinaryCommit, liveBinaryDirty, liveBinarySrc := liveID.Commit, liveID.Dirty, liveID.Src
	// Campaign platform from live_identity when bound; never silently rename with generator GOOS.
	osName := liveID.GOOS
	liveArch := liveID.GOARCH
	if osName == "" || !isValidGOOS(osName) {
		osName = runtime.GOOS
	}
	if liveArch == "" || !isValidGOARCH(liveArch) {
		liveArch = runtime.GOARCH
	}
	// Final path-component guard (basename must never contain separators).
	if !isValidGOOS(osName) {
		osName = "unknown"
	}
	if !liveID.OK {
		reasons = append(reasons, liveID.Err)
		if floor == "GO" || floor == "LIMITED_GO" {
			floor = demoteFloor(floor, "NO_GO")
			reasons = append(reasons, "invalid/missing live_identity forbids GO/LIMITED_GO")
		}
		if liveBinarySrc == "" {
			liveBinarySrc = "missing"
		}
	}
	// External Grok CLI content binding (Pro R6 P1): standalone phases must share
	// the same --grok-executable contents, not only Reinframe harness identity.
	if grokOK, grokWhy := loadLiveGrokExecutableOK(evDir); !grokOK {
		// Require binding when any phase evidence is present (preflight/scenarios).
		if preflightPresent || len(scenarios) > 0 {
			reasons = append(reasons, grokWhy)
			if floor == "GO" || floor == "LIMITED_GO" {
				floor = demoteFloor(floor, "NO_GO")
				reasons = append(reasons, "invalid/missing live_grok_executable forbids GO/LIMITED_GO")
			}
		}
	}
	// Hooks helper content binding (Pro R14 P1): when hook scenarios exist, require
	// live_grokhooks_executable.json so outcomes cannot come from a swapped helper.
	if hasHookScenarios(scenarios) {
		if hooksOK, hooksWhy := loadLiveGrokhooksExecutableOK(evDir); !hooksOK {
			reasons = append(reasons, hooksWhy)
			if floor == "GO" || floor == "LIMITED_GO" {
				floor = demoteFloor(floor, "NO_GO")
				reasons = append(reasons, "invalid/missing live_grokhooks_executable forbids GO/LIMITED_GO")
			}
		}
	}

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
	// Bind qualification to the LIVE executor binary (not a later report re-run generator).
	if demote, msgs := liveQualification(disp, privacy, caps, scenarios, preflightPresent, preflightValid, preflightUsable, ver, liveBinaryCommit, liveBinarySrc, liveBinaryDirty); demote != disp {
		disp = demote
		reasons = append(reasons, msgs...)
	}
	// Re-assert floor cannot be promoted past.
	disp = demoteFloor(floor, disp)

	report := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue":                167,
			"generated_at":         stamp(),
			// Campaign platform (prefer live_identity bind; Pro R10 P2).
			"goos":                 osName,
			"goarch":               liveArch,
			"live_goos":            liveID.GOOS,
			"live_goarch":          liveID.GOARCH,
			"report_generator_goos":   runtime.GOOS,
			"report_generator_goarch": runtime.GOARCH,
			"grok_version":         ver,
			"grok_version_full":    verFull,
			"reinframe_commit":     commit,
			"reinframe_dirty":      dirty,
			"reinframe_commit_src": commitSrc,
			// Live executor vs report generator identities (#post-230 GPT P1-B).
			// starting_main_sha / live_binary_* describe the process that executed live phases.
			// report_generator_* describe the binary that produced this formal report.
			"starting_main_sha":           liveBinaryCommit,
			"live_binary_commit":          liveBinaryCommit,
			"live_binary_dirty":           liveBinaryDirty,
			"live_binary_commit_src":      liveBinarySrc,
			"report_generator_commit":     commit,
			"report_generator_dirty":      dirty,
			"report_generator_commit_src": commitSrc,
			// derived when live vs generator commit, dirty, or platform differ (Pro R10 P2).
			"derived": liveID.OK && liveBinaryCommit != "" && commit != "" &&
				(liveBinaryCommit != commit || liveBinaryDirty != dirty ||
					(liveID.GOOS != "" && liveID.GOOS != runtime.GOOS) ||
					(liveID.GOARCH != "" && liveID.GOARCH != runtime.GOARCH)),
			"main_tip_note":               "evidence produced against live host using shipped #165/#166 APIs; v2 gates (#199/#215); live_binary_commit vs report_generator_commit may differ when report is re-run",
			"harness":                     "cmd/groklive",
			"evidence_binding_note":       "generated by cmd/groklive report; live_binary_commit requires complete live_identity.json (no generator fallback)",
			"schema_note":                 "v2 closed disposition matrix; historical v1 evidence is immutable under HISTORICAL_v1.md",
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
	// If we omit after qualification used caps, demote GO/LIMITED_GO (#219 review).
	if caps != nil {
		if err := capabilityManifestEmitOK(caps); err != nil {
			reasons = append(reasons, "capability_manifests omitted: "+err.Error())
			if disp == "GO" || disp == "LIMITED_GO" {
				disp = "NO_GO"
				reasons = append(reasons, "omitted capability_manifests forbids GO/LIMITED_GO")
			}
			report["final_disposition"] = disp
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
		if demote, msgs := liveQualification(disp, privacy, caps, scenarios, preflightPresent, preflightValid, preflightUsable, ver, liveBinaryCommit, liveBinarySrc, liveBinaryDirty); demote != disp {
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
	// Sync disposition from report if ensure demoted GO after caps strip.
	if d, ok := report["final_disposition"].(string); ok && d != "" {
		disp = d
	}
	report["limitations"] = reasons
	report["final_disposition"] = disp

	base := fmt.Sprintf("issue-167-live-v2-%s-%s-%s", sanitizeVersion(ver), osName, day)
	jsonPath := filepath.Join(evDir, base+".json")
	mdPath := filepath.Join(evDir, base+".md")
	schemaPath := filepath.Join(evDir, "reinframe.grok_build_live_control.v2.schema.json")

	// Write JSON first; only then MD/schema once disk JSON is schema-valid.
	if err := writeJSON(jsonPath, report); err != nil {
		return liveReportOutcome{}, err
	}
	onDisk, err := os.ReadFile(jsonPath)
	if err != nil {
		return liveReportOutcome{}, err
	}
	var diskReport map[string]any
	if err := json.Unmarshal(onDisk, &diskReport); err != nil {
		_ = os.Remove(jsonPath)
		return liveReportOutcome{}, fmt.Errorf("written report not JSON: %w", err)
	}
	if err := validateReportAgainstCommittedSchema(diskReport); err != nil {
		_ = os.Remove(jsonPath)
		return liveReportOutcome{}, fmt.Errorf("written report fails committed schema: %w", err)
	}

	md := redactLocalIdentity(renderMD(report, disp, ack, ver, osName, reasons, scenarios))
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		_ = os.Remove(jsonPath)
		return liveReportOutcome{}, err
	}
	if err := os.WriteFile(schemaPath, EmbeddedV2SchemaJSON(), 0o600); err != nil {
		_ = os.Remove(jsonPath)
		_ = os.Remove(mdPath)
		return liveReportOutcome{}, fmt.Errorf("write schema: %w", err)
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
// When caps are stripped, demotes GO/LIMITED_GO on the report body (#219).
func ensureReportSchemaValid(report map[string]any, reasons *[]string) error {
	if err := validateReportAgainstCommittedSchema(report); err == nil {
		return nil
	} else {
		*reasons = append(*reasons, "committed_schema: "+err.Error())
		if _, ok := report["capability_manifests"]; ok {
			delete(report, "capability_manifests")
			*reasons = append(*reasons, "capability_manifests omitted: fails committed schema")
			if d, _ := report["final_disposition"].(string); d == "GO" || d == "LIMITED_GO" {
				report["final_disposition"] = "NO_GO"
				*reasons = append(*reasons, "omitted capability_manifests forbids GO/LIMITED_GO")
			}
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
			// Schema-aligned: only closed {id: "..."} (no methodId-only for emit).
			for k := range x {
				if k != "id" {
					return nil, fmt.Errorf("auth method object allows only id")
				}
			}
			id, _ := x["id"].(string)
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

// scanPrivacy is a complete-or-fail privacy scan of the flat evidence directory (#215/#219).
// Only regular files are content-scanned; size is checked before full load via Stat + LimitReader.
func scanPrivacy(evDir string) map[string]any {
	out := map[string]any{
		"method":         "complete_or_fail_flat_scan",
		"complete":       false,
		"files_seen":     0,
		"files_scanned":  0,
		"files_skipped":  0,
		"bytes_scanned":  0,
		"auth_json_read": false,
		"auth_json_path_seen_in_honesty_notes_only": false,
		"auth_json_path_leak_suspected":             false,
		"token_fields_in_auth_envelope":             false,
		"raw_thoughts_stored":                       false,
		"secret_pattern_hits":                       0,
		"failure_classes":                           []string{},
	}
	// Hostnames for leak detection: report-generator host PLUS live executor host
	// recorded at campaign time (Codex GraphQL P1: derived report on another machine
	// must still flag residual live hostnames without a .local suffix).
	scanHosts := privacyScanHostnames(evDir)
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
		// Metadata used only to feed hostname scan tokens — not published evidence.
		// Skip content scan so the stored live hostname itself is not a self-leak.
		if e.Name() == liveScanContextFile {
			seen++
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
		// Nested JSON + escaped keys (e.g. trust_launch stdout embedding "thought").
		if contentHasPrivateReasoning(b) {
			rawThoughts = true
		}
		// Local host identity / absolute paths (#168 / GPT P1-C).
		if contentHasLocalIdentityLeak(s, scanHosts...) {
			hits++
			fails = append(fails, "local_identity:"+e.Name())
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
	// Complete only when every seen entry was scanned and at least one file scanned
	// and no privacy failure classes (including local identity leaks).
	out["complete"] = skipped == 0 && scanned > 0 && len(fails) == 0
	return out
}

// contentHasLocalIdentityLeak reports home/tmp absolute paths or .local hostnames in evidence.
// Covers Unix and Windows (including JSON-escaped backslashes after Marshal).
// extraHostnames are additional hosts to token-scan (live executor hostname on derived reports).
func contentHasLocalIdentityLeak(s string, extraHostnames ...string) bool {
	if s == "" {
		return false
	}
	// Already redacted placeholders are fine as tokens; residual raw paths still fail.
	if strings.Contains(s, "/Users/") || strings.Contains(s, "/home/") {
		return true
	}
	if strings.Contains(s, "/var/folders/") {
		return true
	}
	// Windows user roots: C:\Users\…, C:\\Users\\… (JSON), C:/Users/…
	if winUsersPath.MatchString(s) || winUsersPathEscaped.MatchString(s) || winUsersPathSlash.MatchString(s) {
		return true
	}
	if strings.Contains(s, `\Users\`) || strings.Contains(s, `\\Users\\`) {
		return true
	}
	if localHostname.MatchString(s) {
		return true
	}
	// Hostnames without .local suffix (Linux CI / containers / live executor).
	// Always strip [HOSTNAME] placeholders first so mixed placeholder+raw fails (Codex P1).
	// Match token boundaries only — unrestricted Contains false-flags schema ids when
	// hostname is a common word like "build" (Codex P2 on tip 608cdcc).
	hosts := make([]string, 0, 2+len(extraHostnames))
	if h, err := os.Hostname(); err == nil {
		hosts = append(hosts, h)
	}
	hosts = append(hosts, extraHostnames...)
	strippedHost := strings.ReplaceAll(s, "[HOSTNAME]", "")
	seenH := map[string]struct{}{}
	for _, h := range hosts {
		h = strings.TrimSpace(h)
		if h == "" || h == "localhost" || len(h) <= 1 {
			continue
		}
		key := strings.ToLower(h)
		if _, dup := seenH[key]; dup {
			continue
		}
		seenH[key] = struct{}{}
		if hostnameTokenPresent(strippedHost, h) {
			return true
		}
	}
	// Unhashed temp paths (Unix /tmp and Windows Temp). Strip [TMP:…] placeholders first
	// so a mixed file with one good placeholder and one raw path still fails (#Codex P2).
	withoutPlaceholders := localTmpPlaceholder.ReplaceAllString(s, "")
	// Also strip bare [TMP] env-root replacements and identity placeholders.
	withoutPlaceholders = strings.ReplaceAll(withoutPlaceholders, "[TMP]", "")
	withoutPlaceholders = strings.ReplaceAll(withoutPlaceholders, "[HOSTNAME]", "")
	withoutPlaceholders = strings.ReplaceAll(withoutPlaceholders, "[HOME]", "")
	if localTmpPath.MatchString(withoutPlaceholders) {
		return true
	}
	if winTempPath.MatchString(withoutPlaceholders) || winTempPathEscaped.MatchString(withoutPlaceholders) {
		return true
	}
	if strings.Contains(withoutPlaceholders, `\AppData\Local\Temp`) ||
		strings.Contains(withoutPlaceholders, `\\AppData\\Local\\Temp`) ||
		strings.Contains(withoutPlaceholders, `\Windows\Temp`) ||
		strings.Contains(withoutPlaceholders, `\\Windows\\Temp`) {
		return true
	}
	// Residual ls -l owner before redaction (including mid-line JSON stdout).
	// Skip rows already rewritten to [USER]/[GROUP] so redacted evidence is clean.
	for _, m := range lsOwnerGroup.FindAllStringSubmatch(s, -1) {
		if len(m) < 6 {
			continue
		}
		owner, group := m[3], m[5]
		if owner == "[USER]" || group == "[GROUP]" {
			continue
		}
		// Real residual ownership columns.
		return true
	}
	// Residual env account names (len>=3) only in path/ownership-token contexts —
	// not unrestricted substrings (Pro R7 P2: USER=agent must not match "agent stdio").
	withoutUserPlaceholders := strings.ReplaceAll(withoutPlaceholders, "[USER]", "")
	withoutUserPlaceholders = strings.ReplaceAll(withoutUserPlaceholders, "[GROUP]", "")
	for _, key := range []string{"USER", "LOGNAME", "USERNAME"} {
		u := strings.TrimSpace(os.Getenv(key))
		if u == "" || u == "root" || len(u) < 3 {
			continue
		}
		if contentHasAccountTokenLeak(withoutUserPlaceholders, u) {
			return true
		}
	}
	return false
}

// contentHasAccountTokenLeak reports residual account names only as path segments
// or parsed ls -l owner/group columns — never arbitrary quote/whitespace tokens
// (Pro R13 P2: USER=grok must not flag {"version":"grok 1.0.0"}).
func contentHasAccountTokenLeak(s, user string) bool {
	if user == "" {
		return false
	}
	// Path segments.
	for _, pref := range []string{"/Users/", "/home/", `\Users\`, `\\Users\\`} {
		if strings.Contains(s, pref+user) {
			return true
		}
	}
	// ls -l ownership columns only (mode links owner group …).
	// Example: "lrwxr-xr-x@ 1 alice  staff  27 …"
	re := regexp.MustCompile(`(?m)(?:^|[\s"\\])[l-][rwxSsTt-]{9}[@+]?\s+\d+\s+` + regexp.QuoteMeta(user) + `\s+\S+`)
	if re.MatchString(s) {
		return true
	}
	// Group column as second identity field after a numeric links count.
	reG := regexp.MustCompile(`(?m)(?:^|[\s"\\])[l-][rwxSsTt-]{9}[@+]?\s+\d+\s+\S+\s+` + regexp.QuoteMeta(user) + `(?:[\s"\\]|$)`)
	return reG.MatchString(s)
}

// liveIdentitySchema is the sealed schema id for live executor provenance.
const liveIdentitySchema = "reinframe.live_identity.v1"

// liveGrokExeSchema seals the external Grok CLI binary content binding for a run.
const liveGrokExeSchema = "reinframe.live_grok_executable.v1"

// liveScanContextSchema / file hold the live-executor hostname for privacy scan only
// (not redacted, excluded from content scan so it cannot self-leak).
const (
	liveScanContextSchema = "reinframe.live_scan_context.v1"
	liveScanContextFile   = "live_scan_context.json"
)

// liveIdentity holds a verified live-executor identity (never the report generator).
type liveIdentity struct {
	Commit string
	Dirty  bool
	Src    string
	GOOS   string
	GOARCH string
	OK     bool
	Err    string // non-empty when OK is false
}

// isValidGOOS / isValidGOARCH reject path traversal and non-platform tokens before
// live_goos/live_goarch are used in report basenames (Codex GraphQL P2 on #230).
func isValidGOOS(s string) bool {
	switch strings.TrimSpace(s) {
	case "aix", "android", "darwin", "dragonfly", "freebsd", "hurd", "illumos",
		"ios", "js", "linux", "nacl", "netbsd", "openbsd", "plan9", "solaris",
		"wasip1", "windows", "zos":
		return true
	default:
		return false
	}
}

func isValidGOARCH(s string) bool {
	switch strings.TrimSpace(s) {
	case "386", "amd64", "amd64p32", "arm", "arm64", "arm64be", "armbe",
		"loong64", "mips", "mips64", "mips64le", "mips64p32", "mips64p32le",
		"mipsle", "ppc", "ppc64", "ppc64le", "riscv", "riscv64", "s390",
		"s390x", "sparc", "sparc64", "wasm":
		return true
	default:
		return false
	}
}

// writeLiveScanContext records the live host's hostname without writeJSON redaction
// so a later derived report on another machine can still scan for that token.
func writeLiveScanContext(evDir string) error {
	h, err := os.Hostname()
	if err != nil {
		h = ""
	}
	h = strings.TrimSpace(h)
	if h == "" {
		// Still write an empty record so report can see context was attempted.
		h = ""
	}
	if err := os.MkdirAll(evDir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(map[string]any{
		"schema":        liveScanContextSchema,
		"live_hostname": h,
		"at":            stamp(),
	}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(evDir, liveScanContextFile), b, 0o600)
}

// privacyScanHostnames returns generator + live-executor hostnames for leak checks.
func privacyScanHostnames(evDir string) []string {
	var out []string
	if h, err := os.Hostname(); err == nil {
		if t := strings.TrimSpace(h); t != "" {
			out = append(out, t)
		}
	}
	b, err := os.ReadFile(filepath.Join(evDir, liveScanContextFile))
	if err != nil {
		return out
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return out
	}
	if schema, _ := m["schema"].(string); strings.TrimSpace(schema) != liveScanContextSchema {
		return out
	}
	if h, _ := m["live_hostname"].(string); strings.TrimSpace(h) != "" {
		out = append(out, strings.TrimSpace(h))
	}
	return out
}

// writeLiveIdentity records the live executor binary identity for later report provenance.
func writeLiveIdentity(evDir string) error {
	rev, dirty, src := reinframeBuildIdentity()
	if rev == "" || src == "unknown" || !isFullVCSRevision(rev) {
		return fmt.Errorf("writeLiveIdentity: current binary lacks qualifying identity (rev=%q src=%s)", rev, src)
	}
	if !isValidGOOS(runtime.GOOS) || !isValidGOARCH(runtime.GOARCH) {
		return fmt.Errorf("writeLiveIdentity: unexpected runtime platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if err := writeLiveScanContext(evDir); err != nil {
		return fmt.Errorf("writeLiveIdentity: scan context: %w", err)
	}
	return writeJSON(filepath.Join(evDir, "live_identity.json"), map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     rev,
		"live_binary_dirty":      dirty,
		"live_binary_commit_src": src,
		// Platform of the live campaign host (Pro R10 P2: do not attribute generator GOOS).
		"live_goos":   runtime.GOOS,
		"live_goarch": runtime.GOARCH,
		"at":          stamp(),
	})
}

// ensureLiveIdentity creates live_identity.json if missing, or verifies an existing file
// matches the current binary. Used by standalone hooks/acp and runAll so report never
// has to invent identity from the generator.
//
// Refuse to retrofit identity onto a directory that already holds live phase outputs
// without identity (Codex P1: mixed pre-change scenarios + new binary stamp).
func ensureLiveIdentity(evDir string) error {
	if err := os.MkdirAll(evDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(evDir, "live_identity.json")
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		id := loadLiveIdentity(evDir)
		if !id.OK {
			return fmt.Errorf("ensureLiveIdentity: existing file invalid: %s", id.Err)
		}
		rev, dirty, src := reinframeBuildIdentity()
		if id.Commit != rev || id.Dirty != dirty || id.Src != src {
			return fmt.Errorf("ensureLiveIdentity: live_identity mismatch current binary (live=%s dirty=%v src=%s; current=%s dirty=%v src=%s)",
				id.Commit, id.Dirty, id.Src, rev, dirty, src)
		}
		// When platform is bound, refuse cross-platform retrofit of the same commit.
		if id.GOOS != "" && id.GOARCH != "" && (id.GOOS != runtime.GOOS || id.GOARCH != runtime.GOARCH) {
			return fmt.Errorf("ensureLiveIdentity: live platform mismatch (live=%s/%s; current=%s/%s)",
				id.GOOS, id.GOARCH, runtime.GOOS, runtime.GOARCH)
		}
		return nil
	}
	if hasExistingLiveEvidenceWithoutIdentity(evDir) {
		return fmt.Errorf("ensureLiveIdentity: refuse to retrofit live_identity onto existing live evidence (scenarios/hooks/acp artifacts present without identity)")
	}
	if err := writeLiveIdentity(evDir); err != nil {
		return err
	}
	// Atomic create: refuse to proceed if the written file is not loadable.
	id := loadLiveIdentity(evDir)
	if !id.OK {
		return fmt.Errorf("ensureLiveIdentity: post-write verification failed: %s", id.Err)
	}
	return nil
}

// hasExistingLiveEvidenceWithoutIdentity reports phase artifacts that bind a run
// to a prior binary (scenarios, hooks, trust, acp) when live_identity.json is absent.
func hasExistingLiveEvidenceWithoutIdentity(evDir string) bool {
	for _, name := range []string{
		"scenarios.json",
		"preflight.json",
		"hook_invocations.jsonl",
		"trust_launch.json",
		"acp_manifest.json",
		"hooks_doctor_pre_trust.json",
		"hooks_doctor_post_trust.json",
	} {
		st, err := os.Stat(filepath.Join(evDir, name))
		if err == nil && !st.IsDir() && st.Size() > 0 {
			return true
		}
	}
	return false
}

// parseLiveIdentityJSON validates a complete live_identity document (no silent partials).
func parseLiveIdentityJSON(b []byte) (liveIdentity, error) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return liveIdentity{}, fmt.Errorf("live_identity.json malformed: %w", err)
	}
	if m == nil {
		return liveIdentity{}, fmt.Errorf("live_identity.json empty object")
	}
	schema, _ := m["schema"].(string)
	if strings.TrimSpace(schema) != liveIdentitySchema {
		return liveIdentity{}, fmt.Errorf("live_identity.json schema must be %s", liveIdentitySchema)
	}
	commit, ok := m["live_binary_commit"].(string)
	commit = strings.TrimSpace(commit)
	if !ok || commit == "" {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing live_binary_commit")
	}
	if !isFullVCSRevision(commit) {
		return liveIdentity{}, fmt.Errorf("live_identity.json live_binary_commit not full VCS revision")
	}
	dirty, ok := m["live_binary_dirty"].(bool)
	if !ok {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing live_binary_dirty bool")
	}
	src, ok := m["live_binary_commit_src"].(string)
	src = strings.TrimSpace(src)
	if !ok || src == "" {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing live_binary_commit_src")
	}
	switch src {
	case "ldflags", "vcs":
		// ok
	case "unknown":
		return liveIdentity{}, fmt.Errorf("live_identity.json live_binary_commit_src=unknown cannot qualify")
	default:
		return liveIdentity{}, fmt.Errorf("live_identity.json live_binary_commit_src %q not allowed", src)
	}
	// Platform fields are mandatory for a valid live identity (Codex GraphQL P2 on #230).
	// Absent fields previously allowed generator GOOS/GOARCH fallback into provenance naming.
	goos, okGOOS := m["live_goos"].(string)
	goarch, okGOARCH := m["live_goarch"].(string)
	goos = strings.TrimSpace(goos)
	goarch = strings.TrimSpace(goarch)
	if !okGOOS || goos == "" {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing live_goos")
	}
	if !okGOARCH || goarch == "" {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing live_goarch")
	}
	// Reject path traversal / non-GOOS tokens before basename use (Codex #230 P2).
	if !isValidGOOS(goos) {
		return liveIdentity{}, fmt.Errorf("live_identity.json live_goos %q not a known GOOS", goos)
	}
	if !isValidGOARCH(goarch) {
		return liveIdentity{}, fmt.Errorf("live_identity.json live_goarch %q not a known GOARCH", goarch)
	}
	return liveIdentity{Commit: commit, Dirty: dirty, Src: src, GOOS: goos, GOARCH: goarch, OK: true}, nil
}

// loadLiveIdentity loads and validates live_identity.json. On any failure OK=false and
// Err is set — never substitutes the report-generator identity.
func loadLiveIdentity(evDir string) liveIdentity {
	b, err := os.ReadFile(filepath.Join(evDir, "live_identity.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return liveIdentity{Err: "live_identity.json missing"}
		}
		return liveIdentity{Err: "live_identity.json unreadable: " + err.Error()}
	}
	id, err := parseLiveIdentityJSON(b)
	if err != nil {
		return liveIdentity{Err: err.Error()}
	}
	return id
}

// fileContentSHA256 returns the hex SHA-256 of file contents (not the path string).
func fileContentSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ensureGrokExecutableIdentity records or verifies the live Grok CLI binary content
// hash so standalone preflight/hooks/acp cannot swap --grok-executable mid-run
// (Pro R6 P1). Path string hashing alone is insufficient.
func ensureGrokExecutableIdentity(evDir, grokExe string) error {
	if strings.TrimSpace(grokExe) == "" {
		return fmt.Errorf("ensureGrokExecutableIdentity: grok executable path required")
	}
	abs, err := filepath.Abs(grokExe)
	if err != nil {
		return fmt.Errorf("ensureGrokExecutableIdentity: abs: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("ensureGrokExecutableIdentity: stat %s: %w", abs, err)
	}
	if st.IsDir() {
		return fmt.Errorf("ensureGrokExecutableIdentity: %s is a directory", abs)
	}
	sum, err := fileContentSHA256(abs)
	if err != nil {
		return fmt.Errorf("ensureGrokExecutableIdentity: hash: %w", err)
	}
	if err := os.MkdirAll(evDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(evDir, "live_grok_executable.json")
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("ensureGrokExecutableIdentity: read existing: %w", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("ensureGrokExecutableIdentity: existing malformed: %w", err)
		}
		schema, _ := m["schema"].(string)
		if strings.TrimSpace(schema) != liveGrokExeSchema {
			return fmt.Errorf("ensureGrokExecutableIdentity: schema must be %s", liveGrokExeSchema)
		}
		prevSum, _ := m["grok_executable_sha256"].(string)
		if strings.TrimSpace(prevSum) == "" || prevSum != sum {
			return fmt.Errorf("ensureGrokExecutableIdentity: grok executable content mismatch (live=%s current=%s)", prevSum, sum)
		}
		return nil
	}
	return writeJSON(path, map[string]any{
		"schema":                  liveGrokExeSchema,
		"grok_executable_path":    abs,
		"grok_executable_sha256":  sum,
		"at":                      stamp(),
	})
}

// loadLiveGrokExecutableOK reports whether live_grok_executable.json is complete.
func loadLiveGrokExecutableOK(evDir string) (ok bool, reason string) {
	b, err := os.ReadFile(filepath.Join(evDir, "live_grok_executable.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, "live_grok_executable.json missing"
		}
		return false, "live_grok_executable.json unreadable: " + err.Error()
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false, "live_grok_executable.json malformed"
	}
	if schema, _ := m["schema"].(string); strings.TrimSpace(schema) != liveGrokExeSchema {
		return false, "live_grok_executable.json schema invalid"
	}
	sum, _ := m["grok_executable_sha256"].(string)
	if strings.TrimSpace(sum) == "" || len(sum) != 64 {
		return false, "live_grok_executable.json missing content sha256"
	}
	return true, ""
}

// liveGrokhooksSchema seals the grokhooks helper content binding for hooks campaigns.
const liveGrokhooksSchema = "reinframe.live_grokhooks_executable.v1"

// ensureGrokhooksExecutable records or verifies the hooks helper binary content
// so --grokhooks cannot swap mid-run independently of live_binary_commit (Pro R14 P1).
func ensureGrokhooksExecutable(evDir, hooksExe string) error {
	if strings.TrimSpace(hooksExe) == "" {
		return fmt.Errorf("ensureGrokhooksExecutable: grokhooks path required")
	}
	abs, err := filepath.Abs(hooksExe)
	if err != nil {
		return fmt.Errorf("ensureGrokhooksExecutable: abs: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("ensureGrokhooksExecutable: stat %s: %w", abs, err)
	}
	if st.IsDir() {
		return fmt.Errorf("ensureGrokhooksExecutable: %s is a directory", abs)
	}
	sum, err := fileContentSHA256(abs)
	if err != nil {
		return fmt.Errorf("ensureGrokhooksExecutable: hash: %w", err)
	}
	if err := os.MkdirAll(evDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(evDir, "live_grokhooks_executable.json")
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("ensureGrokhooksExecutable: read existing: %w", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return fmt.Errorf("ensureGrokhooksExecutable: existing malformed: %w", err)
		}
		schema, _ := m["schema"].(string)
		if strings.TrimSpace(schema) != liveGrokhooksSchema {
			return fmt.Errorf("ensureGrokhooksExecutable: schema must be %s", liveGrokhooksSchema)
		}
		prevSum, _ := m["grokhooks_executable_sha256"].(string)
		if strings.TrimSpace(prevSum) == "" || prevSum != sum {
			return fmt.Errorf("ensureGrokhooksExecutable: content mismatch (live=%s current=%s)", prevSum, sum)
		}
		// Do not compare grokhooks_executable_path: writeJSON redacts home/tmp paths to
		// [HOME]/[TMP:…] so a second bind always false-mismatched absolute paths
		// (Codex GraphQL P1 on #230). Content SHA-256 is the authoritative bind —
		// same contract as ensureGrokExecutableIdentity.
		return nil
	}
	return writeJSON(path, map[string]any{
		"schema":                       liveGrokhooksSchema,
		"grokhooks_executable_path":    abs,
		"grokhooks_executable_sha256":  sum,
		"at":                           stamp(),
	})
}

// hasHookScenarios reports whether any HOOK-* scenario is present in evidence.
func hasHookScenarios(scenarios map[string]ScenarioResult) bool {
	for id := range scenarios {
		if strings.HasPrefix(id, "HOOK-") {
			return true
		}
	}
	return false
}

// loadLiveGrokhooksExecutableOK reports whether live_grokhooks_executable.json is complete.
func loadLiveGrokhooksExecutableOK(evDir string) (ok bool, reason string) {
	b, err := os.ReadFile(filepath.Join(evDir, "live_grokhooks_executable.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return false, "live_grokhooks_executable.json missing"
		}
		return false, "live_grokhooks_executable.json unreadable: " + err.Error()
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false, "live_grokhooks_executable.json malformed"
	}
	if schema, _ := m["schema"].(string); strings.TrimSpace(schema) != liveGrokhooksSchema {
		return false, "live_grokhooks_executable.json schema invalid"
	}
	sum, _ := m["grokhooks_executable_sha256"].(string)
	if strings.TrimSpace(sum) == "" || len(sum) != 64 {
		return false, "live_grokhooks_executable.json missing content sha256"
	}
	return true, ""
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
