package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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
	if b, err := os.ReadFile(filepath.Join(evDir, "preflight.json")); err == nil {
		var pf map[string]any
		if json.Unmarshal(b, &pf) == nil {
			if v, ok := pf["version"].(string); ok && v != "" {
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

	report := map[string]any{
		"schema_version": "reinframe.grok_build_live_control.v1",
		"provenance": map[string]any{
			"issue":         167,
			"generated_at":  stamp(),
			"goos":          osName,
			"goarch":        runtime.GOARCH,
			"grok_version":  ver,
			"main_tip_note": "evidence produced against live host using shipped #165/#166 APIs",
			"harness":       "cmd/groklive",
		},
		"entry_gates": map[string]any{
			"live_flag_required": true,
			"auth_json_read":     false,
			"credential_print":   false,
		},
		"trust_results":             pick(scenarios, "TRUST-001", "TRUST-STALE-001", "TRUST-RESTORE-001"),
		"hook_results":              pick(scenarios, "HOOK-ALLOW-001", "HOOK-DENY-001", "HOOK-MAP-001", "HOOK-UNINSTALL-001"),
		"hook_failure_semantics":    pick(scenarios, "HOOK-FAIL-001", "HOOK-FAIL-002", "HOOK-FAIL-003", "HOOK-FAIL-004"),
		"static_permission_results": map[string]any{"status": "NOT_RUN", "detail": "optional fragment not required for GO"},
		"acp_negotiation":           pick(scenarios, "ACP-INIT-001"),
		"auth_boundary":             pick(scenarios, "ACP-AUTH-001"),
		"session_results":           pick(scenarios, "ACP-SESSION-001", "ACP-OPTIONAL-001"),
		"advice_results":            pick(scenarios, "ADVICE-DEDUP-001"),
		"challenge_results":         pick(scenarios, "CHALLENGE-001"),
		"ack_layers": map[string]any{
			"strongest_proven": ack,
			"explicit_claimed": false,
			"note":             "JSON-RPC success is transport; session/update is session_visible; explicit never from transport alone",
		},
		"process_cleanup":      pick(scenarios, "ACP-CLEANUP-001"),
		"capability_manifests": loadOptionalJSON(filepath.Join(evDir, "acp_manifest.json")),
		"privacy_checks":       privacy,
		"limitations":          reasons,
		"scenarios":            scenarios,
		"final_disposition":    disp,
	}

	jsonPath := filepath.Join(evDir, base+".json")
	mdPath := filepath.Join(evDir, base+".md")
	_ = writeJSON(jsonPath, report)
	md := renderMD(report, disp, ack, ver, osName, reasons, scenarios)
	_ = os.WriteFile(mdPath, []byte(md), 0o600)

	schemaPath := filepath.Join(evDir, "reinframe.grok_build_live_control.v1.schema.json")
	if _, err := os.Stat(schemaPath); err != nil {
		_ = writeJSON(schemaPath, minimalSchema())
	}

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

// evaluateDisposition ranks GO / LIMITED_GO / MORE_DATA / NO_GO from scenario map.
// Mandatory scenarios must PASS for GO. INCONCLUSIVE on any mandatory → LIMITED_GO.
// Missing/FAIL mandatory → NO_GO. ACP-AUTH-001 must PASS (hard auth boundary).
func evaluateDisposition(scenarios map[string]ScenarioResult) (disp string, reasons []string) {
	mandatory := []string{
		"HOOK-ALLOW-001", "HOOK-DENY-001",
		"ACP-INIT-001", "ACP-AUTH-001", "ACP-SESSION-001", "ACP-CLEANUP-001",
	}
	disp = "GO"
	reasons = []string{}
	if len(scenarios) == 0 {
		return "MORE_DATA", []string{"no scenarios recorded"}
	}
	for _, id := range mandatory {
		sr, ok := scenarios[id]
		if !ok || sr.Status == "NOT_RUN" || sr.Status == "" {
			disp = "NO_GO"
			reasons = append(reasons, id+" missing")
			continue
		}
		if sr.Status == "FAIL" {
			disp = "NO_GO"
			reasons = append(reasons, id+" FAIL")
		}
		if sr.Status == "INCONCLUSIVE" && disp == "GO" {
			disp = "LIMITED_GO"
			reasons = append(reasons, id+" INCONCLUSIVE")
		}
		// LIMITED_GO already set stays unless NO_GO.
		if sr.Status == "INCONCLUSIVE" && disp == "LIMITED_GO" {
			// already recorded
			if !containsStr(reasons, id+" INCONCLUSIVE") {
				reasons = append(reasons, id+" INCONCLUSIVE")
			}
		}
	}
	if sr, ok := scenarios["ACP-AUTH-001"]; !ok || sr.Status != "PASS" {
		disp = "NO_GO"
		if !containsStr(reasons, "ACP-AUTH-001 not PASS") {
			reasons = append(reasons, "ACP-AUTH-001 not PASS")
		}
	}
	return disp, reasons
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
	// Best-effort scan of small evidence files for secret-looking material / auth.json paths.
	out := map[string]any{
		"method":                        "best_effort_scan",
		"auth_json_path_seen":           false,
		"token_fields_in_auth_envelope": false,
		"raw_thoughts_stored":           false,
		"secret_pattern_hits":           0,
	}
	entries, err := os.ReadDir(evDir)
	if err != nil {
		out["error"] = err.Error()
		return out
	}
	hits := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Cap file size for scan.
		b, err := os.ReadFile(filepath.Join(evDir, name))
		if err != nil || len(b) > 1<<20 {
			continue
		}
		s := string(b)
		if strings.Contains(s, "auth.json") || strings.Contains(s, ".grok/auth") {
			out["auth_json_path_seen"] = true
		}
		if strings.Contains(s, `"token"`) && strings.Contains(s, "authenticate") {
			out["token_fields_in_auth_envelope"] = true
		}
		for _, pat := range []string{"sk-", "xai-", "Bearer ", "eyJ"} {
			if strings.Contains(s, pat) {
				// Ignore our own redaction markers.
				if strings.Contains(s, "[REDACTED]") && pat != "eyJ" {
					continue
				}
				hits++
			}
		}
	}
	out["secret_pattern_hits"] = hits
	return out
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
	b.WriteString(fmt.Sprintf("- **Disposition:** `%s`\n", disp))
	b.WriteString(fmt.Sprintf("- **Grok version:** %s\n", ver))
	b.WriteString(fmt.Sprintf("- **OS:** %s\n", osName))
	b.WriteString(fmt.Sprintf("- **Strongest ACK proven:** `%s`\n", ack))
	b.WriteString("- **Auth.json read:** no\n")
	b.WriteString("- **Explicit ACK claimed:** no\n\n")
	b.WriteString("## Scenarios\n\n| ID | Status | Detail |\n|----|--------|--------|\n")
	// Stable-ish order: mandatory first then rest via map iteration is fine for MD.
	for id, sr := range scenarios {
		b.WriteString(fmt.Sprintf("| %s | %s | %s |\n", id, sr.Status, strings.ReplaceAll(boundStr(sr.Detail, 80), "|", "/")))
	}
	if len(reasons) > 0 {
		b.WriteString("\n## Limitations\n\n")
		for _, r := range reasons {
			b.WriteString("- " + r + "\n")
		}
	}
	b.WriteString("\n## Non-claims\n\n")
	b.WriteString("- No Level 2 / CapPause from hooks alone\n")
	b.WriteString("- No cross-host ranking\n")
	b.WriteString("- No credential material intentionally stored\n")
	_ = report
	return b.String()
}

func minimalSchema() map[string]any {
	return map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"$id":                  "reinframe.grok_build_live_control.v1",
		"title":                "Reinframe Grok Build live control evidence",
		"type":                 "object",
		"required":             []string{"schema_version", "final_disposition", "scenarios"},
		"additionalProperties": true,
		"properties": map[string]any{
			"schema_version":    map[string]any{"type": "string"},
			"final_disposition": map[string]any{"enum": []string{"GO", "LIMITED_GO", "MORE_DATA", "NO_GO"}},
			"scenarios":         map[string]any{"type": "object"},
		},
	}
}
