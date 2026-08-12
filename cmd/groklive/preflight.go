package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func runPreflight(args []string) {
	fs := flag.NewFlagSet("preflight", flag.ExitOnError)
	exe := fs.String("grok-executable", "", "optional absolute path to grok")
	out := fs.String("evidence-out", "", "optional evidence directory")
	_ = fs.Parse(args)

	rep := map[string]any{
		"at":      stamp(),
		"probes":  []any{},
		"usable":  false,
		"blocker": nil,
	}
	add := func(cmd string, exit int, stdout, stderr string) {
		probes := rep["probes"].([]any)
		rep["probes"] = append(probes, map[string]any{
			"command": cmd,
			"exit":    exit,
			"stdout":  boundStr(redactSecrets(stdout), 1500),
			"stderr":  boundStr(redactSecrets(stderr), 800),
		})
	}

	// uname / go
	outB, errB, code := runCapture("uname", "-a")
	add("uname -a", code, outB, errB)
	outB, errB, code = runCapture("go", "version")
	add("go version", code, outB, errB)

	grok := strings.TrimSpace(*exe)
	if grok == "" {
		if p, err := exec.LookPath("grok"); err == nil {
			grok = p
		}
	}
	if grok == "" {
		for _, p := range []string{
			filepath.Join(os.Getenv("HOME"), ".local/bin/grok"),
			"/usr/local/bin/grok",
			"/opt/homebrew/bin/grok",
			"/usr/bin/grok",
		} {
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				grok = p
				break
			}
		}
	}
	if grok == "" {
		rep["blocker"] = map[string]any{
			"class":    "binary_absent",
			"command":  "command -v grok",
			"exit":     1,
			"stderr":   "grok not found on PATH or standard locations",
			"binary":   "",
			"version":  "",
			"why_code": "repository code cannot install or authenticate the Grok Build CLI",
		}
		printJSON(rep)
		if *out != "" {
			_ = writeJSON(filepath.Join(*out, "preflight.json"), rep)
		}
		os.Exit(1)
	}
	abs, _ := filepath.Abs(grok)
	base := filepath.Base(abs)
	// #168 / GPT P1-C: never publish raw absolute paths or symlink targets.
	rep["binary"] = base
	rep["binary_path_sha256"] = sha256Hex(abs)

	// Content-bind live harness + Grok CLI BEFORE probes so version/help/initialize
	// evidence cannot come from a different binary than the recorded hash (Codex P2).
	// After probes, re-verify the same binding (create-or-verify is idempotent).
	var evDir string
	if *out != "" {
		evDir = mustAbs(*out, "--evidence-out")
		if err := ensureLiveIdentity(evDir); err != nil {
			fail(fmt.Errorf("groklive preflight: live_identity: %w", err))
		}
		if err := ensureGrokExecutableIdentity(evDir, abs); err != nil {
			fail(fmt.Errorf("groklive preflight: live_grok_executable: %w", err))
		}
	}

	outB, errB, code = runCapture("ls", "-l", abs)
	// Never retain local account owner/group from ls -l (Codex P1).
	add("ls -l "+base, code, redactLsOwnership(outB), errB)
	outB, errB, code = runCapture(abs, "--version")
	add(base+" --version", code, outB, errB)
	rep["version"] = strings.TrimSpace(outB)
	outB, errB, code = runCapture(abs, "--no-auto-update", "--help")
	add(base+" --no-auto-update --help", code, outB, errB)
	outB, errB, code = runCapture(abs, "--no-auto-update", "agent", "stdio", "--help")
	add(base+" --no-auto-update agent stdio --help", code, outB, errB)

	// Minimal ACP initialize — proves process can start (auth tested in acp subcommand).
	initOK, initDetail := probeInitialize(abs)
	rep["acp_initialize_probe"] = map[string]any{"ok": initOK, "detail": boundStr(initDetail, 400)}
	if initOK {
		rep["usable"] = true
	} else {
		rep["blocker"] = map[string]any{
			"class":    "other_external_environment",
			"command":  base + " --no-auto-update agent stdio",
			"exit":     1,
			"stderr":   boundStr(initDetail, 400),
			"binary":   base,
			"version":  rep["version"],
			"why_code": "ACP initialize failed against live Grok process",
		}
	}
	printJSON(rep)
	if evDir != "" {
		// Re-verify executable content still matches pre-probe binding.
		if err := ensureGrokExecutableIdentity(evDir, abs); err != nil {
			fail(fmt.Errorf("groklive preflight: live_grok_executable post-probe: %w", err))
		}
		if err := writeJSON(filepath.Join(evDir, "preflight.json"), rep); err != nil {
			fail(err)
		}
	}
	if !initOK {
		os.Exit(1)
	}
}

func probeInitialize(grok string) (bool, string) {
	ctx, cancel := ctxTimeout(20)
	defer cancel()
	cmd := exec.CommandContext(ctx, grok, "--no-auto-update", "agent", "stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false, err.Error()
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return false, err.Error()
	}
	req := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":1,"clientInfo":{"name":"groklive-preflight","version":"0"}}}` + "\n"
	_, _ = stdin.Write([]byte(req))
	_ = stdin.Close()
	// Wait briefly for response then kill
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-time.After(8 * time.Second):
		_ = cmd.Process.Kill()
		<-done
	case <-done:
	}
	out := stdout.String()
	if strings.Contains(out, `"protocolVersion":1`) || strings.Contains(out, `"protocolVersion": 1`) {
		return true, "initialize returned protocolVersion 1"
	}
	return false, "no protocolVersion 1 in response: " + boundStr(redactSecrets(out+stderr.String()), 300)
}

func runCapture(name string, args ...string) (stdout, stderr string, code int) {
	ctx, cancel := ctxTimeout(30)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var outB, errB bytes.Buffer
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	return outB.String(), errB.String(), code
}

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func boundStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func redactSecrets(s string) string {
	// Never retain credential-looking material or local host identity in harness output (#168).
	repl := []struct{ old, new string }{
		{"sk-", "[REDACTED]"},
		{"xai-", "[REDACTED]"},
		{"Bearer ", "[REDACTED] "},
		{"API_TOKEN", "API_TOKEN_REDACTED"},
	}
	out := s
	for _, r := range repl {
		out = strings.ReplaceAll(out, r.old, r.new)
	}
	return redactLocalIdentity(out)
}

// redactLocalIdentity removes home/temp paths and hostnames from evidence strings (#168 / GPT P1-C).
// Handles Unix paths, Windows paths (including JSON-escaped backslashes), and common env roots.
func redactLocalIdentity(s string) string {
	if s == "" {
		return s
	}
	// Env-bound roots (HOME/USERPROFILE and TEMP/TMP/TMPDIR), including JSON-escaped forms.
	for _, key := range []string{"HOME", "USERPROFILE"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			s = replacePathVariants(s, v, "[HOME]")
		}
	}
	for _, key := range []string{"TEMP", "TMP", "TMPDIR"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" && len(v) > 2 {
			s = replacePathVariants(s, v, "[TMP]")
		}
	}
	// Generic absolute home paths (Unix + Windows).
	s = localUsersPath.ReplaceAllString(s, "[HOME]")
	s = localUnixHomePath.ReplaceAllString(s, "[HOME]")
	s = winUsersPath.ReplaceAllString(s, "[HOME]")
	s = winUsersPathEscaped.ReplaceAllString(s, "[HOME]")
	s = winUsersPathSlash.ReplaceAllString(s, "[HOME]")
	// Temp directories — replace with short hash of original path (no raw path retained).
	s = localVarFoldersPath.ReplaceAllStringFunc(s, func(p string) string {
		return "[TMP:" + sha256Hex(p)[:16] + "]"
	})
	s = localTmpPath.ReplaceAllStringFunc(s, func(p string) string {
		return "[TMP:" + sha256Hex(p)[:16] + "]"
	})
	s = winTempPath.ReplaceAllStringFunc(s, func(p string) string {
		return "[TMP:" + sha256Hex(p)[:16] + "]"
	})
	s = winTempPathEscaped.ReplaceAllStringFunc(s, func(p string) string {
		return "[TMP:" + sha256Hex(p)[:16] + "]"
	})
	// Local hostnames: .local mDNS and the process runtime hostname (uname -a /
	// Linux runners often lack a .local suffix — GPT-5.6 Pro P1).
	s = localHostname.ReplaceAllString(s, "[HOSTNAME]")
	if h, err := os.Hostname(); err == nil {
		h = strings.TrimSpace(h)
		// Boundary-aware only — unrestricted ReplaceAll rewrites schema tokens when
		// hostname is a short common word (e.g. "build" in grok_build, "go" in goos)
		// (Pro R10 P2).
		if h != "" && h != "localhost" && len(h) > 1 {
			s = redactHostnameToken(s, h)
		}
	}
	// Local account names in path/ownership contexts only — never unrestricted
	// substring replace (Codex P2: USER=go must not rewrite "goos").
	s = redactLocalAccountNames(s)
	s = redactLsOwnership(s)
	return s
}

// hostnameTokenRE matches a host string as a whole DNS label so short hostnames
// cannot corrupt schema ids (e.g. hostname "build" inside grok_build), while still
// matching FQDNs like build-01.corp.example.com when host is build-01 (Pro R21 P1).
// Case-insensitive: Windows/DNS hostnames often vary in case in tool output (Pro R14 P1).
func hostnameTokenRE(host string) *regexp.Regexp {
	// Left boundary excludes letter/digit/underscore/hyphen (not bare mid-identifier).
	// Middle: optional FQDN suffix labels. Right: end or non-DNS char (RE2 has no lookaround).
	return regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(host) +
		`((?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)*)([^A-Za-z0-9_.-]|$)`)
}

// hostnameTokenPresent reports whether host appears as a token (not a substring of
// a larger identifier). Used by both redaction and privacy scan (Codex P2).
func hostnameTokenPresent(s, host string) bool {
	if s == "" || host == "" {
		return false
	}
	return hostnameTokenRE(host).MatchString(s)
}

// redactHostnameToken replaces a runtime hostname only as a whole token so short
// hostnames cannot mutate JSON keys or schema ids (Pro R10 P2). FQDN forms of the
// short name are replaced with [HOSTNAME] (Pro R21 P1).
func redactHostnameToken(s, host string) string {
	if s == "" || host == "" {
		return s
	}
	// ${1}=left boundary, ${3}=right boundary (preserve), FQDN body dropped into placeholder.
	return hostnameTokenRE(host).ReplaceAllString(s, `${1}[HOSTNAME]${3}`)
}

// redactLocalAccountNames rewrites env account names only as path segments.
// Ownership columns are handled exclusively by redactLsOwnership — never apply
// whitespace-bound whole-document replace of USER (Pro R12 P2: USER=grok would
// corrupt version strings like "grok 1.0.0").
func redactLocalAccountNames(s string) string {
	for _, key := range []string{"USER", "LOGNAME", "USERNAME"} {
		u := strings.TrimSpace(os.Getenv(key))
		if u == "" || u == "root" || len(u) < 2 {
			continue
		}
		// Path segment only: /Users/name, /home/name, \Users\name
		s = strings.ReplaceAll(s, "/Users/"+u, "/Users/[USER]")
		s = strings.ReplaceAll(s, "/home/"+u, "/home/[USER]")
		s = strings.ReplaceAll(s, `\Users\`+u, `\Users\[USER]`)
		s = strings.ReplaceAll(s, `\\Users\\`+u, `\\Users\\[USER]`)
	}
	return s
}

// redactLsOwnership rewrites Unix-style ls -l owner and group columns to placeholders.
// Matches at line start or mid-string (JSON "stdout": "lrwx… 1 user group …").
func redactLsOwnership(s string) string {
	if s == "" {
		return s
	}
	// prefix + mode links + owner + space + group … (mid-line after quote/space)
	return lsOwnerGroup.ReplaceAllString(s, `${1}${2}[USER]${4}[GROUP]`)
}

// replacePathVariants replaces an absolute path in raw, slash-normalized, and JSON-escaped forms.
func replacePathVariants(s, path, repl string) string {
	if path == "" {
		return s
	}
	s = strings.ReplaceAll(s, path, repl)
	// Forward-slash form (common after filepath.ToSlash / mixed evidence).
	if slash := filepath.ToSlash(path); slash != path {
		s = strings.ReplaceAll(s, slash, repl)
	}
	// JSON Marshal escapes Windows backslashes as \\.
	if strings.Contains(path, `\`) {
		esc := strings.ReplaceAll(path, `\`, `\\`)
		s = strings.ReplaceAll(s, esc, repl)
	}
	return s
}

var (
	localUsersPath      = regexp.MustCompile(`(?i)/Users/[^/\s"'\\]+`)
	localUnixHomePath   = regexp.MustCompile(`(?i)/home/[^/\s"'\\]+`)
	localVarFoldersPath = regexp.MustCompile(`/var/folders/[^\s"'\\]+`)
	localTmpPath        = regexp.MustCompile(`(?i)(?:/private)?/(?:tmp|var/tmp)/[^\s"'\\]+`)
	localHostname       = regexp.MustCompile(`(?i)\b[a-z0-9][a-z0-9._-]*\.local\b`)
	// Placeholders produced by redactLocalIdentity for temp paths.
	localTmpPlaceholder = regexp.MustCompile(`\[TMP:[0-9a-fA-F]+\]`)
	// ls -l owner/group columns — line start OR after whitespace/quote (JSON-escaped stdout).
	// Groups: 1=prefix, 2=mode+links, 3=owner, 4=space, 5=group
	lsOwnerGroup = regexp.MustCompile(`(?m)(^|[\s"\\])([bcdlps\-][\w@.+-]{9,14}\s+\d+\s+)(\S+)(\s+)(\S+)`)
	// Windows user profile trees (raw, JSON-escaped, and slash forms). Match through
	// the rest of the path so AppData\Local\Temp under the profile is removed too.
	winUsersPath        = regexp.MustCompile(`(?i)[A-Za-z]:\\Users\\[^\s"']+`)
	winUsersPathEscaped = regexp.MustCompile(`(?i)[A-Za-z]:\\\\Users\\\\[^\s"']+`)
	winUsersPathSlash   = regexp.MustCompile(`(?i)[A-Za-z]:/Users/[^\s"']+`)
	// System Windows Temp (not under \Users\…).
	winTempPath        = regexp.MustCompile(`(?i)[A-Za-z]:\\Windows\\Temp\\[^\s"']*`)
	winTempPathEscaped = regexp.MustCompile(`(?i)[A-Za-z]:\\\\Windows\\\\Temp\\\\[^\s"']*`)
)
