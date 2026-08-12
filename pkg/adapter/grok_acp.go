package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Pinned Grok Build ACP profile (#166 foundation, #191 official-contract hardening).
// Official launch: grok --no-auto-update agent stdio
// Auth: method selection only — credentials stay with the Grok process / XAI_API_KEY.
// Docs: https://docs.x.ai/build/integrations/acp (retrieved 2026-08-06)
const (
	GrokACPProfileV1       = "reinframe.grok_build_acp.v1"
	GrokACPProtocolVersion = 1
	MaxGrokACPMessageBytes = 1 << 20
	DefaultGrokACPStartup  = 15 * time.Second
	DefaultGrokACPQueue    = 64
	MaxGrokACPAuthMethods  = 16
	MaxGrokACPAuthMethodID = 128
	// Graceful then bounded force for owned process trees.
	grokACPGracefulWait = 2 * time.Second
	grokACPForceWait    = 3 * time.Second
)

// ACK layers (never collapse transport success into explicit agent ACK).
const (
	ACKLayerNone           = "none"
	ACKLayerTransport      = "transport"       // JSON-RPC response received
	ACKLayerSessionVisible = "session_visible" // session/update observed after delivery
	ACKLayerExplicit       = "explicit"        // host protocol documents agent receipt
	ACKLayerBehavioral     = "behavioral"      // subsequent tool/turn evidence
)

// Terminal transport errors (pending RPCs must not hang on caller deadlines alone).
var (
	ErrGrokACPReaderClosed = errors.New("grok acp: transport reader closed")
	ErrGrokACPClosed       = errors.New("grok acp: closed")
	ErrGrokACPAuth         = errors.New("grok acp: authenticate failed")
)

// GrokACPAuthError is a closed, non-retryable auth failure without credential material.
type GrokACPAuthError struct {
	Reason string
}

func (e *GrokACPAuthError) Error() string {
	if e == nil || e.Reason == "" {
		return ErrGrokACPAuth.Error()
	}
	return "grok acp: authenticate: " + e.Reason
}

func (e *GrokACPAuthError) Is(target error) bool {
	return target == ErrGrokACPAuth
}

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
	plat   grokProcPlatform
	stdin  io.WriteCloser
	stdout io.ReadCloser
	mu     sync.Mutex
	nextID atomic.Int64
	// pending maps request id → response channel
	pending map[int64]chan jsonRPCMessage
	// updates is a bounded queue of session/update notifications
	updates chan map[string]any
	// audit holds bounded lifecycle diagnostics (no secrets).
	audit []string
	// lastACK tracks strongest ACK layer observed for last advice delivery
	lastACK string
	closed  atomic.Bool
	// readerDone closed when stdout reader exits
	readerDone chan struct{}
	// terminalErr set when reader dies; fail-pending uses this.
	terminalErr error
	// negotiated holds capabilities from the last successful initialize result.
	negotiated GrokACPNegotiatedCaps
	// initOK is true only after fail-closed initialize succeeded.
	initOK bool
	// deliverMu serializes DrainUpdates + session/prompt + update wait across all
	// actuators sharing this client (Pro R13 P2: actuator-scoped mutex is insufficient).
	deliverMu sync.Mutex
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
	configureGrokProcess(cmd)
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
	plat, err := attachGrokProcess(cmd)
	if err != nil {
		_ = stdin.Close()
		_ = signalGrokProcess(cmd, &plat, true)
		_, _ = cmd.Process.Wait()
		releaseGrokProcess(&plat)
		return nil, fmt.Errorf("grok acp: attach process tree: %w", err)
	}
	c := &GrokACPClient{
		cfg:        cfg,
		cmd:        cmd,
		plat:       plat,
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
	go c.readLoop()
	return c
}

// Initialize sends initialize and returns the raw result only after fail-closed validation.
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
	caps, err := ParseGrokACPNegotiatedCaps(raw)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("grok acp: initialize result: %w", err)
	}
	c.mu.Lock()
	c.negotiated = caps
	c.initOK = true
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

// Authenticate selects an advertised auth method and sends the official delegated-auth
// envelope. Reinframe never accepts, stores, forwards, logs, or error-echoes credentials.
// Credential ownership remains with the Grok process environment (e.g. XAI_API_KEY).
func (c *GrokACPClient) Authenticate(ctx context.Context, methodID string) error {
	if !utf8.ValidString(methodID) || methodID == "" {
		return &GrokACPAuthError{Reason: "invalid method id"}
	}
	if len(methodID) > MaxGrokACPAuthMethodID {
		return &GrokACPAuthError{Reason: "method id too long"}
	}
	// Reject credential-shaped values if a caller mistakes the API.
	if looksLikeCredentialMarker(methodID) {
		return &GrokACPAuthError{Reason: "method id must not carry credential material"}
	}
	caps := c.Negotiated()
	if len(caps.AuthMethods) == 0 {
		return &GrokACPAuthError{Reason: "not advertised"}
	}
	ok := false
	for _, m := range caps.AuthMethods {
		if m == methodID {
			ok = true
			break
		}
	}
	if !ok {
		return &GrokACPAuthError{Reason: "method not advertised"}
	}
	// Official shape: methodId + _meta.headless — no token/code fields.
	params := map[string]any{
		"methodId": methodID,
		"_meta": map[string]any{
			"headless": true,
		},
	}
	if _, err := c.call(ctx, "authenticate", params); err != nil {
		// Never wrap remote payloads that might echo secrets.
		return &GrokACPAuthError{Reason: "rejected by agent"}
	}
	c.upgradeACK(ACKLayerTransport)
	return nil
}

// BuildGrokACPAuthenticateParams is the pure builder for the delegated-auth envelope (tests).
func BuildGrokACPAuthenticateParams(methodID string) (map[string]any, error) {
	if methodID == "" || !utf8.ValidString(methodID) || len(methodID) > MaxGrokACPAuthMethodID {
		return nil, &GrokACPAuthError{Reason: "invalid method id"}
	}
	if looksLikeCredentialMarker(methodID) {
		return nil, &GrokACPAuthError{Reason: "method id must not carry credential material"}
	}
	return map[string]any{
		"methodId": methodID,
		"_meta":    map[string]any{"headless": true},
	}, nil
}

func looksLikeCredentialMarker(s string) bool {
	lower := strings.ToLower(s)
	markers := []string{"token=", "bearer ", "sk-", "xai-", "api_key", "apikey", "auth.json", "-----begin"}
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// SessionNew creates a session; returns session id when present.
// When mcpServers is omitted, an empty list is sent so live Grok Build agents
// that require the field (protocol 1) still accept session/new.
// Caller maps are never mutated: params are shallow-copied before defaults apply.
func (c *GrokACPClient) SessionNew(ctx context.Context, params map[string]any) (string, error) {
	if !c.initialized() {
		return "", fmt.Errorf("grok acp: initialize required")
	}
	p := map[string]any{}
	for k, v := range params {
		p[k] = v
	}
	if _, ok := p["mcpServers"]; !ok {
		p["mcpServers"] = []any{}
	}
	raw, err := c.call(ctx, "session/new", p)
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

// PromptDeliveryMeta is transport metadata for a session/prompt delivery (#199).
// RequestID is the JSON-RPC request id; Result is the bounded response object when present.
type PromptDeliveryMeta struct {
	RequestID int64
	Result    map[string]any
}

// SessionPrompt delivers a prompt (safe-boundary advice path).
// Records transport ACK on JSON-RPC success; session_visible only via NoteSessionVisible
// after source-correlated matching (never from bare queue presence alone).
func (c *GrokACPClient) SessionPrompt(ctx context.Context, sessionID, prompt string, interventionID, challengeID string) error {
	_, err := c.SessionPromptMeta(ctx, sessionID, prompt, interventionID, challengeID)
	return err
}

// SessionPromptMeta is SessionPrompt plus JSON-RPC request id / result for correlation.
func (c *GrokACPClient) SessionPromptMeta(ctx context.Context, sessionID, prompt string, interventionID, challengeID string) (PromptDeliveryMeta, error) {
	var meta PromptDeliveryMeta
	if !c.initialized() {
		return meta, fmt.Errorf("grok acp: initialize required")
	}
	if sessionID == "" || prompt == "" {
		return meta, fmt.Errorf("grok acp: sessionId and prompt required")
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
	raw, id, err := c.callWithID(ctx, "session/prompt", params)
	meta.RequestID = id
	if err != nil {
		return meta, err
	}
	if len(raw) > 0 {
		var res map[string]any
		if json.Unmarshal(raw, &res) == nil {
			meta.Result = res
		}
	}
	// Transport ACK only if not already upgraded (session/update may race during call).
	c.upgradeACK(ACKLayerTransport)
	return meta, nil
}

// DrainUpdates discards all currently queued session/update notifications (pre-prompt watermark).
// Returns the number of drained messages. Does not block.
func (c *GrokACPClient) DrainUpdates() int {
	n := 0
	for {
		select {
		case _, ok := <-c.updates:
			if !ok {
				return n
			}
			n++
		default:
			return n
		}
	}
}

func (c *GrokACPClient) initialized() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.initOK
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

// AuditSnapshot returns a copy of bounded lifecycle diagnostics (no secrets).
func (c *GrokACPClient) AuditSnapshot() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.audit...)
}

func (c *GrokACPClient) noteAudit(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.audit) >= 32 {
		c.audit = c.audit[1:]
	}
	c.audit = append(c.audit, boundRunes(msg, 120))
}

// ProcessPID returns the owned process PID when StartGrokACPClient launched one.
// Zero means no owned process (e.g. NewGrokACPClientForTest) or process already reaped.
func (c *GrokACPClient) ProcessPID() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// Close gracefully shuts down the client and owned process tree (graceful → force).
// Success means shutdown was attempted; callers that need orphan proof should
// capture ProcessPID before Close and verify the PID is not alive afterward.
func (c *GrokACPClient) Close() error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}
	_ = c.stdin.Close()
	// Wait briefly for reader
	select {
	case <-c.readerDone:
	case <-time.After(grokACPGracefulWait):
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = signalGrokProcess(c.cmd, &c.plat, false)
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(grokACPForceWait):
			_ = signalGrokProcess(c.cmd, &c.plat, true)
			select {
			case <-done:
			case <-time.After(grokACPForceWait):
			}
		}
		releaseGrokProcess(&c.plat)
	}
	return nil
}

func (c *GrokACPClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	raw, _, err := c.callWithID(ctx, method, params)
	return raw, err
}

func (c *GrokACPClient) callWithID(ctx context.Context, method string, params any) (json.RawMessage, int64, error) {
	if c.closed.Load() {
		return nil, 0, ErrGrokACPClosed
	}
	id := c.nextID.Add(1)
	ch := make(chan jsonRPCMessage, 1)
	c.mu.Lock()
	if c.terminalErr != nil {
		err := c.terminalErr
		c.mu.Unlock()
		return nil, id, err
	}
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
		return nil, id, err
	}
	if len(raw) > c.cfg.MaxMessageBytes {
		return nil, id, fmt.Errorf("grok acp: request exceeds max size")
	}
	c.mu.Lock()
	_, err = c.stdin.Write(append(raw, '\n'))
	c.mu.Unlock()
	if err != nil {
		return nil, id, err
	}

	select {
	case <-ctx.Done():
		return nil, id, ctx.Err()
	case <-c.readerDone:
		c.mu.Lock()
		term := c.terminalErr
		c.mu.Unlock()
		if term == nil {
			term = ErrGrokACPReaderClosed
		}
		return nil, id, term
	case resp := <-ch:
		if resp.Error != nil {
			// Bound remote error text; never pass through credential-looking payloads.
			em := boundRunes(resp.Error.Message, 200)
			if looksLikeCredentialMarker(em) {
				em = "agent error redacted"
			}
			return nil, id, fmt.Errorf("grok acp: %s: %s", method, em)
		}
		return resp.Result, id, nil
	}
}

// UpdateStrongCorrelation reports whether a session/update carries identity that
// can prove source-correlation for a specific prompt delivery (#199).
// Session id alone is never enough: require matching interventionId, challengeId,
// or requestId when those fields are present on the update.
// If the update exposes none of those identity fields, correlation is impossible.
//
// Identity is read only from protocol envelope maps (top-level, params, update,
// and envelope meta) — never by recursively flattening agent-controlled content
// (tool-call args / structured content) that could echo the prompt's InterventionID
// (GPT-5.6 Pro P1 on recursive flatten spoofing).
func UpdateStrongCorrelation(update map[string]any, interventionID, challengeID string, requestID int64) (ok bool, reason string) {
	if update == nil {
		return false, "nil update"
	}
	// Non-session/update notifications must never upgrade to source-correlated ACK
	// even if they carry matching identity fields (Codex P1).
	if !isSessionUpdateNotification(update) {
		return false, "not a session/update notification"
	}
	flat, layerConflict := envelopeIdentityMap(update)
	if layerConflict {
		return false, "conflicting envelope identity fields across layers"
	}
	// Canonicalize alias keys (requestId vs request_id, etc.) before match checks
	// so unequal aliases cannot hide behind firstRequestID preference (Codex P1).
	if aliasConflict, why := identityAliasConflicts(flat); aliasConflict {
		return false, why
	}
	// Collect every identity present on the envelope, then require zero conflicts
	// before accepting any match (GPT-5.6 Pro / Codex P1: matching interventionId
	// must not ignore a mismatched requestId from a delayed retry).
	type hit struct {
		kind  string
		match bool
	}
	var hits []hit
	if v := firstStringAny(flat, "interventionId", "intervention_id", "InterventionID"); v != "" {
		hits = append(hits, hit{kind: "interventionId", match: interventionID != "" && v == interventionID})
	}
	if v := firstStringAny(flat, "challengeId", "challenge_id", "ChallengeID"); v != "" {
		hits = append(hits, hit{kind: "challengeId", match: challengeID != "" && v == challengeID})
	}
	if rid, ok := firstRequestID(flat); ok {
		hits = append(hits, hit{kind: "requestId", match: requestID > 0 && rid == requestID})
	}
	if len(hits) == 0 {
		return false, "update lacks request/intervention/challenge identity"
	}
	var matched []string
	var mismatched []string
	for _, h := range hits {
		if h.match {
			matched = append(matched, h.kind)
		} else {
			// Only treat as conflict when the delivery supplied that identity kind.
			// Unsupplied kinds (empty interventionID / challengeID / requestID<=0)
			// cannot mismatch.
			switch h.kind {
			case "interventionId":
				if interventionID != "" {
					mismatched = append(mismatched, h.kind)
				}
			case "challengeId":
				if challengeID != "" {
					mismatched = append(mismatched, h.kind)
				}
			case "requestId":
				if requestID > 0 {
					mismatched = append(mismatched, h.kind)
				}
			}
		}
	}
	if len(mismatched) > 0 {
		return false, "conflicting envelope identity fields: " + strings.Join(mismatched, ",")
	}
	if len(matched) == 0 {
		return false, "identity present but does not match this prompt"
	}
	// When the delivery assigned a JSON-RPC request id, require that id (or a
	// matching challenge) on the update. Intervention-only is insufficient:
	// reused interventionIds can ride a delayed update across DrainUpdates
	// (Codex P1 atomic watermark / false session_visible).
	if requestID > 0 {
		hasReq := false
		hasChal := false
		for _, m := range matched {
			if m == "requestId" {
				hasReq = true
			}
			if m == "challengeId" {
				hasChal = true
			}
		}
		if !hasReq && !hasChal {
			return false, "requestId required when delivery has request id (intervention-only insufficient)"
		}
	}
	// Prefer a single stable reason (intervention > challenge > request).
	for _, pref := range []string{"interventionId", "challengeId", "requestId"} {
		for _, m := range matched {
			if m == pref {
				return true, pref
			}
		}
	}
	return true, matched[0]
}

// envelopeIdentityKeys are the only fields used for strong correlation.
// Includes RPC aliases so they participate in layer/alias conflict detection
// (Pro R6 P1: rpcId was listed in firstRequestID but never copied from envelopes).
var envelopeIdentityKeys = []string{
	"interventionId", "intervention_id", "InterventionID",
	"challengeId", "challenge_id", "ChallengeID",
	"requestId", "request_id", "id",
	"rpcId", "rpc_id",
}

// envelopeIdentityMap copies identity fields from protocol envelopes only.
// It does not recurse into arbitrary nested maps (agent content / tool args).
// layerConflict is true when the same key appears with unequal values across
// envelope layers (top-level vs params vs update vs meta).
func envelopeIdentityMap(u map[string]any) (out map[string]any, layerConflict bool) {
	out = map[string]any{}
	if u == nil {
		return out, false
	}
	var conflict bool
	copyIdentityKeys(out, &conflict, u)
	// JSON-RPC notification envelope: params holds session/update body.
	if p, ok := u["params"].(map[string]any); ok {
		copyIdentityKeys(out, &conflict, p)
		if up, ok := p["update"].(map[string]any); ok {
			copyIdentityKeys(out, &conflict, up)
			if m, ok := up["meta"].(map[string]any); ok {
				copyIdentityKeys(out, &conflict, m)
			}
		}
		// Some hosts put meta beside update under params.
		if m, ok := p["meta"].(map[string]any); ok {
			copyIdentityKeys(out, &conflict, m)
		}
	}
	// Direct update envelope (tests / already-unwrapped notifications).
	if up, ok := u["update"].(map[string]any); ok {
		copyIdentityKeys(out, &conflict, up)
		if m, ok := up["meta"].(map[string]any); ok {
			copyIdentityKeys(out, &conflict, m)
		}
	}
	if m, ok := u["meta"].(map[string]any); ok {
		copyIdentityKeys(out, &conflict, m)
	}
	return out, conflict
}

func copyIdentityKeys(dst map[string]any, conflict *bool, src map[string]any) {
	if src == nil {
		return
	}
	for _, k := range envelopeIdentityKeys {
		v, ok := src[k]
		if !ok {
			continue
		}
		// Explicit JSON null is a present malformed identity (Pro R7).
		if v == nil {
			if conflict != nil {
				*conflict = true
			}
			// Keep a sentinel so later layers see a conflicted field.
			if _, exists := dst[k]; !exists {
				dst[k] = nil
			}
			continue
		}
		if existing, exists := dst[k]; exists {
			equal := false
			if isRequestIdentityKey(k) {
				equal = identityValuesEqual(existing, v)
			} else {
				// intervention/challenge: exact string equality only; any type mismatch
				// or value mismatch is a layer conflict (Pro R7 type smuggling).
				es, eOK := existing.(string)
				vs, vOK := v.(string)
				equal = eOK && vOK && es == vs
			}
			if !equal && conflict != nil {
				*conflict = true
			}
			continue
		}
		dst[k] = v
	}
}

// identityValuesEqual is used only for request-ID family keys (numeric forms).
// Intervention/challenge identities must use exact string equality separately
// (Pro R7 P1: string "42" must not equal numeric 42 for interventionId).
func identityValuesEqual(a, b any) bool {
	// Normalize JSON number-like forms so float64(42) == int(42) == "42".
	if ia, ok := coerceRequestIDValue(a); ok {
		if ib, ok := coerceRequestIDValue(b); ok {
			return ia == ib
		}
	}
	sa, aOK := a.(string)
	sb, bOK := b.(string)
	if aOK && bOK {
		return sa == sb
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// isRequestIdentityKey reports keys that use numeric request-id equality.
func isRequestIdentityKey(k string) bool {
	switch k {
	case "requestId", "request_id", "rpcId", "rpc_id", "id":
		return true
	default:
		return false
	}
}

func firstStringAny(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// identityAliasConflicts reports unequal values among supported identity aliases
// on a single flattened envelope (e.g. requestId=42 and request_id=99).
func identityAliasConflicts(flat map[string]any) (bool, string) {
	if flat == nil {
		return false, ""
	}
	// String identity families. Present non-string values are malformed and must
	// fail closed (Pro R6 P1: numeric interventionId was silently ignored).
	for _, fam := range []struct {
		kind string
		keys []string
	}{
		{"interventionId", []string{"interventionId", "intervention_id", "InterventionID"}},
		{"challengeId", []string{"challengeId", "challenge_id", "ChallengeID"}},
	} {
		var seen []string
		for _, k := range fam.keys {
			v, ok := flat[k]
			if !ok {
				continue
			}
			// Present null is malformed (must not be treated as absent).
			if v == nil {
				return true, "malformed identity field type: " + fam.kind
			}
			s, isStr := v.(string)
			if !isStr {
				return true, "malformed identity field type: " + fam.kind
			}
			if strings.TrimSpace(s) == "" {
				continue
			}
			seen = append(seen, s)
		}
		if len(seen) >= 2 {
			for i := 1; i < len(seen); i++ {
				if seen[i] != seen[0] {
					return true, "conflicting envelope identity aliases: " + fam.kind
				}
			}
		}
	}
	// Request ID family (numeric + full-string integer forms).
	// Keys match firstRequestID — generic "id" is not treated as request identity
	// here (too overloaded on JSON-RPC envelopes).
	//
	// Codex/Pro P1: a present but unparseable request field (e.g. "42junk") must
	// always conflict, even when no other request alias exists and another identity
	// family (interventionId) would otherwise match. Silent discard enables false
	// source-correlated session_visible upgrades.
	var rids []int64
	for _, k := range []string{"requestId", "request_id", "rpcId", "rpc_id"} {
		v, ok := flat[k]
		if !ok || v == nil {
			continue
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			continue
		}
		i, ok := coerceRequestIDValue(v)
		if !ok {
			return true, "unparseable request identity field: " + k
		}
		rids = append(rids, i)
	}
	if len(rids) >= 2 {
		for i := 1; i < len(rids); i++ {
			if rids[i] != rids[0] {
				return true, "conflicting envelope identity aliases: requestId"
			}
		}
	}
	return false, ""
}

// coerceRequestIDValue parses a canonical integer request id from JSON forms.
// String values must be entirely an integer (no prefix scan: "42junk" rejected).
func coerceRequestIDValue(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		if x != float64(int64(x)) {
			return 0, false
		}
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, false
		}
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}

func firstRequestID(m map[string]any) (int64, bool) {
	for _, k := range []string{"requestId", "request_id", "rpcId", "rpc_id"} {
		if v, ok := m[k]; ok {
			if i, ok := coerceRequestIDValue(v); ok {
				return i, true
			}
		}
	}
	return 0, false
}

// isSessionUpdateNotification reports whether the update is a session/update
// notification (not an unrelated ACP method carrying identity fields).
//
// Method rules (Pro R24/R25):
//   - Any recognized method key that is present (top-level "method", top-level
//     "_method", or params["_method"]) must be a string exactly equal to
//     "session/update" — no TrimSpace acceptance of padded names, no collapse of
//     non-string / null / empty into "absent" (Pro R25 P2).
//   - Multiple present method fields must all be valid and mutually equal.
//   - Shape-only fallback (sessionUpdate key) runs only when none of those keys exist.
func isSessionUpdateNotification(u map[string]any) bool {
	if u == nil {
		return false
	}
	type methodHit struct {
		present bool
		ok      bool
		value   string
	}
	parse := func(m map[string]any, key string) methodHit {
		if m == nil {
			return methodHit{}
		}
		raw, exists := m[key]
		if !exists {
			return methodHit{}
		}
		// Key present: must be exact string "session/update" (no trim).
		s, isStr := raw.(string)
		if !isStr {
			return methodHit{present: true, ok: false}
		}
		if s != "session/update" {
			return methodHit{present: true, ok: false, value: s}
		}
		return methodHit{present: true, ok: true, value: s}
	}

	topMethod := parse(u, "method")
	topInternal := parse(u, "_method")
	var paramsInternal methodHit
	if p, _ := u["params"].(map[string]any); p != nil {
		paramsInternal = parse(p, "_method")
	}

	hits := []methodHit{topMethod, topInternal, paramsInternal}
	anyPresent := false
	var accepted string
	for _, h := range hits {
		if !h.present {
			continue
		}
		anyPresent = true
		if !h.ok {
			return false
		}
		if accepted == "" {
			accepted = h.value
		} else if accepted != h.value {
			return false
		}
	}
	if anyPresent {
		return accepted == "session/update"
	}

	// Methodless JSON-RPC-shaped envelopes must fail closed (Pro R26 P2):
	// shape fallback is only for unwrapped update bodies, not incomplete envelopes.
	if jsonRPCEnvelopeWithoutMethod(u) {
		return false
	}

	// Unwrapped fixtures/tests without a method field: accept only when the body
	// looks like a session/update (sessionUpdate key present).
	if _, ok := u["sessionUpdate"]; ok {
		return true
	}
	if p, _ := u["params"].(map[string]any); p != nil {
		if _, ok := p["sessionUpdate"]; ok {
			return true
		}
		if up, _ := p["update"].(map[string]any); up != nil {
			if _, ok := up["sessionUpdate"]; ok {
				return true
			}
		}
	}
	if up, _ := u["update"].(map[string]any); up != nil {
		if _, ok := up["sessionUpdate"]; ok {
			return true
		}
	}
	return false
}

// jsonRPCEnvelopeWithoutMethod reports incomplete JSON-RPC envelopes that have
// envelope markers but no method/_method (Pro R26 P2).
func jsonRPCEnvelopeWithoutMethod(u map[string]any) bool {
	if u == nil {
		return false
	}
	if _, ok := u["jsonrpc"]; ok {
		return true
	}
	if _, ok := u["result"]; ok {
		return true
	}
	if _, ok := u["error"]; ok {
		return true
	}
	// id + params without sessionUpdate at top level is envelope-shaped.
	if _, hasID := u["id"]; hasID {
		if _, hasParams := u["params"]; hasParams {
			if _, topSU := u["sessionUpdate"]; !topSU {
				return true
			}
		}
	}
	// Bare params object carrying nested sessionUpdate (no top-level sessionUpdate).
	if p, ok := u["params"].(map[string]any); ok {
		if _, topSU := u["sessionUpdate"]; !topSU {
			if _, nested := p["sessionUpdate"]; nested {
				return true
			}
			if up, ok := p["update"].(map[string]any); ok {
				if _, nested := up["sessionUpdate"]; nested {
					return true
				}
			}
		}
	}
	return false
}

func (c *GrokACPClient) failAllPending(err error) {
	c.mu.Lock()
	c.terminalErr = err
	pending := c.pending
	c.pending = make(map[int64]chan jsonRPCMessage)
	c.mu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- jsonRPCMessage{Error: &jsonRPCError{Code: -32000, Message: "reader closed"}}:
		default:
		}
	}
}

func (c *GrokACPClient) readLoop() {
	defer func() {
		c.failAllPending(ErrGrokACPReaderClosed)
		close(c.readerDone)
	}()
	sc := bufio.NewScanner(c.stdout)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, c.cfg.MaxMessageBytes+1)
	seenIDs := make(map[int64]struct{})
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > c.cfg.MaxMessageBytes {
			c.noteAudit("drop_oversized_line")
			continue
		}
		if !utf8.Valid(line) {
			c.noteAudit("drop_invalid_utf8")
			continue
		}
		var msg jsonRPCMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			c.noteAudit("drop_malformed_json")
			continue
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
				c.noteAudit("queue_overflow_drop")
			}
			continue
		}
		if msg.ID != nil {
			id := *msg.ID
			if _, dup := seenIDs[id]; dup {
				c.noteAudit("duplicate_response_id")
				continue
			}
			seenIDs[id] = struct{}{}
			// Bound seenIDs growth
			if len(seenIDs) > 4096 {
				seenIDs = map[int64]struct{}{id: {}}
			}
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch == nil {
				c.noteAudit("unknown_response_id")
				continue
			}
			select {
			case ch <- msg:
			default:
				c.noteAudit("pending_channel_full")
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

// GrokACPNegotiatedCaps holds validated facts from initialize (not assumed).
// No unbounded raw maps — only closed typed fields.
type GrokACPNegotiatedCaps struct {
	ProtocolVersion int
	LoadSession     bool
	// Pause/Cancel/Resume are independent native capability facts.
	Pause  bool
	Cancel bool
	Resume bool
	// CancelMethod is session/cancel when empty and Cancel is true.
	CancelMethod string
	// ToolInspection / DiffInspection only when explicitly advertised.
	ToolInspection bool
	DiffInspection bool
	// AuthMethods are advertised method ids (never secrets).
	AuthMethods []string
	// CapsDigest is a bounded non-secret summary of recognized caps (not a raw dump).
	CapsDigest string
}

// GrokACPFoundationManifest is honest ACP-only capability claim.
// Prefer ManifestFromNegotiated after initialize; NewGrokACPFoundationManifest is pre-handshake defaults.
type GrokACPFoundationManifest struct {
	Profile            string   `json:"profile"`
	ProtocolVersion    int      `json:"protocol_version"`
	CapEventStream     bool     `json:"cap_event_stream"`
	CapToolInspection  bool     `json:"cap_tool_inspection"`
	CapAdviceDelivery  bool     `json:"cap_advice_delivery"`
	CapDiffInspection  bool     `json:"cap_diff_inspection"`
	CapPause           bool     `json:"cap_pause"`
	CapCancel          bool     `json:"cap_cancel"`
	CapResume          bool     `json:"cap_resume"`
	CapInterventionAck bool     `json:"cap_intervention_ack"`
	ExplicitAck        bool     `json:"explicit_ack"`
	LoadSession        bool     `json:"load_session"`
	AuthMethods        []string `json:"auth_methods,omitempty"`
	// NegotiatedLevel is -1 pre-handshake (not achieved); post-init from canonical evaluator.
	NegotiatedLevel int    `json:"negotiated_level"`
	HonestyNote     string `json:"honesty_note"`
}

// NewGrokACPFoundationManifest returns pre-negotiation defaults (no achieved level/caps).
func NewGrokACPFoundationManifest() GrokACPFoundationManifest {
	return GrokACPFoundationManifest{
		Profile:            GrokACPProfileV1,
		ProtocolVersion:    GrokACPProtocolVersion,
		CapEventStream:     false,
		CapToolInspection:  false,
		CapAdviceDelivery:  false,
		CapDiffInspection:  false,
		CapPause:           false,
		CapCancel:          false,
		CapResume:          false,
		CapInterventionAck: false,
		ExplicitAck:        false,
		LoadSession:        false,
		NegotiatedLevel:    -1,
		HonestyNote: "pre-handshake: no achieved level or unproven caps; call ManifestFromNegotiated after initialize; " +
			"JSON-RPC success is transport ACK not explicit agent ACK; never read/write ~/.grok/auth.json; " +
			"live #167 harness path via cmd/groklive does not imply disposition GO",
	}
}

// ParseGrokACPNegotiatedCaps validates and extracts capability facts from an initialize result.
// Accepts raw JSON bytes (preferred) or will marshal a map when used from tests via helper.
func ParseGrokACPNegotiatedCaps(initResult json.RawMessage) (GrokACPNegotiatedCaps, error) {
	var out GrokACPNegotiatedCaps
	if len(initResult) == 0 {
		return out, fmt.Errorf("grok acp: empty initialize result")
	}
	if len(initResult) > MaxGrokACPMessageBytes {
		return out, fmt.Errorf("grok acp: initialize result too large")
	}
	if !utf8.Valid(initResult) {
		return out, fmt.Errorf("grok acp: initialize result invalid utf-8")
	}
	// Closed typed decode — reject wrong types via strict intermediate.
	var wire struct {
		ProtocolVersion *float64       `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
		AgentCaps       map[string]any `json:"agentCapabilities"`
		AuthMethods     []any          `json:"authMethods"`
	}
	dec := json.NewDecoder(strings.NewReader(string(initResult)))
	dec.UseNumber()
	if err := dec.Decode(&wire); err != nil {
		return out, fmt.Errorf("grok acp: initialize decode: %w", err)
	}
	if wire.ProtocolVersion == nil {
		return out, fmt.Errorf("grok acp: protocolVersion required")
	}
	pv := int(*wire.ProtocolVersion)
	// Fail-closed: exact supported version only.
	if pv != GrokACPProtocolVersion {
		return out, fmt.Errorf("grok acp: unsupported protocolVersion %d (want %d)", pv, GrokACPProtocolVersion)
	}
	out.ProtocolVersion = pv

	caps := wire.Capabilities
	if caps == nil {
		caps = wire.AgentCaps
	}
	if caps != nil {
		out.LoadSession = boolCap(caps, "loadSession") || boolCap(caps, "load_session")
		out.Pause = boolCap(caps, "pause")
		out.Cancel = boolCap(caps, "cancel") || boolCap(caps, "promptCancel")
		out.Resume = boolCap(caps, "resume")
		out.ToolInspection = boolCap(caps, "toolInspection") || boolCap(caps, "tool_inspection")
		out.DiffInspection = boolCap(caps, "diffInspection") || boolCap(caps, "diff_inspection")
		if out.Cancel {
			out.CancelMethod = "session/cancel"
		}
		// Compact non-secret digest of recognized booleans only.
		out.CapsDigest = FormatGrokACPCapsDigest(
			out.LoadSession, out.Pause, out.Cancel, out.Resume, out.ToolInspection, out.DiffInspection)
	}

	if wire.AuthMethods != nil {
		if len(wire.AuthMethods) > MaxGrokACPAuthMethods {
			return out, fmt.Errorf("grok acp: too many authMethods")
		}
		seen := make(map[string]struct{}, len(wire.AuthMethods))
		for _, item := range wire.AuthMethods {
			id, err := parseAuthMethodID(item)
			if err != nil {
				return out, err
			}
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				return out, fmt.Errorf("grok acp: duplicate auth method %q", id)
			}
			seen[id] = struct{}{}
			out.AuthMethods = append(out.AuthMethods, id)
		}
	}
	return out, nil
}

// ParseGrokACPNegotiatedCapsMap is a test/helper wrapper for map input.
func ParseGrokACPNegotiatedCapsMap(initResult map[string]any) (GrokACPNegotiatedCaps, error) {
	raw, err := json.Marshal(initResult)
	if err != nil {
		return GrokACPNegotiatedCaps{}, err
	}
	return ParseGrokACPNegotiatedCaps(raw)
}

// FormatGrokACPCapsDigest returns the canonical non-secret capability digest string
// used in acp_manifest.json and live qualification (#218).
// Format is closed and versioned with the boolean field order; do not invent alternate digests.
func FormatGrokACPCapsDigest(load, pause, cancel, resume, tool, diff bool) string {
	return fmt.Sprintf("load=%t pause=%t cancel=%t resume=%t tool=%t diff=%t",
		load, pause, cancel, resume, tool, diff)
}

// CapsDigestFromFoundation recomputes the digest from a post-handshake foundation claim.
func CapsDigestFromFoundation(m GrokACPFoundationManifest) string {
	return FormatGrokACPCapsDigest(
		m.LoadSession, m.CapPause, m.CapCancel, m.CapResume, m.CapToolInspection, m.CapDiffInspection)
}

func boolCap(caps map[string]any, key string) bool {
	v, ok := caps[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func parseAuthMethodID(item any) (string, error) {
	switch t := item.(type) {
	case string:
		if !utf8.ValidString(t) {
			return "", fmt.Errorf("grok acp: auth method invalid utf-8")
		}
		if len(t) > MaxGrokACPAuthMethodID {
			return "", fmt.Errorf("grok acp: auth method id too long")
		}
		if looksLikeCredentialMarker(t) {
			return "", fmt.Errorf("grok acp: auth method id rejects credential markers")
		}
		return t, nil
	case map[string]any:
		id, _ := t["id"].(string)
		if id == "" {
			id, _ = t["methodId"].(string)
		}
		if id == "" {
			return "", nil
		}
		if !utf8.ValidString(id) || len(id) > MaxGrokACPAuthMethodID {
			return "", fmt.Errorf("grok acp: auth method id invalid")
		}
		if looksLikeCredentialMarker(id) {
			return "", fmt.Errorf("grok acp: auth method id rejects credential markers")
		}
		// Reject maps that embed credential-looking keys.
		for k := range t {
			lk := strings.ToLower(k)
			if lk == "token" || lk == "secret" || lk == "api_key" || lk == "apikey" || lk == "password" {
				return "", fmt.Errorf("grok acp: auth method object must not include credential fields")
			}
		}
		return id, nil
	default:
		return "", fmt.Errorf("grok acp: auth method entry has unsupported type")
	}
}

// ProtocolCapabilityManifest builds the canonical protocol.CapabilityManifest from negotiated facts.
// session/prompt is the advice path; session/update is the event stream. Tool/diff/pause require explicit ads.
func ProtocolCapabilityManifest(caps GrokACPNegotiatedCaps) protocol.CapabilityManifest {
	return protocol.CapabilityManifest{
		AgentID:                "grok_build_acp",
		Version:                GrokACPProfileV1,
		SupportsEventStream:    true, // ACP notifications after successful initialize
		SupportsAdviceDelivery: true, // session/prompt delivery path
		SupportsToolInspection: caps.ToolInspection,
		SupportsDiffInspection: caps.DiffInspection,
		SupportsPause:          caps.Pause, // native pause only when advertised
		SupportsCancel:         caps.Cancel,
		SupportsResume:         caps.Resume,
	}
}

// ManifestFromNegotiated builds the honest capability claim via the canonical level evaluator.
// Partial pause/cancel/resume never yields Level 2 without the full Level1+Level2 mask.
func ManifestFromNegotiated(caps GrokACPNegotiatedCaps) GrokACPFoundationManifest {
	m := NewGrokACPFoundationManifest()
	if caps.ProtocolVersion > 0 {
		m.ProtocolVersion = caps.ProtocolVersion
	}
	pm := ProtocolCapabilityManifest(caps)
	level := protocol.EvaluateAchievableLevel(&pm)
	m.CapEventStream = pm.SupportsEventStream
	m.CapToolInspection = pm.SupportsToolInspection
	m.CapAdviceDelivery = pm.SupportsAdviceDelivery
	m.CapDiffInspection = pm.SupportsDiffInspection
	m.CapPause = pm.SupportsPause
	m.CapCancel = pm.SupportsCancel
	m.CapResume = pm.SupportsResume
	m.LoadSession = caps.LoadSession
	m.AuthMethods = append([]string(nil), caps.AuthMethods...)
	m.NegotiatedLevel = level
	m.HonestyNote = "derived from initialize via protocol.EvaluateAchievableLevel; " +
		"Level 2 requires full Level1 mask plus CapDiffInspection+CapPause+CapCancel+CapResume; " +
		"JSON-RPC success is transport ACK not explicit agent ACK; never read/write ~/.grok/auth.json; " +
		"live #167 harness path exercised on cmd/groklive (does not claim disposition GO); " +
		"no CapPause/L2 claimed from hooks alone"
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
