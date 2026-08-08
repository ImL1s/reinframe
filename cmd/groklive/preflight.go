package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
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
	rep["binary"] = abs
	outB, errB, code = runCapture("ls", "-l", abs)
	add("ls -l "+abs, code, outB, errB)
	outB, errB, code = runCapture(abs, "--version")
	add(abs+" --version", code, outB, errB)
	rep["version"] = strings.TrimSpace(outB)
	outB, errB, code = runCapture(abs, "--no-auto-update", "--help")
	add(abs+" --no-auto-update --help", code, outB, errB)
	outB, errB, code = runCapture(abs, "--no-auto-update", "agent", "stdio", "--help")
	add(abs+" --no-auto-update agent stdio --help", code, outB, errB)

	// Minimal ACP initialize — proves process can start (auth tested in acp subcommand).
	initOK, initDetail := probeInitialize(abs)
	rep["acp_initialize_probe"] = map[string]any{"ok": initOK, "detail": boundStr(initDetail, 400)}
	if initOK {
		rep["usable"] = true
	} else {
		rep["blocker"] = map[string]any{
			"class":    "other_external_environment",
			"command":  abs + " --no-auto-update agent stdio",
			"exit":     1,
			"stderr":   boundStr(initDetail, 400),
			"binary":   abs,
			"version":  rep["version"],
			"why_code": "ACP initialize failed against live Grok process",
		}
	}
	printJSON(rep)
	if *out != "" {
		_ = writeJSON(filepath.Join(*out, "preflight.json"), rep)
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
	// Never retain credential-looking material in harness output.
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
	return out
}
