package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func runReport(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	out := fs.String("evidence-out", "", "evidence directory")
	_ = fs.Parse(args)
	evDir := mustAbs(*out, "--evidence-out")
	scenarios := loadScenarioMap(evDir)

	disp, reasons := evaluateDisposition(scenarios)

	// Soft privacy scan of evidence directory (best-effort; not a full audit).
	privacy := scanPrivacy(evDir)

	ver := "unknown"
	verFull := ""
	if b, err := os.ReadFile(filepath.Join(evDir, "preflight.json")); err == nil {
		var pf map[string]any
		if json.Unmarshal(b, &pf) == nil {
			if v, ok := pf["version"].(string); ok && v != "" {
				verFull = strings.TrimSpace(v)
				ver = sanitizeVersion(v)
			}
			if usable, ok := pf["usable"].(bool); ok && !usable && disp != "NO_GO" {
				disp = "MORE_DATA"
				reasons = append(reasons, "preflight usable=false")
			}
		}
	} else if disp == "GO" || disp == "LIMITED_GO" {
		// Missing preflight provenance is not a false GO for live control.
		reasons = append(reasons, "preflight.json missing")
		if disp == "GO" {
			disp = "LIMITED_GO"
		}
	}
	osName := runtime.GOOS
	day := time.Now().UTC().Format("2006-01-02")
	base := fmt.Sprintf("issue-167-live-%s-%s-%s", sanitizeVersion(ver), osName, day)

	ack := "transport"
	if sr, ok := scenarios["ACP-SESSION-001"]; ok && sr.ACKLayer != "" {
		ack = sr.ACKLayer
	}

	// Ensure STATIC-PERM-001 is present for registry completeness.
	if _, ok := scenarios["STATIC-PERM-001"]; !ok {
		scenarios["STATIC-PERM-001"] = ScenarioResult{
			ID:     "STATIC-PERM-001",
			Status: "NOT_RUN",
			Detail: "optional Reinframe-owned static permission fragment not exercised",
			At:     stamp(),
		}
		disp, reasons = evaluateDisposition(scenarios)
	}

	sessCorr := false
	if sr, ok := scenarios["ACP-SESSION-001"]; ok {
		sessCorr = sr.SessionCorrelated && sr.ACKLayer == "session_visible"
	}

	report := map[string]any{
		"schema_version": LiveControlSchemaV2,
		"provenance": map[string]any{
			"issue":                 167,
			"generated_at":          stamp(),
			"goos":                  osName,
			"goarch":                runtime.GOARCH,
			"grok_version":          ver,
			"grok_version_full":     verFull,
			"reinframe_commit":      gitHEAD(),
			"starting_main_sha":     "62889cb59916fa2dd412a2c5d511c7ac7c4b23c6",
			"main_tip_note":         "evidence produced against live host using shipped #165/#166 APIs; v2 gates (#199)",
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
		"process_cleanup":      pick(scenarios, "ACP-CLEANUP-001"),
		"capability_manifests": loadOptionalJSON(filepath.Join(evDir, "acp_manifest.json")),
		"privacy_checks":       privacy,
		"limitations":          reasons,
		"scenarios":            scenarios,
		"scenario_registry":    append([]string{}, goMandatoryIDs...),
		"final_disposition":    disp,
	}
	if verrs := validateReportV2Basics(report, scenarios); len(verrs) > 0 {
		// Always recompute disposition from scenarios on mismatch (#199).
		disp2, reasons2 := evaluateDisposition(scenarios)
		disp = disp2
		reasons = append(reasons2, verrs...)
		report["final_disposition"] = disp
		report["limitations"] = reasons
		if disp == "GO" {
			// Extra belt: never emit GO with validation errors.
			disp = "NO_GO"
			reasons = append(reasons, "validation errors forbid GO")
			report["final_disposition"] = disp
			report["limitations"] = reasons
		}
	}

	// Prefer v2 basename; keep OS/date pin.
	base = fmt.Sprintf("issue-167-live-v2-%s-%s-%s", sanitizeVersion(ver), osName, day)
	jsonPath := filepath.Join(evDir, base+".json")
	mdPath := filepath.Join(evDir, base+".md")
	if err := writeJSON(jsonPath, report); err != nil {
		fail(err)
	}
	md := renderMD(report, disp, ack, ver, osName, reasons, scenarios)
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		fail(err)
	}

	schemaPath := filepath.Join(evDir, "reinframe.grok_build_live_control.v2.schema.json")
	_ = writeJSON(schemaPath, closedSchemaV2())

	printJSON(map[string]any{
		"ok":           true,
		"disposition":  disp,
		"json":         jsonPath,
		"md":           mdPath,
		"reasons":      reasons,
		"mandatory_ok": disp == "GO" || disp == "LIMITED_GO",
	})
	if disp == "NO_GO" {
		os.Exit(1)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func scanPrivacy(evDir string) map[string]any {
	// Best-effort scan. Honesty notes that say "never read auth.json" are not path leaks.
	out := map[string]any{
		"method":         "best_effort_scan",
		"auth_json_read": false,
		"auth_json_path_seen_in_honesty_notes_only": false,
		"auth_json_path_leak_suspected":             false,
		"token_fields_in_auth_envelope":             false,
		"raw_thoughts_stored":                       false,
		"secret_pattern_hits":                       0,
	}
	entries, err := os.ReadDir(evDir)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	hits := 0
	honestyOnly := false
	leak := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(evDir, e.Name()))
		if err != nil || len(b) > 1<<20 {
			continue
		}
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
	}
	out["secret_pattern_hits"] = hits
	out["auth_json_path_seen_in_honesty_notes_only"] = honestyOnly && !leak
	out["auth_json_path_leak_suspected"] = leak
	return out
}

func gitHEAD() string {
	b, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
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
	// Keep only filename-safe characters for evidence basenames.
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '(' || r == ')' || r == '[' || r == ']':
			b.WriteByte('-')
		default:
			// drop
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
	// Stable-ish order: mandatory first then rest via map iteration is fine for MD.
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

// minimalSchema kept for tools that still look for v1 path; v1 is historical only.
func minimalSchema() map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  LiveControlSchemaV1,
		"title":                "Reinframe Grok Build live control evidence (historical v1)",
		"type":                 "object",
		"required":             []string{"schema_version", "final_disposition", "scenarios"},
		"additionalProperties": true,
		"properties": map[string]any{
			"schema_version":    map[string]any{"type": "string"},
			"final_disposition": map[string]any{"enum": []string{"GO", "LIMITED_GO", "MORE_DATA", "NO_GO"}},
			"scenarios":         map[string]any{"type": "object"},
		},
		"description": "Historical only. Use reinframe.grok_build_live_control.v2 for new reports (#199).",
	}
}
