package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestClaudeBridge_PretoolAllow(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	inputJSON := `{"session_id":"sess-1","tool_name":"ReadFile","file_path":"pkg/main.go"}`
	inR, inW, _ := os.Pipe()
	go func() {
		_, _ = inW.Write([]byte(inputJSON))
		_ = inW.Close()
	}()
	os.Stdin = inR

	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	exitCode := runPretool([]string{})
	_ = outW.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	outStr := buf.String()
	if !strings.Contains(outStr, `"decision":"approve"`) {
		t.Fatalf("expected approve decision, got: %s", outStr)
	}
}

func TestClaudeBridge_PretoolDeny(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	inputJSON := `{"session_id":"sess-1","tool_name":"Bash","file_path":""}`
	inR, inW, _ := os.Pipe()
	go func() {
		_, _ = inW.Write([]byte(inputJSON))
		_ = inW.Close()
	}()
	os.Stdin = inR

	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	exitCode := runPretool([]string{"-deny-tool", "Bash"})
	_ = outW.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	outStr := buf.String()
	if !strings.Contains(outStr, `"decision":"block"`) {
		t.Fatalf("expected block decision, got: %s", outStr)
	}
}

func TestClaudeBridge_Prompt(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	inputJSON := `{"session_id":"sess-prompt-1","prompt":"Please audit the project security"}`
	inR, inW, _ := os.Pipe()
	go func() {
		_, _ = inW.Write([]byte(inputJSON))
		_ = inW.Close()
	}()
	os.Stdin = inR

	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	exitCode := runPrompt([]string{})
	_ = outW.Close()

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, outR)

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}

	outStr := buf.String()
	if !strings.Contains(outStr, `"task_id"`) {
		t.Fatalf("expected task_id in output, got: %s", outStr)
	}
}
