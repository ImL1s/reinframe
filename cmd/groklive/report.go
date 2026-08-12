package main

import (
	"bytes"
	"crypto/rand"
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
	"sync"
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
	ctxIn := fs.String("scan-context-in", "", "optional external live_scan_context JSON (outside evidence-out)")
	_ = fs.Parse(args)
	evDir := mustAbs(*out, "--evidence-out")
	if s := strings.TrimSpace(*ctxIn); s != "" {
		abs, err := filepath.Abs(s)
		if err != nil {
			fail(fmt.Errorf("groklive report: --scan-context-in: %w", err))
		}
		scanContextInPath = abs
	}
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

	// Bind imported live-executor hostnames into free-text redactors before any
	// writeJSON/Markdown emission (Pro R32 P1: cross-host residual live host).
	if ctxHost, ctxOK, _ := loadLiveScanContext(evDir); ctxOK {
		setExtraRedactHostnames(ctxHost)
		defer clearExtraRedactHostnames()
	}

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
	// Campaign platform: only use live-bound values. Do not silently rename with
	// generator GOOS when identity is incomplete (Pro R22 P2).
	osName := liveID.GOOS
	liveArch := liveID.GOARCH
	if !liveID.OK || osName == "" || !isValidGOOS(osName) {
		osName = "unknown"
	}
	if !liveID.OK || liveArch == "" || !isValidGOARCH(liveArch) {
		liveArch = "unknown"
	}
	// Basename path-component guard (never separators / traversal).
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
			// derived=true unless a complete live identity proves live==generator
			// (Pro R22 P2: incomplete identity must not claim derived=false).
			"derived": !liveID.OK || liveBinaryCommit == "" || commit == "" ||
				liveBinaryCommit != commit || liveBinaryDirty != dirty ||
				(liveID.GOOS != "" && liveID.GOOS != runtime.GOOS) ||
				(liveID.GOARCH != "" && liveID.GOARCH != runtime.GOARCH),
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

	// Strip any legacy in-evidence bare-hostname control file. Private cache
	// context is kept so standalone report re-runs remain possible (Pro R18).
	if err := scrubLegacyInEvidenceScanContext(evDir); err != nil {
		_ = os.Remove(jsonPath)
		_ = os.Remove(mdPath)
		_ = os.Remove(schemaPath)
		return liveReportOutcome{}, fmt.Errorf("scrub legacy live_scan_context from evidence: %w", err)
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
	// Live scan context: fail-closed when live_identity is present (Pro R17 P1).
	// Control file is not counted in files_seen/scanned (scrubbed after report; not
	// published evidence). Hostname is used only for residual leak detection.
	var fails []string
	ctxHost, ctxOK, ctxWhy := loadLiveScanContext(evDir)
	idPath := filepath.Join(evDir, "live_identity.json")
	if st, err := os.Stat(idPath); err == nil && !st.IsDir() {
		if !ctxOK {
			fails = append(fails, "live_scan_context:"+ctxWhy)
		}
	}
	scanHosts := privacyScanHostnames(evDir, ctxHost, ctxOK)
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
	for _, e := range entries {
		// Evidence dir is flat: nested directories forbid complete qualification.
		if e.IsDir() {
			seen++
			skipped++
			fails = append(fails, "nested_directory:"+e.Name())
			continue
		}
		// Any raw scan-context control file under evidence is a publication failure
		// (Pro R28 P1) — do not silently skip; mark incomplete.
		if e.Name() == liveScanContextFile || e.Name() == liveScanContextPortableFile {
			seen++
			fails = append(fails, "scan_context_in_evidence:"+e.Name())
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
		// Filenames also publish identity (Pro R35 P1): build-01.log with clean body
		// must not allow privacy.complete=true.
		if contentHasLocalIdentityLeak(e.Name(), scanHosts...) {
			hits++
			fails = append(fails, "local_identity_filename:"+e.Name())
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

// structuredJSONEnumPairRE matches candidate closed structured key/value pairs.
// Only validated enum values are blanked — arbitrary values under the same keys
// must remain visible to hostname scan (Pro R31 P1).
var structuredJSONEnumPairRE = regexp.MustCompile(
	`"(?P<key>goos|goarch|live_goos|live_goarch|report_generator_goos|report_generator_goarch|` +
		`final_disposition|disposition|strongest_proven|status|class|src|` +
		`live_binary_commit_src|report_generator_commit_src|schema)"\s*:\s*"(?P<val>[^"]*)"`)

// isClosedStructuredEnumValue reports whether val is a field-specific closed enum
// for a structured key. Invalid/arbitrary values must NOT be exempted from leak scan.
func isClosedStructuredEnumValue(key, val string) bool {
	switch key {
	case "goos", "live_goos", "report_generator_goos":
		return isValidGOOS(val)
	case "goarch", "live_goarch", "report_generator_goarch":
		return isValidGOARCH(val)
	case "final_disposition", "disposition":
		switch val {
		case "GO", "LIMITED_GO", "MORE_DATA", "NO_GO":
			return true
		}
	case "strongest_proven":
		switch val {
		case "transport", "session_visible", "explicit", "unknown":
			return true
		}
	case "status":
		switch val {
		case "PASS", "FAIL", "NOT_RUN", "INCONCLUSIVE":
			return true
		}
	case "class":
		switch val {
		case "binary_absent", "other_external_environment":
			return true
		}
	case "src", "live_binary_commit_src", "report_generator_commit_src":
		switch val {
		case "ldflags", "vcs", "unknown":
			return true
		}
	case "schema":
		// Explicit schema registry only (Pro R32 P1: no broad reinframe.* fallback
		// that can blank hostname-bearing values like reinframe.x/build-01.v1).
		switch val {
		case liveIdentitySchema, liveScanContextSchema,
			"reinframe.grok_build_live_control.v2",
			"reinframe.grok_build_acp.v1",
			"reinframe.grok_build.v1",
			"reinframe.grok_build_live_control.v1":
			return true
		}
	}
	return false
}

// blankClosedStructuredEnumValues removes only field-validated closed enum values
// so hostnames colliding with GOOS/GOARCH tokens do not false-flag (Pro R26 P2),
// while invalid values under the same keys stay scannable (Pro R31 P1).
func blankClosedStructuredEnumValues(s string) string {
	return structuredJSONEnumPairRE.ReplaceAllStringFunc(s, func(m string) string {
		sub := structuredJSONEnumPairRE.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		key, val := sub[1], sub[2]
		if isClosedStructuredEnumValue(key, val) {
			return `"":""`
		}
		return m
	})
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
	// Residual --project root (outside HOME/TMP) is a leak (Pro R31/R32 P1).
	if p := strings.TrimSpace(liveProjectRoot); p != "" && len(p) >= 3 {
		if strings.Contains(s, p) {
			return true
		}
		// JSON-escaped Windows separators.
		if strings.Contains(p, `\`) {
			esc := strings.ReplaceAll(p, `\`, `\\`)
			if strings.Contains(s, esc) {
				return true
			}
		}
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
	// Blank only validated closed structured enum/platform values before hostname
	// token scan (Pro R26 P2 + Pro R31 P1: invalid status values remain scannable).
	hosts := make([]string, 0, 2+len(extraHostnames))
	if h, err := os.Hostname(); err == nil {
		hosts = append(hosts, h)
	}
	hosts = append(hosts, extraHostnames...)
	strippedHost := strings.ReplaceAll(s, "[HOSTNAME]", "")
	strippedHost = blankClosedStructuredEnumValues(strippedHost)
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
	// Legacy accidental under-evidence portable name (Pro R28 P1): never write;
	// presence in evidence is a privacy failure.
	liveScanContextPortableFile = ".live_scan_context.private.json"
)

// External scan-context transfer paths (Pro R28 P1). Must stay outside --evidence-out.
// Set via report/all flags or tests; empty means private-cache only.
var (
	scanContextOutPath string
	scanContextInPath  string
)

// liveIdentity holds a verified live-executor identity (never the report generator).
type liveIdentity struct {
	Commit        string
	Dirty         bool
	Src           string
	GOOS          string
	GOARCH        string
	ScanContextID string // binds private hostname context to this campaign (Pro R19)
	OK            bool
	Err           string // non-empty when OK is false
}

// privateCacheRootFn is overridable in tests (Pro R19: inject XDG-style roots).
var privateCacheRootFn = defaultPrivateCacheRoot

func defaultPrivateCacheRoot() (string, error) {
	if d, err := os.UserCacheDir(); err == nil && strings.TrimSpace(d) != "" {
		return d, nil
	}
	if d, err := os.UserConfigDir(); err == nil && strings.TrimSpace(d) != "" {
		return d, nil
	}
	return "", fmt.Errorf("no user cache/config dir")
}

func newScanContextID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// realPathBestEffort returns a cleaned absolute path with as many existing
// components symlink-resolved as possible (Pro R20/R21 containment).
func realPathBestEffort(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return filepath.Clean(p)
	}
	abs = filepath.Clean(abs)
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r
	}
	// Resolve longest existing ancestor, re-join missing suffix.
	suffix := ""
	cur := abs
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			if suffix == "" {
				return r
			}
			return filepath.Join(r, suffix)
		}
		dir := filepath.Dir(cur)
		if dir == cur {
			break
		}
		base := filepath.Base(cur)
		if suffix == "" {
			suffix = base
		} else {
			suffix = filepath.Join(base, suffix)
		}
		cur = dir
	}
	return abs
}

// pathContainedIn reports whether child is equal to parent or nested under it.
// Uses resolved paths + os.SameFile on every ancestor of both the lexical child
// and its symlink-resolved form, so:
//   - case-insensitive APFS aliases (Evidence vs eVIDENCE) are caught (Pro R20)
//   - cache-root symlink into evidence/subdir is caught (Pro R21 P1)
func pathContainedIn(child, parent string) (bool, error) {
	pAbs, err := filepath.Abs(parent)
	if err != nil {
		return false, err
	}
	pAbs = filepath.Clean(pAbs)
	pReal := realPathBestEffort(pAbs)
	pInfo, err := os.Stat(pReal)
	if err != nil {
		// Parent must exist for a meaningful containment decision.
		return false, err
	}

	cAbs, err := filepath.Abs(child)
	if err != nil {
		return false, err
	}
	cAbs = filepath.Clean(cAbs)
	cReal := realPathBestEffort(cAbs)

	// Walk both lexical and resolved child ancestors; SameFile vs evidence root.
	for _, start := range []string{cAbs, cReal} {
		cur := start
		for {
			if info, err := os.Stat(cur); err == nil {
				if os.SameFile(info, pInfo) {
					return true, nil
				}
			}
			if eval, err := filepath.EvalSymlinks(cur); err == nil && eval != cur {
				if info, err := os.Stat(eval); err == nil && os.SameFile(info, pInfo) {
					return true, nil
				}
				// Walk resolved target ancestors too (symlink → evidence/subdir).
				rcur := eval
				for {
					if info, err := os.Stat(rcur); err == nil && os.SameFile(info, pInfo) {
						return true, nil
					}
					// Also: is rcur under pReal via Rel after both real?
					if underLexical(rcur, pReal) {
						return true, nil
					}
					next := filepath.Dir(rcur)
					if next == rcur {
						break
					}
					rcur = next
				}
			}
			if underLexical(cur, pReal) || underLexical(cur, pAbs) {
				return true, nil
			}
			next := filepath.Dir(cur)
			if next == cur {
				break
			}
			cur = next
		}
	}
	return underLexical(cReal, pReal) || underLexical(cAbs, pAbs) || underLexical(cReal, pAbs), nil
}

// pathVolumeCaseInsensitive reports whether lexical path comparison for the
// given paths should fold case. Windows always folds; Linux never; Darwin probes
// the target volume (not the process temp dir) so mixed APFS mounts are correct
// (Pro R33 P2 + Pro R34 P2). os.SameFile still catches real case-aliases when
// both paths exist.
func pathVolumeCaseInsensitive(paths ...string) bool {
	switch runtime.GOOS {
	case "windows":
		return true
	case "darwin":
		return darwinVolumeCaseInsensitive(paths...)
	default:
		return false
	}
}

var (
	darwinVolMu    sync.Mutex
	darwinVolCache = map[string]bool{} // key: device id string of probe root
)

// nearestExistingDir walks up to the first existing directory for probing.
func nearestExistingDir(p string) string {
	p = filepath.Clean(p)
	if p == "" {
		return ""
	}
	cur := p
	for {
		if st, err := os.Stat(cur); err == nil {
			if st.IsDir() {
				return cur
			}
			return filepath.Dir(cur)
		}
		next := filepath.Dir(cur)
		if next == cur {
			return ""
		}
		cur = next
	}
}

func darwinVolumeCaseInsensitive(paths ...string) bool {
	// Prefer an existing ancestor of the caller paths so the probe runs on the
	// same volume as evidence/cache (Pro R34 P2). Fall back to system temp only
	// when no path is usable.
	var roots []string
	for _, p := range paths {
		if d := nearestExistingDir(p); d != "" {
			roots = append(roots, d)
		}
	}
	if len(roots) == 0 {
		if t := os.TempDir(); t != "" {
			roots = append(roots, t)
		}
	}
	if len(roots) == 0 {
		return false
	}
	// If any compared path's volume is case-sensitive, do not fold (safe for
	// mixed mounts: SameFile still catches true aliases on folding volumes).
	allFold := true
	any := false
	for _, root := range roots {
		fold, ok := probeDarwinVolumeCase(root)
		if !ok {
			continue
		}
		any = true
		if !fold {
			allFold = false
			break
		}
	}
	if !any {
		return false
	}
	return allFold
}

// volumeCacheKey identifies the filesystem volume for case-sensitivity cache.
// Portable: prefer device id via FileInfo.Sys when the type is known at runtime
// without importing platform-specific syscall.Stat_t (Windows build).
func volumeCacheKey(root string, st os.FileInfo) string {
	// Reflect-free portable approach: use root path. Same volume paths share
	// nearestExistingDir ancestors, so sibling probes reuse the same root key
	// after the first nearestExistingDir walk lands on the volume mount.
	return filepath.Clean(root)
}

func probeDarwinVolumeCase(root string) (fold bool, ok bool) {
	root = filepath.Clean(root)
	if root == "" {
		return false, false
	}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return false, false
	}
	key := volumeCacheKey(root, st)
	darwinVolMu.Lock()
	if v, hit := darwinVolCache[key]; hit {
		darwinVolMu.Unlock()
		return v, true
	}
	darwinVolMu.Unlock()

	// Probe inside root: create unique subdir to avoid collisions.
	probeDir, err := os.MkdirTemp(root, ".rf-case-probe-*")
	if err != nil {
		// Unwritable volume: fail open toward case-sensitive.
		return false, false
	}
	defer func() { _ = os.RemoveAll(probeDir) }()
	p := filepath.Join(probeDir, "a")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		return false, false
	}
	alt := filepath.Join(probeDir, "A")
	stA, errA := os.Stat(p)
	stB, errB := os.Stat(alt)
	if errA != nil || errB != nil {
		// Case-sensitive: "A" does not exist as alias of "a".
		fold = false
	} else {
		fold = os.SameFile(stA, stB)
	}
	darwinVolMu.Lock()
	darwinVolCache[key] = fold
	darwinVolMu.Unlock()
	return fold, true
}

func underLexical(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	if child == parent {
		return true
	}
	sep := string(os.PathSeparator)
	// Case-sensitive volumes (Linux, case-sensitive APFS): exact match only.
	// Unconditional ToLower falsely merges /workspace/cache with /workspace/Cache
	// (Pro R33 P2). Volume-scoped on Darwin (Pro R34 P2).
	if !pathVolumeCaseInsensitive(child, parent) {
		if strings.HasPrefix(child, parent+sep) {
			return true
		}
		if rel, err := filepath.Rel(parent, child); err == nil {
			if rel == "." {
				return true
			}
			if rel != ".." && !strings.HasPrefix(rel, ".."+sep) {
				return true
			}
		}
		return false
	}
	// Case-insensitive volumes: fold case for equality and prefix (Windows/APFS).
	if equalFoldPath(child, parent) {
		return true
	}
	cl, pl := strings.ToLower(child), strings.ToLower(parent)
	if strings.HasPrefix(cl, pl+sep) {
		return true
	}
	if rel, err := filepath.Rel(parent, child); err == nil {
		if rel == "." {
			return true
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+sep) {
			return true
		}
	}
	if rel, err := filepath.Rel(pl, cl); err == nil {
		if rel == "." {
			return true
		}
		if rel != ".." && !strings.HasPrefix(rel, ".."+sep) {
			return true
		}
	}
	return false
}

func equalFoldPath(a, b string) bool {
	if a == b {
		return true
	}
	if !pathVolumeCaseInsensitive(a, b) {
		return false
	}
	return strings.EqualFold(a, b)
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

// liveScanContextPath returns the private path for the bare live hostname control
// file, keyed by high-entropy scan_context_id only (Pro R26 P1).
// Evidence absolute path is intentionally NOT part of the key so rename/move of
// the evidence directory on the same host still resolves the same context.
// Rejects paths that resolve under the evidence directory (XDG_CACHE_HOME traps).
func liveScanContextPath(evDir, scanContextID string) (string, error) {
	if strings.TrimSpace(scanContextID) == "" || len(scanContextID) < 16 {
		return "", fmt.Errorf("liveScanContextPath: scan_context_id required")
	}
	abs, err := filepath.Abs(evDir)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	// Portable key: scan_context_id alone (v2). Prior v1 mixed abs(evDir) and could
	// not survive evidence rename/copy on the same machine (Pro R26 P1).
	sum := sha256.Sum256([]byte("live_scan_context.v2\n" + scanContextID))
	base, err := privateCacheRootFn()
	if err != nil || strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("liveScanContextPath: cache root: %w", err)
	}
	dir := filepath.Join(base, "reinframe-groklive", "live_scan_context")
	path := filepath.Join(dir, hex.EncodeToString(sum[:16])+".json")
	// Containment: never write private control inside evidence (Pro R19/R20).
	if under, err := pathContainedIn(path, abs); err != nil {
		return "", fmt.Errorf("liveScanContextPath: containment: %w", err)
	} else if under {
		return "", fmt.Errorf("liveScanContextPath: private path %s is inside evidence %s", path, abs)
	}
	if under, err := pathContainedIn(dir, abs); err != nil {
		return "", fmt.Errorf("liveScanContextPath: dir containment: %w", err)
	} else if under {
		return "", fmt.Errorf("liveScanContextPath: private dir %s is inside evidence %s", dir, abs)
	}
	if under, err := pathContainedIn(base, abs); err != nil {
		// base may not exist yet — ignore missing; equalFold still runs inside pathContainedIn for abs paths
		if !os.IsNotExist(err) {
			return "", fmt.Errorf("liveScanContextPath: base containment: %w", err)
		}
	} else if under {
		return "", fmt.Errorf("liveScanContextPath: private cache root %s is inside evidence %s", base, abs)
	}
	return path, nil
}

// writeLiveScanContext records the live host's hostname outside the evidence
// directory, bound to scanContextID (Pro R17–R19).
func writeLiveScanContext(evDir, scanContextID string) error {
	h, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("writeLiveScanContext: hostname: %w", err)
	}
	h = strings.TrimSpace(h)
	if h == "" || h == "localhost" || len(h) <= 1 {
		return fmt.Errorf("writeLiveScanContext: empty/localhost/short hostname cannot bind live scan context")
	}
	path, err := liveScanContextPath(evDir, scanContextID)
	if err != nil {
		return fmt.Errorf("writeLiveScanContext: path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	// Re-check containment after directory creation (SameFile now applies).
	evAbs := evDir
	if a, e := filepath.Abs(evDir); e == nil {
		evAbs = a
	}
	if under, err := pathContainedIn(path, evAbs); err != nil {
		return fmt.Errorf("writeLiveScanContext: post-mkdir containment: %w", err)
	} else if under {
		return fmt.Errorf("writeLiveScanContext: private path inside evidence after mkdir")
	}
	b, err := json.MarshalIndent(map[string]any{
		"schema":          liveScanContextSchema,
		"scan_context_id": scanContextID,
		"live_hostname":   h,
		"evidence_dir":    filepath.Clean(evAbs),
		"at":              stamp(),
	}, "", "  ")
	if err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(evDir, liveScanContextFile))
	_ = os.Remove(filepath.Join(evDir, liveScanContextPortableFile))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return err
	}
	// Optional external transfer path — must NOT be under evidence (Pro R28 P1).
	if err := writeScanContextOutFile(evAbs, b); err != nil {
		return err
	}
	return nil
}

// writeScanContextOutFile writes validated scan-context JSON to --scan-context-out
// when set. No-op when the flag is empty. Fail-closed on containment or write errors.
func writeScanContextOutFile(evAbs string, b []byte) error {
	out := strings.TrimSpace(scanContextOutPath)
	if out == "" {
		return nil
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return fmt.Errorf("scan-context-out abs: %w", err)
	}
	if under, err := pathContainedIn(outAbs, evAbs); err != nil {
		return fmt.Errorf("scan-context-out containment: %w", err)
	} else if under {
		return fmt.Errorf("--scan-context-out must be outside evidence directory")
	}
	if err := os.MkdirAll(filepath.Dir(outAbs), 0o700); err != nil {
		return fmt.Errorf("scan-context-out mkdir: %w", err)
	}
	// Atomic-ish: temp sibling then rename so partial writes are not left as final.
	tmp := outAbs + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write scan-context-out: %w", err)
	}
	if err := os.Rename(tmp, outAbs); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("write scan-context-out rename: %w", err)
	}
	return nil
}

// exportLiveScanContextOut copies the validated private scan context for this
// campaign to --scan-context-out when set (Pro R30 P2: resume existing identity).
// Fail if the user requested export but the private file cannot be read or written.
func exportLiveScanContextOut(evDir, scanContextID string) error {
	out := strings.TrimSpace(scanContextOutPath)
	if out == "" {
		return nil
	}
	scanContextID = strings.TrimSpace(scanContextID)
	if scanContextID == "" {
		return fmt.Errorf("export scan-context-out: empty scan_context_id")
	}
	path, err := liveScanContextPath(evDir, scanContextID)
	if err != nil {
		return fmt.Errorf("export scan-context-out path: %w", err)
	}
	doc, ok, why := loadLiveScanContextFile(path)
	if !ok {
		// Allow external --scan-context-in already imported into cache by loadLiveScanContext.
		// Re-resolve via load after optional import side effects.
		if h, ok2, why2 := loadLiveScanContext(evDir); !ok2 || h == "" {
			return fmt.Errorf("export scan-context-out: private context: %s; reload: %s", why, why2)
		}
		// loadLiveScanContext may have imported; re-read private path.
		doc, ok, why = loadLiveScanContextFile(path)
		if !ok {
			return fmt.Errorf("export scan-context-out: private context after import: %s", why)
		}
	}
	if doc.ID != scanContextID {
		return fmt.Errorf("export scan-context-out: scan_context_id mismatch")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("export scan-context-out read: %w", err)
	}
	// Re-validate bytes before shipping (symlink/malformed already handled by load).
	if _, ok2, why2 := parseLiveScanContextBytes(b); !ok2 {
		return fmt.Errorf("export scan-context-out reparse: %s", why2)
	}
	evAbs := evDir
	if a, e := filepath.Abs(evDir); e == nil {
		evAbs = a
	}
	if err := writeScanContextOutFile(evAbs, b); err != nil {
		return fmt.Errorf("export scan-context-out: %w", err)
	}
	return nil
}

type liveScanContextDoc struct {
	Hostname string
	ID       string
}

// parseLiveScanContextBytes validates a live_scan_context document body.
func parseLiveScanContextBytes(b []byte) (liveScanContextDoc, bool, string) {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return liveScanContextDoc{}, false, "malformed"
	}
	if schema, _ := m["schema"].(string); strings.TrimSpace(schema) != liveScanContextSchema {
		return liveScanContextDoc{}, false, "schema invalid"
	}
	h, okH := m["live_hostname"].(string)
	h = strings.TrimSpace(h)
	if !okH || h == "" || h == "localhost" || len(h) <= 1 {
		return liveScanContextDoc{}, false, "live_hostname empty"
	}
	id, _ := m["scan_context_id"].(string)
	id = strings.TrimSpace(id)
	if id == "" || len(id) < 16 {
		return liveScanContextDoc{}, false, "scan_context_id missing"
	}
	return liveScanContextDoc{Hostname: h, ID: id}, true, ""
}

// loadLiveScanContext validates private scan context (fail-closed) and binds it
// to live_identity.scan_context_id when identity is present (Pro R19 P1).
func loadLiveScanContext(evDir string) (hostname string, ok bool, reason string) {
	expectedID := ""
	if id := loadLiveIdentity(evDir); id.OK {
		expectedID = id.ScanContextID
		if expectedID == "" {
			return "", false, "live_identity missing scan_context_id"
		}
	}

	tryLoad := func(path string) (liveScanContextDoc, bool, string) {
		return loadLiveScanContextFile(path)
	}

	if expectedID != "" {
		path, pathErr := liveScanContextPath(evDir, expectedID)
		if pathErr != nil {
			return "", false, "private path: " + pathErr.Error()
		}
		doc, ok2, why := tryLoad(path)
		if !ok2 {
			// External import path (outside evidence) for cross-host re-eval (Pro R28 P1).
			if in := strings.TrimSpace(scanContextInPath); in != "" {
				inAbs, aErr := filepath.Abs(in)
				if aErr != nil {
					return "", false, "scan-context-in abs: " + aErr.Error()
				}
				evAbs, _ := filepath.Abs(evDir)
				if under, cErr := pathContainedIn(inAbs, filepath.Clean(evAbs)); cErr == nil && under {
					return "", false, "scan-context-in must be outside evidence directory"
				}
				docIn, okIn, whyIn := tryLoad(inAbs)
				if okIn {
					if docIn.ID != expectedID {
						return "", false, "scan_context_id mismatch (import)"
					}
					// Import into this host private cache.
					if b, rErr := os.ReadFile(inAbs); rErr == nil {
						if mkErr := os.MkdirAll(filepath.Dir(path), 0o700); mkErr == nil {
							_ = os.WriteFile(path, b, 0o600)
						}
					}
					return docIn.Hostname, true, ""
				}
				return "", false, why + "; import:" + whyIn
			}
			// In-evidence control files are publication violations, not load sources.
			return "", false, why
		}
		if doc.ID != expectedID {
			return "", false, "scan_context_id mismatch"
		}
		return doc.Hostname, true, ""
	}

	// No live_identity yet: optional context (privacy does not require it).
	// Cannot resolve private path without ID — only legacy in-evidence file.
	legacy := filepath.Join(evDir, liveScanContextFile)
	doc, ok2, why := tryLoad(legacy)
	if !ok2 {
		return "", false, why
	}
	return doc.Hostname, true, ""
}

func loadLiveScanContextFile(path string) (liveScanContextDoc, bool, string) {
	st, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return liveScanContextDoc{}, false, "missing"
		}
		return liveScanContextDoc{}, false, "unreadable: " + err.Error()
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return liveScanContextDoc{}, false, "symlink"
	}
	if !st.Mode().IsRegular() {
		return liveScanContextDoc{}, false, "not regular file"
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return liveScanContextDoc{}, false, "unreadable: " + err.Error()
	}
	return parseLiveScanContextBytes(b)
}

// privacyScanHostnames returns generator + validated live-executor hostnames.
func privacyScanHostnames(evDir string, ctxHost string, ctxOK bool) []string {
	var out []string
	if h, err := os.Hostname(); err == nil {
		if t := strings.TrimSpace(h); t != "" {
			out = append(out, t)
		}
	}
	if ctxOK && strings.TrimSpace(ctxHost) != "" {
		out = append(out, strings.TrimSpace(ctxHost))
		return out
	}
	// Fail-closed path: do not silently invent live host from a bad file.
	_ = evDir
	return out
}

// scrubLegacyInEvidenceScanContext removes any leftover bare-hostname control files
// from the evidence directory. Private cache path is retained so report re-runs work.
// Both legacy public and accidental portable under-evidence names are removed (Pro R28).
func scrubLegacyInEvidenceScanContext(evDir string) error {
	for _, name := range []string{liveScanContextFile, liveScanContextPortableFile} {
		path := filepath.Join(evDir, name)
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("scrubLegacyInEvidenceScanContext: %s: %w", name, err)
		}
		if _, stErr := os.Lstat(path); stErr == nil {
			return fmt.Errorf("scrubLegacyInEvidenceScanContext: %s still present after remove", name)
		} else if !os.IsNotExist(stErr) {
			return fmt.Errorf("scrubLegacyInEvidenceScanContext: verify %s: %w", name, stErr)
		}
	}
	return nil
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
	scanID, err := newScanContextID()
	if err != nil {
		return fmt.Errorf("writeLiveIdentity: scan_context_id: %w", err)
	}
	if err := writeLiveScanContext(evDir, scanID); err != nil {
		return fmt.Errorf("writeLiveIdentity: scan context: %w", err)
	}
	if err := writeJSON(filepath.Join(evDir, "live_identity.json"), map[string]any{
		"schema":                 liveIdentitySchema,
		"live_binary_commit":     rev,
		"live_binary_dirty":      dirty,
		"live_binary_commit_src": src,
		// Platform of the live campaign host (Pro R10 P2: do not attribute generator GOOS).
		"live_goos":         runtime.GOOS,
		"live_goarch":       runtime.GOARCH,
		"scan_context_id":   scanID,
		"at":                stamp(),
	}); err != nil {
		// Roll back private context so a partial identity cannot leave a stale host token.
		if path, pErr := liveScanContextPath(evDir, scanID); pErr == nil {
			_ = os.Remove(path)
		}
		return err
	}
	return nil
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
	// Lstat: do not treat dangling/following symlinks as "missing" then write through
	// them outside evidence (Pro R37 P2).
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ensureLiveIdentity: live_identity.json is a symlink")
		}
		if st.IsDir() {
			return fmt.Errorf("ensureLiveIdentity: live_identity.json is a directory")
		}
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
		// Sidecar is mandatory with live_identity (Pro R17 P1: no identity-without-context).
		if _, ok, why := loadLiveScanContext(evDir); !ok {
			return fmt.Errorf("ensureLiveIdentity: live_scan_context: %s", why)
		}
		// Resume with existing identity must still honor --scan-context-out
		// (Pro R30 P2: writeLiveScanContext only runs on first create).
		if err := exportLiveScanContextOut(evDir, id.ScanContextID); err != nil {
			return fmt.Errorf("ensureLiveIdentity: %w", err)
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
	// Campaign nonce binding private hostname context (Pro R19 P1).
	scanID, _ := m["scan_context_id"].(string)
	scanID = strings.TrimSpace(scanID)
	if scanID == "" || len(scanID) < 16 {
		return liveIdentity{}, fmt.Errorf("live_identity.json missing scan_context_id")
	}
	return liveIdentity{Commit: commit, Dirty: dirty, Src: src, GOOS: goos, GOARCH: goarch, ScanContextID: scanID, OK: true}, nil
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
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ensureGrokExecutableIdentity: live_grok_executable.json is a symlink")
		}
		if st.IsDir() {
			return fmt.Errorf("ensureGrokExecutableIdentity: live_grok_executable.json is a directory")
		}
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
	// Never publish raw absolute paths in public evidence (Pro R26 P1): basename +
	// path digest are diagnostic only; content SHA-256 is the bind.
	return writeJSON(path, map[string]any{
		"schema":                       liveGrokExeSchema,
		"grok_executable_basename":     filepath.Base(abs),
		"grok_executable_path_sha256":  sha256Hex(abs),
		"grok_executable_sha256":       sum,
		"at":                           stamp(),
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
	// Pro R25 P2: require SHA-256 hex syntax, not merely len==64 (zzzz… must fail).
	if !isSHA256Hex(sum) {
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
	if st, err := os.Lstat(path); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ensureGrokhooksExecutable: live_grokhooks_executable.json is a symlink")
		}
		if st.IsDir() {
			return fmt.Errorf("ensureGrokhooksExecutable: live_grokhooks_executable.json is a directory")
		}
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
	// Never publish raw absolute paths (Pro R26 P1).
	return writeJSON(path, map[string]any{
		"schema":                          liveGrokhooksSchema,
		"grokhooks_executable_basename":   filepath.Base(abs),
		"grokhooks_executable_path_sha256": sha256Hex(abs),
		"grokhooks_executable_sha256":     sum,
		"at":                              stamp(),
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
	// Pro R25 P2: require SHA-256 hex syntax, not merely len==64 (zzzz… must fail).
	if !isSHA256Hex(sum) {
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
