package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

func TestClaudeBridge_AppealAndRetryWorkflow(t *testing.T) {
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	defer func() {
		os.Stdin = oldStdin
		os.Stdout = oldStdout
	}()

	tmpStore := filepath.Join(t.TempDir(), "test_challenge_store.json")

	// Step 1: Initial invocation denied with appealable challenge
	input1 := `{"session_id":"sess-e2e","tool_name":"Bash","file_path":"deploy.sh","tool_use_id":"inv-1"}`
	inR1, inW1, _ := os.Pipe()
	go func() {
		_, _ = inW1.Write([]byte(input1))
		_ = inW1.Close()
	}()
	os.Stdin = inR1

	outR1, outW1, _ := os.Pipe()
	os.Stdout = outW1

	exitCode1 := runPretool([]string{"-deny-tool", "Bash", "-store-path", tmpStore})
	_ = outW1.Close()

	var buf1 bytes.Buffer
	_, _ = io.Copy(&buf1, outR1)

	if exitCode1 != 0 {
		t.Fatalf("step 1 expected exit code 0, got %d", exitCode1)
	}

	var resp1 map[string]any
	if err := json.Unmarshal(buf1.Bytes(), &resp1); err != nil {
		t.Fatalf("failed to unmarshal step 1 response: %v", err)
	}
	if resp1["decision"] != "block" {
		t.Fatalf("step 1 expected block, got %v", resp1["decision"])
	}

	reinframeMeta, _ := resp1["reinframe"].(map[string]any)
	if reinframeMeta == nil {
		t.Fatalf("step 1 expected reinframe metadata, got nil")
	}
	chCtx, _ := reinframeMeta["challenge"].(map[string]any)
	if chCtx == nil {
		t.Fatalf("step 1 expected challenge context, got nil")
	}
	challengeID, _ := chCtx["challenge_id"].(string)
	challengeNonce, _ := chCtx["challenge_nonce"].(string)
	if challengeID == "" || challengeNonce == "" {
		t.Fatalf("expected non-empty challenge ID and nonce, got id=%q nonce=%q", challengeID, challengeNonce)
	}

	// Step 2: Appeal the challenge via appeal subcommand
	outR2, outW2, _ := os.Pipe()
	os.Stdout = outW2

	exitCode2 := runAppeal([]string{
		"-challenge-id", challengeID,
		"-nonce", challengeNonce,
		"-value", "Run production safe diagnostics",
		"-prevented", "Prevents production failure during diagnostics",
		"-cost", "0 USD negligible compute cost",
		"-store-path", tmpStore,
	})
	_ = outW2.Close()

	var buf2 bytes.Buffer
	_, _ = io.Copy(&buf2, outR2)

	if exitCode2 != 0 {
		t.Fatalf("step 2 appeal expected exit code 0, got %d. Out: %s", exitCode2, buf2.String())
	}

	var appealResp map[string]any
	if err := json.Unmarshal(buf2.Bytes(), &appealResp); err != nil {
		t.Fatalf("failed to unmarshal appeal response: %v", err)
	}
	if !strings.EqualFold(fmt.Sprint(appealResp["state"]), "justified") {
		t.Fatalf("expected state justified, got %v", appealResp["state"])
	}

	// Step 3: Retry turn with challenge binding and matching tool_use_id -> Allowed once
	input3 := fmt.Sprintf(`{"session_id":"sess-e2e","tool_name":"Bash","file_path":"deploy.sh","challenge_id":"%s","challenge_nonce":"%s","tool_use_id":"inv-1"}`, challengeID, challengeNonce)
	inR3, inW3, _ := os.Pipe()
	go func() {
		_, _ = inW3.Write([]byte(input3))
		_ = inW3.Close()
	}()
	os.Stdin = inR3

	outR3, outW3, _ := os.Pipe()
	os.Stdout = outW3

	exitCode3 := runPretool([]string{"-deny-tool", "Bash", "-store-path", tmpStore})
	_ = outW3.Close()

	var buf3 bytes.Buffer
	_, _ = io.Copy(&buf3, outR3)

	if exitCode3 != 0 {
		t.Fatalf("step 3 retry expected exit code 0, got %d", exitCode3)
	}

	var resp3 map[string]any
	if err := json.Unmarshal(buf3.Bytes(), &resp3); err != nil {
		t.Fatalf("failed to unmarshal step 3 response: %v", err)
	}
	if resp3["decision"] != "approve" {
		t.Fatalf("step 3 expected approve on justified retry, got %v. Raw: %s", resp3["decision"], buf3.String())
	}

	// Step 4: Subsequent tool invocation with different tool_use_id -> Denied (budget consumed)
	input4 := fmt.Sprintf(`{"session_id":"sess-e2e","tool_name":"Bash","file_path":"deploy.sh","challenge_id":"%s","challenge_nonce":"%s","tool_use_id":"inv-2"}`, challengeID, challengeNonce)
	inR4, inW4, _ := os.Pipe()
	go func() {
		_, _ = inW4.Write([]byte(input4))
		_ = inW4.Close()
	}()
	os.Stdin = inR4

	outR4, outW4, _ := os.Pipe()
	os.Stdout = outW4

	exitCode4 := runPretool([]string{"-deny-tool", "Bash", "-store-path", tmpStore})
	_ = outW4.Close()

	var buf4 bytes.Buffer
	_, _ = io.Copy(&buf4, outR4)

	if exitCode4 != 0 {
		t.Fatalf("step 4 retry expected exit code 0, got %d", exitCode4)
	}

	var resp4 map[string]any
	if err := json.Unmarshal(buf4.Bytes(), &resp4); err != nil {
		t.Fatalf("failed to unmarshal step 4 response: %v", err)
	}
	if resp4["decision"] != "block" {
		t.Fatalf("step 4 expected block after retry budget consumed, got %v", resp4["decision"])
	}
}
