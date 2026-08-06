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

// Headless observe-only profile (#166). Separate from ACP control.
// Official: grok -p --output-format streaming-json (read-only stream; no tool approvals).
const (
	GrokHeadlessObserveProfileV1 = "reinframe.grok_build_headless_observe.v1"
	MaxGrokHeadlessLineBytes     = 1 << 20
)

// GrokHeadlessObserveConfig launches a one-shot/observe headless stream.
type GrokHeadlessObserveConfig struct {
	Executable string
	// Args default to ["-p", "--output-format", "streaming-json"] plus prompt.
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
		HonestyNote: "headless streaming-json is observe-only; tool approvals/advice require ACP (#166); " +
			"never read/write ~/.grok/auth.json; live proof #167",
	}
}

// DefaultGrokHeadlessArgs builds argv for streaming-json observe (no shell).
func DefaultGrokHeadlessArgs(prompt string) []string {
	return []string{"-p", "--output-format", "streaming-json", "--", prompt}
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
	for _, a := range args {
		if strings.ContainsAny(a, ";|&") {
			return nil, fmt.Errorf("grok headless: args must not contain shell metacharacters")
		}
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
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
	configureGrokACPProcess(cmd) // same process-group discipline
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("grok headless: start: %w", err)
	}
	events, readErr := parseHeadlessStream(stdout)
	// Always reap process
	_ = signalGrokACPProcess(cmd, false)
	waitErr := cmd.Wait()
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
