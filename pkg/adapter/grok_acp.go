package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Pinned Grok Build ACP profile (#166).
// Official: grok --no-auto-update agent stdio | grok agent stdio
// Docs retrieved 2026-08-06: ~/.grok/docs/user-guide/15-agent-mode.md
const (
	GrokACPProfileV1       = "reinframe.grok_build_acp.v1"
	GrokACPProtocolVersion = 1
	MaxGrokACPMessageBytes = 1 << 20
	DefaultGrokACPStartup  = 15 * time.Second
	DefaultGrokACPQueue    = 64
)

// ACK layers (never collapse transport success into explicit agent ACK).
const (
	ACKLayerNone           = "none"
	ACKLayerTransport      = "transport"       // JSON-RPC response received
	ACKLayerSessionVisible = "session_visible" // session/update observed after delivery
	ACKLayerExplicit       = "explicit"        // host protocol documents agent receipt
	ACKLayerBehavioral     = "behavioral"      // subsequent tool/turn evidence
)

// GrokACPConfig launches or attaches a Grok ACP stdio process.
type GrokACPConfig struct {
	// Executable is resolved path to grok (required; no shell interpolation).
	Executable string
	// Args default to ["--no-auto-update", "agent", "stdio"] when empty.
	Args []string
	// WorkDir is optional process cwd (project root).
	WorkDir string
	// StartupTimeout bounds initialize handshake.
	StartupTimeout time.Duration
	// MaxMessageBytes bounds single JSON-RPC line (default MaxGrokACPMessageBytes).
	MaxMessageBytes int
	// QueueDepth bounds unread notifications (default DefaultGrokACPQueue).
	QueueDepth int
	// Env extra env; never injects secrets from auth.json.
	Env []string
}

// GrokACPClient is a bounded JSON-RPC 2.0 client over stdio.
type GrokACPClient struct {
	cfg    GrokACPConfig
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	nextID atomic.Int64
	// pending maps request id → response channel
	pending map[int64]chan jsonRPCMessage
	// updates is a bounded queue of session/update notifications
	updates chan map[string]any
	// lastACK tracks strongest ACK layer observed for last advice delivery
	lastACK string
	closed  atomic.Bool
	// readerDone closed when stdout reader exits
	readerDone chan struct{}
	// negotiated holds capabilities from the last successful initialize result.
	negotiated GrokACPNegotiatedCaps
}

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// DefaultGrokACPArgs is the production argv (no shell).
func DefaultGrokACPArgs() []string {
	return []string{"--no-auto-update", "agent", "stdio"}
}

// ResolveGrokExecutable returns an absolute path or error (no PATH shell).
func ResolveGrokExecutable(explicit string) (string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			abs, err := filepath.Abs(explicit)
			if err != nil {
				return "", err
			}
			explicit = abs
		}
		fi, err := os.Stat(explicit)
		if err != nil || fi.IsDir() {
			return "", fmt.Errorf("grok acp: executable not found: %s", explicit)
		}
		return explicit, nil
	}
	// Look up PATH without shell.
	p, err := exec.LookPath("grok")
	if err != nil {
		return "", fmt.Errorf("grok acp: grok not on PATH: %w", err)
	}
	return p, nil
}

// StartGrokACPClient starts a subprocess client.
func StartGrokACPClient(ctx context.Context, cfg GrokACPConfig) (*GrokACPClient, error) {
	if cfg.Executable == "" {
		return nil, fmt.Errorf("grok acp: Executable required")
	}
	if strings.ContainsAny(cfg.Executable, " \t\n;$|&") {
		// Reject obvious shell metacharacters in path (no interpolation).
		return nil, fmt.Errorf("grok acp: executable path must not contain shell metacharacters")
	}
	if len(cfg.Args) == 0 {
		cfg.Args = DefaultGrokACPArgs()
	}
	for _, a := range cfg.Args {
		if strings.ContainsAny(a, ";|&") {
			return nil, fmt.Errorf("grok acp: args must not contain shell metacharacters")
		}
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = DefaultGrokACPStartup
	}
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = MaxGrokACPMessageBytes
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = DefaultGrokACPQueue
	}
	cmd := exec.CommandContext(ctx, cfg.Executable, cfg.Args...)
	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}
	if len(cfg.Env) > 0 {
		cmd.Env = append(os.Environ(), cfg.Env...)
	}
	// Never load auth.json contents into env here.
	// Platform process group / job setup for tree cleanup (Unix Setpgid; Windows new group).
	configureGrokACPProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard // do not log secrets from stderr by default
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("grok acp: start: %w", err)
	}
	c := &GrokACPClient{
		cfg:        cfg,
		cmd:        cmd,
		stdin:      stdin,
		stdout:     stdout,
		pending:    make(map[int64]chan jsonRPCMessage),
		updates:    make(chan map[string]any, cfg.QueueDepth),
		lastACK:    ACKLayerNone,
		readerDone: make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// NewGrokACPClientForTest wires fake pipes (no process).
func NewGrokACPClientForTest(serverIn io.WriteCloser, serverOut io.ReadCloser, cfg GrokACPConfig) *GrokACPClient {
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = MaxGrokACPMessageBytes
	}
	if cfg.QueueDepth <= 0 {
		cfg.QueueDepth = DefaultGrokACPQueue
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = DefaultGrokACPStartup
	}
	c := &GrokACPClient{
		cfg:        cfg,
		stdin:      serverIn,  // client writes requests here (server reads)
		stdout:     serverOut, // client reads responses here (server writes)
		pending:    make(map[int64]chan jsonRPCMessage),
		updates:    make(chan map[string]any, cfg.QueueDepth),
		lastACK:    ACKLayerNone,
		readerDone: make(chan struct{}),
	}
	// Swap naming: for tests, caller provides client-facing pipes.
	// Convention: serverIn is what client writes to; serverOut is what client reads.
	go c.readLoop()
	return c
}

// Initialize sends initialize and returns server capabilities result.
// Negotiated capabilities are stored for SessionLoad/Cancel and ManifestFromNegotiated.
func (c *GrokACPClient) Initialize(ctx context.Context, clientInfo map[string]any) (map[string]any, error) {
	params := map[string]any{
		"protocolVersion": GrokACPProtocolVersion,
		"clientInfo":      clientInfo,
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("grok acp: initialize result: %w", err)
	}
	caps := ParseGrokACPNegotiatedCaps(out)
	c.mu.Lock()
	c.negotiated = caps
	c.mu.Unlock()
	c.upgradeACK(ACKLayerTransport)
	return out, nil
}

// Negotiated returns a copy of capabilities from the last initialize.
func (c *GrokACPClient) Negotiated() GrokACPNegotiatedCaps {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negotiated
}

// Authenticate calls ACP authenticate when the server advertised auth methods.
// methodName must appear in negotiated authMethods. Never reads auth.json.
// tokenOrCode is an operator-supplied credential (env/CLI), never logged.
func (c *GrokACPClient) Authenticate(ctx context.Context, methodName, tokenOrCode string) error {
	caps := c.Negotiated()
	if len(caps.AuthMethods) == 0 {
		return fmt.Errorf("grok acp: authenticate not advertised")
	}
	ok := false
	for _, m := range caps.AuthMethods {
		if m == methodName {
			ok = true
			break
		}
	}
	if !ok {
		return fmt.Errorf("grok acp: auth method %q not advertised", methodName)
	}
	if strings.TrimSpace(tokenOrCode) == "" {
		return fmt.Errorf("grok acp: empty credential")
	}
	// Do not put credential into error strings if call fails with wrapper only.
	params := map[string]any{
		"methodId": methodName,
		// ACP auth shapes vary; keep opaque token field without logging it.
		"token": tokenOrCode,
	}
	_, err := c.call(ctx, "authenticate", params)
	if err != nil {
		return fmt.Errorf("grok acp: authenticate failed")
	}
	c.upgradeACK(ACKLayerTransport)
	return nil
}

// SessionNew creates a session; returns session id when present.
func (c *GrokACPClient) SessionNew(ctx context.Context, params map[string]any) (string, error) {
	if params == nil {
		params = map[string]any{}
	}
	raw, err := c.call(ctx, "session/new", params)
	if err != nil {
		return "", err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	id, _ := out["sessionId"].(string)
	if id == "" {
		id, _ = out["session_id"].(string)
	}
	c.upgradeACK(ACKLayerTransport)
	return id, nil
}

// SessionLoad loads an existing session when negotiated LoadSession is true.
func (c *GrokACPClient) SessionLoad(ctx context.Context, sessionID string, params map[string]any) error {
	if sessionID == "" {
		return fmt.Errorf("grok acp: sessionId required")
	}
	if !c.Negotiated().LoadSession {
		return fmt.Errorf("grok acp: session/load not negotiated")
	}
	if params == nil {
		params = map[string]any{}
	}
	params["sessionId"] = sessionID
	_, err := c.call(ctx, "session/load", params)
	if err != nil {
		return err
	}
	c.upgradeACK(ACKLayerTransport)
	return nil
}

// Cancel sends a cancellation request only when the server negotiated cancel support.
func (c *GrokACPClient) Cancel(ctx context.Context, sessionID string) error {
	if !c.Negotiated().Cancel {
		return fmt.Errorf("grok acp: cancel not negotiated")
	}
	params := map[string]any{}
	if sessionID != "" {
		params["sessionId"] = sessionID
	}
	// Prefer session/cancel when advertised; fall back to generic cancel method name.
	method := "session/cancel"
	if c.Negotiated().CancelMethod != "" {
		method = c.Negotiated().CancelMethod
	}
	_, err := c.call(ctx, method, params)
	if err != nil {
		return err
	}
	c.upgradeACK(ACKLayerTransport)
	return nil
}

// SessionPrompt delivers a prompt (safe-boundary advice path).
// Records transport ACK on JSON-RPC success; session_visible when a matching update arrives.
func (c *GrokACPClient) SessionPrompt(ctx context.Context, sessionID, prompt string, interventionID, challengeID string) error {
	if sessionID == "" || prompt == "" {
		return fmt.Errorf("grok acp: sessionId and prompt required")
	}
	// Bound prompt (no secrets from auth files).
	// Official ACP: prompt is ContentBlock[] e.g. [{type:"text", text:"..."}].
	prompt = boundRunes(prompt, MaxGrokContextRunes)
	params := map[string]any{
		"sessionId": sessionID,
		"prompt": []any{
			map[string]any{"type": "text", "text": prompt},
		},
	}
	if interventionID != "" {
		params["interventionId"] = interventionID
	}
	if challengeID != "" {
		params["challengeId"] = challengeID
	}
	_, err := c.call(ctx, "session/prompt", params)
	if err != nil {
		return err
	}
	// Transport ACK only if not already upgraded (session/update may race during call).
	c.upgradeACK(ACKLayerTransport)
	return nil
}

// NoteSessionVisible upgrades ACK when a session/update is observed after delivery.
func (c *GrokACPClient) NoteSessionVisible() {
	c.upgradeACK(ACKLayerSessionVisible)
}

// upgradeACK records the strongest ACK layer without inventing explicit/behavioral.
func (c *GrokACPClient) upgradeACK(layer string) {
	rank := map[string]int{
		ACKLayerNone: 0, ACKLayerTransport: 1, ACKLayerSessionVisible: 2,
		// explicit/behavioral reserved for live proof only
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if rank[layer] > rank[c.lastACK] {
		c.lastACK = layer
	}
}

// LastACKLayer returns the strongest ACK layer recorded (never invents explicit).
func (c *GrokACPClient) LastACKLayer() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastACK
}

// Updates returns the notification queue (session/update etc.).
func (c *GrokACPClient) Updates() <-chan map[string]any {
	return c.updates
}

// Close gracefully shuts down the client and process group / job tree.
func (c *GrokACPClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = c.stdin.Close()
	// Wait briefly for reader
	select {
	case <-c.readerDone:
	case <-time.After(2 * time.Second):
	}
	if c.cmd != nil && c.cmd.Process != nil {
		// Platform-aware: Unix process group; Windows process tree signal.
		_ = signalGrokACPProcess(c.cmd, false)
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = signalGrokACPProcess(c.cmd, true)
			<-done
		}
	}
	return nil
}

func (c *GrokACPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, fmt.Errorf("grok acp: closed")
	}
	id := c.nextID.Add(1)
	ch := make(chan jsonRPCMessage, 1)
	c.mu.Lock()
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	msg := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	if len(raw) > c.cfg.MaxMessageBytes {
		return nil, fmt.Errorf("grok acp: request exceeds max size")
	}
	c.mu.Lock()
	_, err = c.stdin.Write(append(raw, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("grok acp: %s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

func (c *GrokACPClient) readLoop() {
	defer close(c.readerDone)
	sc := bufio.NewScanner(c.stdout)
	// Increase buffer for large messages
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, c.cfg.MaxMessageBytes+1)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > c.cfg.MaxMessageBytes {
			continue // drop oversized
		}
		var msg jsonRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue // drop malformed
		}
		if msg.Method != "" && msg.ID == nil {
			// Notification
			var params map[string]any
			_ = json.Unmarshal(msg.Params, &params)
			if params == nil {
				params = map[string]any{"method": msg.Method}
			} else {
				params["_method"] = msg.Method
			}
			if msg.Method == "session/update" {
				c.NoteSessionVisible()
			}
			select {
			case c.updates <- params:
			default:
				// drop when queue full (bounded)
			}
			continue
		}
		if msg.ID != nil {
			c.mu.Lock()
			ch := c.pending[*msg.ID]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
				}
			}
		}
	}
}

// MapSessionUpdateToSummary produces a bounded redacted event summary (no private CoT).
// Accepts either flat params or official nested {"update":{"sessionUpdate":...}}.
func MapSessionUpdateToSummary(update map[string]any) (kind string, summary string) {
	// Normalize nested ACP shape: params.update.sessionUpdate
	if nested, ok := update["update"].(map[string]any); ok {
		update = nested
	}
	kind, _ = update["sessionUpdate"].(string)
	if kind == "" {
		kind, _ = update["type"].(string)
	}
	if kind == "" {
		kind = "unknown"
	}
	// Prefer tool name / status over free text; never store thought chunks fully.
	if kind == "agent_thought_chunk" {
		return kind, "thought_omitted"
	}
	if tn, ok := update["toolName"].(string); ok {
		summary = "tool=" + tn
	} else if s, ok := update["status"].(string); ok {
		summary = "status=" + s
	} else {
		summary = kind
	}
	return kind, boundRunes(summary, 200)
}

// GrokACPNegotiatedCaps holds facts parsed from initialize (not assumed).
type GrokACPNegotiatedCaps struct {
	ProtocolVersion int
	LoadSession     bool
	Cancel          bool
	// CancelMethod is session/cancel when empty and Cancel is true.
	CancelMethod string
	// AuthMethods are advertised method ids (never secrets).
	AuthMethods []string
	// CapPause is true only when server explicitly advertises pause/cancel/resume mask.
	CapPause bool
	// Raw caps object for diagnostics (bounded, no secrets).
	Raw map[string]any
}

// GrokACPFoundationManifest is honest ACP-only capability claim.
// Prefer ManifestFromNegotiated after initialize; NewGrokACPFoundationManifest is pre-handshake defaults.
type GrokACPFoundationManifest struct {
	Profile            string   `json:"profile"`
	ProtocolVersion    int      `json:"protocol_version"`
	CapEventStream     bool     `json:"cap_event_stream"`
	CapAdviceDelivery  bool     `json:"cap_advice_delivery"`
	CapPause           bool     `json:"cap_pause"`
	CapInterventionAck bool     `json:"cap_intervention_ack"`
	ExplicitAck        bool     `json:"explicit_ack"`
	LoadSession        bool     `json:"load_session"`
	Cancel             bool     `json:"cancel"`
	AuthMethods        []string `json:"auth_methods,omitempty"`
	NegotiatedLevel    int      `json:"negotiated_level"`
	HonestyNote        string   `json:"honesty_note"`
}

// NewGrokACPFoundationManifest returns pre-negotiation defaults (conservative).
func NewGrokACPFoundationManifest() GrokACPFoundationManifest {
	return GrokACPFoundationManifest{
		Profile:            GrokACPProfileV1,
		ProtocolVersion:    GrokACPProtocolVersion,
		CapEventStream:     true,
		CapAdviceDelivery:  true,
		CapPause:           false,
		CapInterventionAck: false,
		ExplicitAck:        false,
		LoadSession:        false,
		Cancel:             false,
		NegotiatedLevel:    1,
		HonestyNote: "pre-handshake defaults; call ManifestFromNegotiated after initialize; " +
			"JSON-RPC success is transport ACK not explicit agent ACK; never read/write ~/.grok/auth.json",
	}
}

// ParseGrokACPNegotiatedCaps extracts capability facts from an initialize result map.
func ParseGrokACPNegotiatedCaps(initResult map[string]any) GrokACPNegotiatedCaps {
	out := GrokACPNegotiatedCaps{ProtocolVersion: GrokACPProtocolVersion}
	if initResult == nil {
		return out
	}
	if pv, ok := initResult["protocolVersion"].(float64); ok {
		out.ProtocolVersion = int(pv)
	}
	caps, _ := initResult["capabilities"].(map[string]any)
	if caps == nil {
		// Some servers nest under agentCapabilities
		caps, _ = initResult["agentCapabilities"].(map[string]any)
	}
	if caps != nil {
		out.Raw = caps
		if v, ok := caps["loadSession"].(bool); ok {
			out.LoadSession = v
		}
		if v, ok := caps["load_session"].(bool); ok {
			out.LoadSession = v
		}
		// Cancel / prompt cancel
		if v, ok := caps["cancel"].(bool); ok && v {
			out.Cancel = true
			out.CancelMethod = "session/cancel"
		}
		if v, ok := caps["promptCancel"].(bool); ok && v {
			out.Cancel = true
			out.CancelMethod = "session/cancel"
		}
		// CapPause only with full pause/cancel/resume mask if advertised.
		pause, _ := caps["pause"].(bool)
		cancel, _ := caps["cancel"].(bool)
		resume, _ := caps["resume"].(bool)
		if pause && cancel && resume {
			out.CapPause = true
			out.Cancel = true
		}
	}
	// Auth methods: authMethods array of {id} or string ids — never tokens.
	if arr, ok := initResult["authMethods"].([]any); ok {
		for _, item := range arr {
			switch t := item.(type) {
			case string:
				if t != "" {
					out.AuthMethods = append(out.AuthMethods, t)
				}
			case map[string]any:
				if id, _ := t["id"].(string); id != "" {
					out.AuthMethods = append(out.AuthMethods, id)
				} else if id, _ := t["methodId"].(string); id != "" {
					out.AuthMethods = append(out.AuthMethods, id)
				}
			}
		}
	}
	return out
}

// ManifestFromNegotiated builds the honest capability claim from negotiated facts.
func ManifestFromNegotiated(caps GrokACPNegotiatedCaps) GrokACPFoundationManifest {
	m := NewGrokACPFoundationManifest()
	if caps.ProtocolVersion > 0 {
		m.ProtocolVersion = caps.ProtocolVersion
	}
	m.LoadSession = caps.LoadSession
	m.Cancel = caps.Cancel
	m.CapPause = caps.CapPause
	m.AuthMethods = append([]string(nil), caps.AuthMethods...)
	if caps.CapPause {
		m.NegotiatedLevel = 2
	} else {
		m.NegotiatedLevel = 1
		m.CapPause = false
	}
	m.HonestyNote = "derived from initialize negotiation; CapPause only with full pause+cancel+resume; " +
		"JSON-RPC success is transport ACK not explicit agent ACK; never read/write ~/.grok/auth.json"
	return m
}

// BuildAdvicePrompt builds safe-boundary advice text with InterventionID/ChallengeID.
func BuildAdvicePrompt(kind, body, interventionID, challengeID string) string {
	parts := []string{"[reinframe advice]", "kind=" + kind}
	if interventionID != "" {
		parts = append(parts, "InterventionID="+interventionID)
	}
	if challengeID != "" {
		parts = append(parts, "ChallengeID="+challengeID)
	}
	parts = append(parts, boundRunes(body, MaxGrokContextRunes))
	return strings.Join(parts, "\n")
}
