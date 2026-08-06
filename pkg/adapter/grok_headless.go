package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Headless observe-only profile (#166 / #191). Separate from ACP control.
// Official: grok --no-auto-update -p <PROMPT> --output-format streaming-json
// https://docs.x.ai/build/cli/headless-scripting (retrieved 2026-08-06)
const (
	GrokHeadlessObserveProfileV1 = "reinframe.grok_build_headless_observe.v1"
	MaxGrokHeadlessLineBytes     = 1 << 20
	DefaultGrokHeadlessTimeout   = 60 * time.Second
	grokHeadlessGracefulWait     = 2 * time.Second
	grokHeadlessForceWait        = 3 * time.Second
)

// GrokHeadlessObserveConfig launches a one-shot/observe headless stream.
type GrokHeadlessObserveConfig struct {
	Executable string
	// Args default to DefaultGrokHeadlessArgs(prompt) when empty.
	// Custom Args are a closed profile: must include official markers (no shell metachar scan as primary validation).
	Args    []string
	Prompt  string
	WorkDir string
	Timeout time.Duration
	Env     []string // never injects auth.json contents
}

// GrokHeadlessObserveEvent is one NDJSON line from streaming-json (bounded summary).
type GrokHeadlessObserveEvent struct {
	Type    string `json:"type,omitempty"`
	Summary string `json:"summary,omitempty"`
	RawOK   bool   `json:"raw_ok"`
}

// GrokHeadlessObserveManifest is observe-only (no CapToolGate/CapAdviceDelivery from this path).
type GrokHeadlessObserveManifest struct {
	Profile           string `json:"profile"`
	CapEventStream    bool   `json:"cap_event_stream"`
	CapToolGate       bool   `json:"cap_tool_gate"`
	CapAdviceDelivery bool   `json:"cap_advice_delivery"`
	CapPause          bool   `json:"cap_pause"`
	ExplicitAck       bool   `json:"explicit_ack"`
	HonestyNote       string `json:"honesty_note"`
}

// NewGrokHeadlessObserveManifest returns the observe-only capability claim.
func NewGrokHeadlessObserveManifest() GrokHeadlessObserveManifest {
	return GrokHeadlessObserveManifest{
		Profile:           GrokHeadlessObserveProfileV1,
		CapEventStream:    true,
		CapToolGate:       false,
		CapAdviceDelivery: false,
		CapPause:          false,
		ExplicitAck:       false,
		HonestyNote: "headless streaming-json is observe-only; tool approvals/advice require ACP; " +
			"never read/write ~/.grok/auth.json; live proof #167 needs #191 + auth env",
	}
}

// DefaultGrokHeadlessArgs builds the official argv for streaming-json observe (no shell).
// Shape: --no-auto-update -p <PROMPT> --output-format streaming-json
func DefaultGrokHeadlessArgs(prompt string) []string {
	return []string{
		"--no-auto-update",
		"-p", prompt,
		"--output-format", "streaming-json",
	}
}

// ValidateGrokHeadlessArgs checks a closed headless observe profile (no shell interpolation).
func ValidateGrokHeadlessArgs(args []string) error {
	if len(args) < 5 {
		return fmt.Errorf("grok headless: args too short for official profile")
	}
	hasNoAuto := false
	hasP := false
	hasFmt := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--no-auto-update":
			hasNoAuto = true
		case "-p", "--single":
			hasP = true
			if i+1 >= len(args) {
				return fmt.Errorf("grok headless: -p/--single requires prompt argument")
			}
			i++ // skip prompt value
		case "--output-format":
			if i+1 >= len(args) || args[i+1] != "streaming-json" {
				return fmt.Errorf("grok headless: --output-format must be streaming-json")
			}
			hasFmt = true
			i++
		}
	}
	if !hasNoAuto || !hasP || !hasFmt {
		return fmt.Errorf("grok headless: args must include --no-auto-update, -p <prompt>, --output-format streaming-json")
	}
	return nil
}

// RunGrokHeadlessObserve runs a headless streaming-json process and collects bounded events.
// Observe-only: does not send tool approvals or treat exit as explicit ACK.
func RunGrokHeadlessObserve(ctx context.Context, cfg GrokHeadlessObserveConfig) ([]GrokHeadlessObserveEvent, error) {
	if cfg.Executable == "" {
		return nil, fmt.Errorf("grok headless: Executable required")
	}
	if strings.ContainsAny(cfg.Executable, " \t\n;$|&") {
		return nil, fmt.Errorf("grok headless: executable path must not contain shell metacharacters")
	}
	prompt := boundRunes(cfg.Prompt, MaxGrokContextRunes)
	if prompt == "" {
		return nil, fmt.Errorf("grok headless: prompt required")
	}
	args := cfg.Args
	if len(args) == 0 {
		args = DefaultGrokHeadlessArgs(prompt)
	}
	if err := ValidateGrokHeadlessArgs(args); err != nil {
		return nil, err
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultGrokHeadlessTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, cfg.Executable, args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	configureGrokProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("grok headless: start: %w", err)
	}
	plat, err := attachGrokProcess(cmd)
	if err != nil {
		_ = signalGrokProcess(cmd, &plat, true)
		_, _ = cmd.Process.Wait()
		releaseGrokProcess(&plat)
		return nil, fmt.Errorf("grok headless: attach process tree: %w", err)
	}
	events, readErr := parseHeadlessStream(stdout)
	// Graceful → bounded force reaping of the owned tree.
	_ = signalGrokProcess(cmd, &plat, false)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-time.After(grokHeadlessGracefulWait):
		_ = signalGrokProcess(cmd, &plat, true)
		select {
		case waitErr = <-done:
		case <-time.After(grokHeadlessForceWait):
			waitErr = fmt.Errorf("grok headless: force wait timeout")
		}
	}
	releaseGrokProcess(&plat)
	if readErr != nil {
		return events, readErr
	}
	if waitErr != nil && runCtx.Err() == nil {
		// Non-zero exit is observation failure, not tool gate.
		return events, fmt.Errorf("grok headless: process exit: %w", waitErr)
	}
	return events, nil
}

// ParseGrokHeadlessStream parses an already-open streaming-json reader (tests).
func ParseGrokHeadlessStream(r io.Reader) ([]GrokHeadlessObserveEvent, error) {
	return parseHeadlessStream(r)
}

func parseHeadlessStream(r io.Reader) ([]GrokHeadlessObserveEvent, error) {
	var out []GrokHeadlessObserveEvent
	sc := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, MaxGrokHeadlessLineBytes+1)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || len(line) > MaxGrokHeadlessLineBytes {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			out = append(out, GrokHeadlessObserveEvent{Type: "malformed", RawOK: false, Summary: "invalid_json"})
			continue
		}
		typ, _ := m["type"].(string)
		if typ == "" {
			typ, _ = m["sessionUpdate"].(string)
		}
		// Never retain thought/reasoning bodies.
		if typ == "agent_thought_chunk" || typ == "thinking" {
			out = append(out, GrokHeadlessObserveEvent{Type: typ, RawOK: true, Summary: "thought_omitted"})
			continue
		}
		sum := typ
		if tn, ok := m["toolName"].(string); ok {
			sum = "tool=" + tn
		} else if s, ok := m["status"].(string); ok {
			sum = "status=" + s
		}
		out = append(out, GrokHeadlessObserveEvent{Type: typ, RawOK: true, Summary: boundRunes(sum, 200)})
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
