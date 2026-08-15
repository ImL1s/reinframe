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
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// Codex App Server Protocol & Limits (#184).
const (
	CodexAppServerProtocolVersion        = 1
	MaxCodexAppServerMessageBytes        = 1048576 // 1MB (#184 max_message_bytes limit)
	DefaultCodexAppServerStartup         = 15 * time.Second
	DefaultCodexAppServerRequestTimeout  = 30 * time.Second
	DefaultCodexAppServerEventsQueue     = 256
	DefaultCodexAppServerApprovalsQueue  = 64
	codexAppServerGracefulWait           = 2 * time.Second
	codexAppServerForceWait              = 3 * time.Second
)

// AppServerErrorCode represents closed typed error codes for App Server operations (#184).
type AppServerErrorCode string

const (
	ErrCodeBinaryNotFound        AppServerErrorCode = "binary_not_found"
	ErrCodeUnsupportedVersion    AppServerErrorCode = "unsupported_version"
	ErrCodeStartupTimeout        AppServerErrorCode = "startup_timeout"
	ErrCodeProtocolMalformed     AppServerErrorCode = "protocol_malformed"
	ErrCodeProtocolOversized     AppServerErrorCode = "protocol_oversized"
	ErrCodeRequestTimeout        AppServerErrorCode = "request_timeout"
	ErrCodeRuntimeCrashed        AppServerErrorCode = "runtime_crashed"
	ErrCodeAuthRequired          AppServerErrorCode = "auth_required"
	ErrCodeAuthExpired           AppServerErrorCode = "auth_expired"
	ErrCodeApprovalUnsupported   AppServerErrorCode = "approval_unsupported"
	ErrCodeModelUnavailable      AppServerErrorCode = "model_unavailable"
	ErrCodeModelIdentityUnproven AppServerErrorCode = "model_identity_unproven"
	ErrCodeRateLimited           AppServerErrorCode = "rate_limited"
	ErrCodeShutdownFailed        AppServerErrorCode = "shutdown_failed"
)

// AppServerError is a structured, closed error type with error code and optional cause (#184).
type AppServerError struct {
	Code    AppServerErrorCode `json:"code"`
	Message string             `json:"message"`
	Cause   error              `json:"-"`
}

func (e *AppServerError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("codex app-server [%s]: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("codex app-server [%s]: %s", e.Code, e.Message)
}

func (e *AppServerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *AppServerError) Is(target error) bool {
	if target == nil {
		return e == nil
	}
	if t, ok := target.(*AppServerError); ok {
		if t.Code != "" && e.Code != "" {
			return t.Code == e.Code
		}
	}
	return false
}

// NewAppServerError creates a new structured AppServerError.
func NewAppServerError(code AppServerErrorCode, msg string, cause ...error) *AppServerError {
	var c error
	if len(cause) > 0 {
		c = cause[0]
	}
	return &AppServerError{
		Code:    code,
		Message: msg,
		Cause:   c,
	}
}

// Sentinel typed errors matching closed AppServerErrorCode values.
var (
	ErrBinaryNotFound        = &AppServerError{Code: ErrCodeBinaryNotFound, Message: "codex app-server binary not found"}
	ErrUnsupportedVersion    = &AppServerError{Code: ErrCodeUnsupportedVersion, Message: "unsupported protocol or app-server version"}
	ErrStartupTimeout        = &AppServerError{Code: ErrCodeStartupTimeout, Message: "app-server startup timed out"}
	ErrProtocolMalformed     = &AppServerError{Code: ErrCodeProtocolMalformed, Message: "malformed JSON-RPC protocol message"}
	ErrProtocolOversized     = &AppServerError{Code: ErrCodeProtocolOversized, Message: "protocol message exceeds 1MB limit"}
	ErrRequestTimeout        = &AppServerError{Code: ErrCodeRequestTimeout, Message: "request timed out"}
	ErrRuntimeCrashed        = &AppServerError{Code: ErrCodeRuntimeCrashed, Message: "app-server runtime process crashed or exited unexpectedly"}
	ErrAuthRequired          = &AppServerError{Code: ErrCodeAuthRequired, Message: "authentication required"}
	ErrAuthExpired           = &AppServerError{Code: ErrCodeAuthExpired, Message: "authentication expired"}
	ErrApprovalUnsupported   = &AppServerError{Code: ErrCodeApprovalUnsupported, Message: "approval requested but unsupported"}
	ErrModelUnavailable      = &AppServerError{Code: ErrCodeModelUnavailable, Message: "requested model unavailable"}
	ErrModelIdentityUnproven = &AppServerError{Code: ErrCodeModelIdentityUnproven, Message: "model identity unproven"}
	ErrRateLimited           = &AppServerError{Code: ErrCodeRateLimited, Message: "rate limited by provider"}
	ErrShutdownFailed        = &AppServerError{Code: ErrCodeShutdownFailed, Message: "failed to shutdown app-server cleanly"}
)

// ModelSubstitutionState indicates how model selection matched requested model (#184).
type ModelSubstitutionState string

const (
	ModelSubstitutionExact            ModelSubstitutionState = "exact_match"
	ModelSubstitutionFallbackAllowed  ModelSubstitutionState = "fallback_allowed"
	ModelSubstitutionIdentityUnproven ModelSubstitutionState = "identity_unproven"
	ModelSubstitutionViolated         ModelSubstitutionState = "substitution_violated"
)

// ModelIdentityState tracks requested vs reported model identities and substitution status (#184).
type ModelIdentityState struct {
	RequestedModelID           string                 `json:"requested_model_id"`
	ReportedModelID            string                 `json:"reported_model_id"`
	AllowProviderModelFallback bool                   `json:"allow_provider_model_fallback"`
	SubstitutionState          ModelSubstitutionState `json:"substitution_state"`
}

// VerifyModelIdentity validates requested model against reported model under fail-closed substitution invariants (#184).
func VerifyModelIdentity(requested, reported string, allowFallback bool) (ModelIdentityState, error) {
	reqTrim := strings.TrimSpace(requested)
	repTrim := strings.TrimSpace(reported)

	// If effective reported model is absent, identity is unproven (never assume requested model ran).
	if repTrim == "" {
		st := ModelIdentityState{
			RequestedModelID:           reqTrim,
			ReportedModelID:            "",
			AllowProviderModelFallback: allowFallback,
			SubstitutionState:          ModelSubstitutionIdentityUnproven,
		}
		return st, NewAppServerError(ErrCodeModelIdentityUnproven, "reported model identity is absent / unproven")
	}

	// When requested is not specified or exact match
	if reqTrim == "" || reqTrim == repTrim {
		return ModelIdentityState{
			RequestedModelID:           reqTrim,
			ReportedModelID:            repTrim,
			AllowProviderModelFallback: allowFallback,
			SubstitutionState:          ModelSubstitutionExact,
		}, nil
	}

	// Requested != reported
	if allowFallback {
		return ModelIdentityState{
			RequestedModelID:           reqTrim,
			ReportedModelID:            repTrim,
			AllowProviderModelFallback: true,
			SubstitutionState:          ModelSubstitutionFallbackAllowed,
		}, nil
	}

	// Fallback not allowed -> violation & fail-closed error
	st := ModelIdentityState{
		RequestedModelID:           reqTrim,
		ReportedModelID:            repTrim,
		AllowProviderModelFallback: false,
		SubstitutionState:          ModelSubstitutionViolated,
	}
	return st, NewAppServerError(ErrCodeModelUnavailable, fmt.Sprintf("model %q unavailable and fallback not allowed (server reported %q)", reqTrim, repTrim))
}

// ApprovalKind represents the bounded approval request types (#184).
type ApprovalKind string

const (
	ApprovalKindCommand ApprovalKind = "command"
	ApprovalKindFile    ApprovalKind = "file"
	ApprovalKindTool    ApprovalKind = "tool"
)

// ApprovalDecision represents the closed approval outcome (#184).
type ApprovalDecision string

const (
	ApprovalDecisionAllow  ApprovalDecision = "allow"
	ApprovalDecisionDeny   ApprovalDecision = "deny"
	ApprovalDecisionCancel ApprovalDecision = "cancel"
)

// ApprovalRequest represents an interception request from App Server (#184).
type ApprovalRequest struct {
	RequestID string          `json:"request_id"`
	ThreadID  string          `json:"thread_id"`
	TurnID    string          `json:"turn_id"`
	Kind      ApprovalKind    `json:"kind"`
	ToolName  string          `json:"tool_name,omitempty"`
	Command   string          `json:"command,omitempty"`
	FilePath  string          `json:"file_path,omitempty"`
	Detail    json.RawMessage `json:"detail,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// ApprovalResponse represents the structured approval decision sent back to App Server (#184).
type ApprovalResponse struct {
	RequestID  string           `json:"request_id"`
	Decision   ApprovalDecision `json:"decision"`
	ReasonCode string           `json:"reason_code,omitempty"`
	Detail     map[string]any   `json:"detail,omitempty"`
}

// RouteApprovalRequest routes an AppServer ApprovalRequest through Reinframe HookGate / Policy (#184).
func RouteApprovalRequest(ctx context.Context, req ApprovalRequest, policy HookPolicy) ApprovalResponse {
	if ctx != nil && ctx.Err() != nil {
		return ApprovalResponse{
			RequestID:  req.RequestID,
			Decision:   ApprovalDecisionCancel,
			ReasonCode: ReasonContextCanceled,
		}
	}

	hookReq := HookRequest{
		SessionID: req.ThreadID,
		ToolName:  req.ToolName,
		FilePath:  req.FilePath,
	}
	if req.Kind == ApprovalKindCommand {
		hookReq.Phase = "PreCommand"
		if req.Command != "" && hookReq.ToolName == "" {
			hookReq.ToolName = req.Command
		}
	} else {
		hookReq.Phase = "PreTool"
	}

	decision := EvaluateHook(ctx, hookReq, policy)
	var appDecision ApprovalDecision
	switch decision.Action {
	case HookActionAllow:
		appDecision = ApprovalDecisionAllow
	case HookActionDeny:
		if ctx != nil && ctx.Err() != nil {
			appDecision = ApprovalDecisionCancel
		} else {
			appDecision = ApprovalDecisionDeny
		}
	case HookActionDefer:
		if ctx != nil && ctx.Err() != nil {
			appDecision = ApprovalDecisionCancel
		} else {
			appDecision = ApprovalDecisionDeny
		}
	default:
		appDecision = ApprovalDecisionDeny
	}

	return ApprovalResponse{
		RequestID:  req.RequestID,
		Decision:   appDecision,
		ReasonCode: decision.ReasonCode,
	}
}

// RuntimeEvent is an event emitted by Codex App Server (#184).
type RuntimeEvent struct {
	EventID     string          `json:"event_id"`
	Type        string          `json:"type"`
	SequenceNum int64           `json:"sequence_num"`
	ThreadID    string          `json:"thread_id"`
	TurnID      string          `json:"turn_id"`
	ItemID      string          `json:"item_id,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

// ToAgentEvent maps a RuntimeEvent into canonical protocol.AgentEvent with deterministic sequence numbering (#184).
func (re RuntimeEvent) ToAgentEvent(sessionID string, seq int64) protocol.AgentEvent {
	sess := sessionID
	if sess == "" {
		sess = re.ThreadID
	}
	eventType := mapRuntimeEventTypeToProtocol(re.Type)
	ts := re.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	eventID := re.EventID
	if eventID == "" {
		eventID = fmt.Sprintf("codex_app_server|%s|seq-%d|%s", sess, seq, re.Type)
	}

	return protocol.AgentEvent{
		EventID:     eventID,
		SessionID:   sess,
		SequenceNum: seq,
		EventType:   eventType,
		Timestamp:   ts,
		Payload:     re.Payload,
	}
}

func mapRuntimeEventTypeToProtocol(rtType string) string {
	switch strings.ToLower(rtType) {
	case "tool_call", "tool.call", "tool_call_event", "item.tool_call":
		return "tool_call"
	case "file_change", "file.change", "item.file_change":
		return "file_change"
	case "test_result", "test.result":
		return "test_result"
	case "turn.started", "turn_start", "turn.start":
		return "turn_start"
	case "turn.finished", "turn_end", "turn.end":
		return "turn_end"
	case "error", "turn.error":
		return "error"
	case "status_change", "status.change":
		return "status_change"
	default:
		return "message"
	}
}

// ThreadStartRequest specifies parameters for starting a new thread (#184).
type ThreadStartRequest struct {
	ThreadID                   string            `json:"thread_id,omitempty"`
	ModelID                    string            `json:"model_id,omitempty"`
	WorkDir                    string            `json:"work_dir,omitempty"`
	Scope                      []string          `json:"scope,omitempty"`
	AllowProviderModelFallback bool              `json:"allow_provider_model_fallback"`
	Metadata                   map[string]string `json:"metadata,omitempty"`
	Env                        []string          `json:"env,omitempty"`
}

// Thread represents an active or loaded thread in Codex App Server (#184).
type Thread struct {
	ID            string             `json:"id"`
	ModelIdentity ModelIdentityState `json:"model_identity"`
	CreatedAt     time.Time          `json:"created_at"`
	Status        string             `json:"status"`
	Metadata      map[string]string  `json:"metadata,omitempty"`
}

// ThreadResumeRequest specifies parameters for resuming an existing thread (#184).
type ThreadResumeRequest struct {
	ThreadID                   string            `json:"thread_id"`
	AllowProviderModelFallback bool              `json:"allow_provider_model_fallback"`
	Metadata                   map[string]string `json:"metadata,omitempty"`
}

// TurnStartRequest specifies parameters for starting a turn in a thread (#184).
type TurnStartRequest struct {
	ThreadID                   string            `json:"thread_id"`
	TurnID                     string            `json:"turn_id,omitempty"`
	Prompt                     string            `json:"prompt"`
	ModelID                    string            `json:"model_id,omitempty"`
	AllowProviderModelFallback bool              `json:"allow_provider_model_fallback"`
	InterventionID             string            `json:"intervention_id,omitempty"`
	ChallengeID                string            `json:"challenge_id,omitempty"`
	Metadata                   map[string]string `json:"metadata,omitempty"`
}

// Turn represents an ongoing or completed turn (#184).
type Turn struct {
	ID            string             `json:"id"`
	ThreadID      string             `json:"thread_id"`
	Status        string             `json:"status"`
	ModelIdentity ModelIdentityState `json:"model_identity"`
	StartedAt     time.Time          `json:"started_at"`
	CompletedAt   *time.Time         `json:"completed_at,omitempty"`
}

// AppServerClient is the bounded runtime bridge interface for Codex App Server (#184).
type AppServerClient interface {
	Start(ctx context.Context) error
	StartThread(ctx context.Context, req ThreadStartRequest) (Thread, error)
	ResumeThread(ctx context.Context, req ThreadResumeRequest) (Thread, error)
	StartTurn(ctx context.Context, req TurnStartRequest) (Turn, error)
	InterruptTurn(ctx context.Context, threadID, turnID string) error
	Events() <-chan RuntimeEvent
	ApprovalRequests() <-chan ApprovalRequest
	RespondApproval(ctx context.Context, response ApprovalResponse) error
	ListModels(ctx context.Context) (json.RawMessage, error)
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Close(ctx context.Context) error
}

// CodexAppServerConfig specifies process launch and runtime parameters (#184).
type CodexAppServerConfig struct {
	Executable                 string
	Args                       []string
	WorkDir                    string
	StartupTimeout             time.Duration
	RequestTimeout             time.Duration
	MaxMessageBytes            int
	EventsQueueDepth           int
	ApprovalsQueueDepth        int
	Env                        []string
	ClientName                 string
	ClientVersion              string
	AllowProviderModelFallback bool
}

// DefaultCodexAppServerArgs returns default discrete argv array.
func DefaultCodexAppServerArgs() []string {
	return []string{"app-server"}
}

// CodexAppServerClient implements AppServerClient using stdio JSON-RPC 2.0 transport (#184).
type CodexAppServerClient struct {
	cfg                    CodexAppServerConfig
	cmd                    *exec.Cmd
	plat                   codexProcPlatform
	stdin                  io.WriteCloser
	stdout                 io.ReadCloser
	mu                     sync.Mutex
	writeMu                sync.Mutex
	nextID                 atomic.Int64
	pending                map[int64]chan jsonRPCMessage
	events                 chan RuntimeEvent
	approvalRequests       chan ApprovalRequest
	serverPendingApprovals map[string]int64
	started                atomic.Bool
	initOK                 atomic.Bool
	closed                 atomic.Bool
	readerDone             chan struct{}
	terminalErr            error
	audit                  []string
	seqCounter             atomic.Int64
	serverInfo             map[string]any
	serverCaps             map[string]any
	isTestTransport        bool
}

// Compile-time check that CodexAppServerClient implements AppServerClient.
var _ AppServerClient = (*CodexAppServerClient)(nil)

// NewCodexAppServerClient creates a new unstarted CodexAppServerClient.
func NewCodexAppServerClient(cfg CodexAppServerConfig) *CodexAppServerClient {
	if len(cfg.Args) == 0 {
		cfg.Args = DefaultCodexAppServerArgs()
	}
	if cfg.StartupTimeout <= 0 {
		cfg.StartupTimeout = DefaultCodexAppServerStartup
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultCodexAppServerRequestTimeout
	}
	if cfg.MaxMessageBytes <= 0 {
		cfg.MaxMessageBytes = MaxCodexAppServerMessageBytes
	}
	if cfg.EventsQueueDepth <= 0 {
		cfg.EventsQueueDepth = DefaultCodexAppServerEventsQueue
	}
	if cfg.ApprovalsQueueDepth <= 0 {
		cfg.ApprovalsQueueDepth = DefaultCodexAppServerApprovalsQueue
	}
	if cfg.ClientName == "" {
		cfg.ClientName = "reinframe"
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = "1.0.0"
	}

	return &CodexAppServerClient{
		cfg:                    cfg,
		pending:                make(map[int64]chan jsonRPCMessage),
		events:                 make(chan RuntimeEvent, cfg.EventsQueueDepth),
		approvalRequests:       make(chan ApprovalRequest, cfg.ApprovalsQueueDepth),
		serverPendingApprovals: make(map[string]int64),
		readerDone:             make(chan struct{}),
	}
}

// NewCodexAppServerClientForTest creates a CodexAppServerClient wired directly to io pipes for tests.
func NewCodexAppServerClientForTest(serverIn io.WriteCloser, serverOut io.ReadCloser, cfg CodexAppServerConfig) *CodexAppServerClient {
	c := NewCodexAppServerClient(cfg)
	c.stdin = serverIn
	c.stdout = serverOut
	c.isTestTransport = true
	return c
}

// Start launches the codex app-server binary and executes initialize -> initialized handshake (#184).
func (c *CodexAppServerClient) Start(ctx context.Context) error {
	if c.closed.Load() {
		return NewAppServerError(ErrCodeRuntimeCrashed, "client is already closed")
	}
	if c.started.Load() {
		return nil
	}

	if !c.isTestTransport {
		if c.cfg.Executable == "" {
			return NewAppServerError(ErrCodeBinaryNotFound, "codex executable path is empty")
		}
		if strings.ContainsAny(c.cfg.Executable, "&|;$`\n\r><()\"'") {
			return NewAppServerError(ErrCodeBinaryNotFound, "codex executable contains illegal shell characters")
		}
		for _, a := range c.cfg.Args {
			if strings.ContainsAny(a, ";|&") {
				return NewAppServerError(ErrCodeBinaryNotFound, "codex args contain illegal shell characters")
			}
		}

		resolvedPath, err := exec.LookPath(c.cfg.Executable)
		if err != nil {
			return NewAppServerError(ErrCodeBinaryNotFound, fmt.Sprintf("executable %q not found on PATH: %v", c.cfg.Executable, err))
		}
		absPath, err := filepath.Abs(resolvedPath)
		if err == nil {
			resolvedPath = absPath
		}

		cmd := exec.Command(resolvedPath, c.cfg.Args...)
		if c.cfg.WorkDir != "" {
			cmd.Dir = c.cfg.WorkDir
		}
		if len(c.cfg.Env) > 0 {
			cmd.Env = append(os.Environ(), c.cfg.Env...)
		}
		cmd.Stderr = io.Discard

		configureCodexProcess(cmd)
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return NewAppServerError(ErrCodeRuntimeCrashed, "failed to create stdin pipe", err)
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			_ = stdin.Close()
			return NewAppServerError(ErrCodeRuntimeCrashed, "failed to create stdout pipe", err)
		}

		if err := cmd.Start(); err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			return NewAppServerError(ErrCodeBinaryNotFound, "failed to start app-server process", err)
		}

		plat, err := attachCodexProcess(cmd)
		if err != nil {
			_ = stdin.Close()
			_ = stdout.Close()
			_ = signalCodexProcess(cmd, &plat, true)
			_, _ = cmd.Process.Wait()
			releaseCodexProcess(&plat)
			return NewAppServerError(ErrCodeRuntimeCrashed, "failed to attach process tree", err)
		}

		c.cmd = cmd
		c.plat = plat
		c.stdin = stdin
		c.stdout = stdout
	}

	go c.readLoop()

	// Initialize handshake
	initCtx := ctx
	if initCtx == nil {
		initCtx = context.Background()
	}
	startupTimeout := c.cfg.StartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = DefaultCodexAppServerStartup
	}
	var cancel context.CancelFunc
	initCtx, cancel = context.WithTimeout(initCtx, startupTimeout)
	defer cancel()

	if err := c.handshake(initCtx); err != nil {
		_ = c.Close(context.Background())
		return err
	}

	c.started.Store(true)
	c.initOK.Store(true)
	return nil
}

func (c *CodexAppServerClient) handshake(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": CodexAppServerProtocolVersion,
		"clientInfo": map[string]any{
			"name":    c.cfg.ClientName,
			"version": c.cfg.ClientVersion,
		},
		"capabilities": map[string]any{
			"approval":  true,
			"streaming": true,
		},
	}

	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || (ctx.Err() == context.DeadlineExceeded) {
			return NewAppServerError(ErrCodeStartupTimeout, "app-server initialize timed out", err)
		}
		return err
	}

	var res struct {
		ServerInfo      map[string]any `json:"serverInfo"`
		ProtocolVersion any            `json:"protocolVersion"`
		Capabilities    map[string]any `json:"capabilities"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return NewAppServerError(ErrCodeProtocolMalformed, "failed to parse initialize response", err)
	}

	c.mu.Lock()
	c.serverInfo = res.ServerInfo
	c.serverCaps = res.Capabilities
	c.mu.Unlock()

	// Send initialized notification
	notif := map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]any{},
	}
	notifRaw, _ := json.Marshal(notif)
	c.writeMu.Lock()
	if c.stdin != nil {
		_, _ = c.stdin.Write(append(notifRaw, '\n'))
	}
	c.writeMu.Unlock()

	return nil
}

// StartThread starts a new session thread in Codex App Server (#184).
func (c *CodexAppServerClient) StartThread(ctx context.Context, req ThreadStartRequest) (Thread, error) {
	if !c.initOK.Load() {
		return Thread{}, NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}

	params := map[string]any{
		"model":         req.ModelID,
		"workDir":       req.WorkDir,
		"scope":         req.Scope,
		"allowFallback": req.AllowProviderModelFallback,
		"metadata":      req.Metadata,
	}
	if req.ThreadID != "" {
		params["threadId"] = req.ThreadID
	}
	if len(req.Env) > 0 {
		params["env"] = req.Env
	}

	raw, err := c.call(ctx, "thread/start", params)
	if err != nil {
		return Thread{}, err
	}

	var res struct {
		ThreadID  string            `json:"threadId"`
		Model     string            `json:"model"`
		Status    string            `json:"status"`
		CreatedAt string            `json:"createdAt"`
		Metadata  map[string]string `json:"metadata"`
		Thread    *struct {
			ID        string            `json:"id"`
			ThreadID  string            `json:"threadId"`
			Model     string            `json:"model"`
			Status    string            `json:"status"`
			CreatedAt string            `json:"createdAt"`
			Metadata  map[string]string `json:"metadata"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return Thread{}, NewAppServerError(ErrCodeProtocolMalformed, "failed to parse thread/start response", err)
	}

	reportedModel := res.Model
	reportedID := res.ThreadID
	reportedStatus := res.Status
	reportedCreatedAt := res.CreatedAt
	reportedMetadata := res.Metadata

	if res.Thread != nil {
		if res.Thread.Model != "" {
			reportedModel = res.Thread.Model
		}
		if res.Thread.ID != "" {
			reportedID = res.Thread.ID
		} else if res.Thread.ThreadID != "" {
			reportedID = res.Thread.ThreadID
		}
		if res.Thread.Status != "" {
			reportedStatus = res.Thread.Status
		}
		if res.Thread.CreatedAt != "" {
			reportedCreatedAt = res.Thread.CreatedAt
		}
		if len(res.Thread.Metadata) > 0 {
			reportedMetadata = res.Thread.Metadata
		}
	}

	ident, err := VerifyModelIdentity(req.ModelID, reportedModel, req.AllowProviderModelFallback)
	if err != nil {
		return Thread{}, err
	}

	var createdAt time.Time
	if reportedCreatedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, reportedCreatedAt); parseErr == nil {
			createdAt = t
		} else if t, parseErr := time.Parse(time.RFC3339, reportedCreatedAt); parseErr == nil {
			createdAt = t
		}
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}

	status := reportedStatus
	if status == "" {
		status = "active"
	}

	threadID := reportedID
	if threadID == "" && req.ThreadID != "" {
		threadID = req.ThreadID
	}

	return Thread{
		ID:            threadID,
		ModelIdentity: ident,
		CreatedAt:     createdAt,
		Status:        status,
		Metadata:      reportedMetadata,
	}, nil
}

// ResumeThread resumes an existing session thread in Codex App Server (#184).
func (c *CodexAppServerClient) ResumeThread(ctx context.Context, req ThreadResumeRequest) (Thread, error) {
	if !c.initOK.Load() {
		return Thread{}, NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	if req.ThreadID == "" {
		return Thread{}, NewAppServerError(ErrCodeProtocolMalformed, "threadId is required for thread/resume")
	}

	params := map[string]any{
		"threadId":      req.ThreadID,
		"allowFallback": req.AllowProviderModelFallback,
		"metadata":      req.Metadata,
	}

	raw, err := c.call(ctx, "thread/resume", params)
	if err != nil {
		return Thread{}, err
	}

	var res struct {
		ThreadID string            `json:"threadId"`
		Model    string            `json:"model"`
		Status   string            `json:"status"`
		Metadata map[string]string `json:"metadata"`
		Thread   *struct {
			ID       string            `json:"id"`
			ThreadID string            `json:"threadId"`
			Model    string            `json:"model"`
			Status   string            `json:"status"`
			Metadata map[string]string `json:"metadata"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return Thread{}, NewAppServerError(ErrCodeProtocolMalformed, "failed to parse thread/resume response", err)
	}

	reportedModel := res.Model
	reportedStatus := res.Status
	reportedMetadata := res.Metadata

	if res.Thread != nil {
		if res.Thread.Model != "" {
			reportedModel = res.Thread.Model
		}
		if res.Thread.Status != "" {
			reportedStatus = res.Thread.Status
		}
		if len(res.Thread.Metadata) > 0 {
			reportedMetadata = res.Thread.Metadata
		}
	}

	ident, err := VerifyModelIdentity("", reportedModel, req.AllowProviderModelFallback)
	if err != nil {
		return Thread{}, err
	}

	status := reportedStatus
	if status == "" {
		status = "active"
	}

	return Thread{
		ID:            req.ThreadID,
		ModelIdentity: ident,
		CreatedAt:     time.Now().UTC(),
		Status:        status,
		Metadata:      reportedMetadata,
	}, nil
}

// StartTurn initiates a turn in an active thread (#184).
func (c *CodexAppServerClient) StartTurn(ctx context.Context, req TurnStartRequest) (Turn, error) {
	if !c.initOK.Load() {
		return Turn{}, NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	if req.ThreadID == "" {
		return Turn{}, NewAppServerError(ErrCodeProtocolMalformed, "threadId is required for turn/start")
	}
	if req.Prompt == "" {
		return Turn{}, NewAppServerError(ErrCodeProtocolMalformed, "prompt is required for turn/start")
	}

	params := map[string]any{
		"threadId": req.ThreadID,
		"prompt":   req.Prompt,
		"input": []map[string]any{
			{
				"type": "text",
				"text": req.Prompt,
			},
		},
		"model":          req.ModelID,
		"allowFallback":  req.AllowProviderModelFallback,
		"interventionId": req.InterventionID,
		"challengeId":    req.ChallengeID,
		"metadata":       req.Metadata,
	}
	if req.TurnID != "" {
		params["turnId"] = req.TurnID
	}

	raw, err := c.call(ctx, "turn/start", params)
	if err != nil {
		return Turn{}, err
	}

	var res struct {
		TurnID    string `json:"turnId"`
		ThreadID  string `json:"threadId"`
		Status    string `json:"status"`
		Model     string `json:"model"`
		StartedAt string `json:"startedAt"`
		Turn      *struct {
			ID        string `json:"id"`
			TurnID    string `json:"turnId"`
			ThreadID  string `json:"threadId"`
			Status    string `json:"status"`
			Model     string `json:"model"`
			StartedAt string `json:"startedAt"`
		} `json:"turn"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return Turn{}, NewAppServerError(ErrCodeProtocolMalformed, "failed to parse turn/start response", err)
	}

	reportedModel := res.Model
	reportedTurnID := res.TurnID
	reportedThreadID := res.ThreadID
	reportedStatus := res.Status
	reportedStartedAt := res.StartedAt

	if res.Turn != nil {
		if res.Turn.Model != "" {
			reportedModel = res.Turn.Model
		}
		if res.Turn.ID != "" {
			reportedTurnID = res.Turn.ID
		} else if res.Turn.TurnID != "" {
			reportedTurnID = res.Turn.TurnID
		}
		if res.Turn.ThreadID != "" {
			reportedThreadID = res.Turn.ThreadID
		}
		if res.Turn.Status != "" {
			reportedStatus = res.Turn.Status
		}
		if res.Turn.StartedAt != "" {
			reportedStartedAt = res.Turn.StartedAt
		}
	}

	ident, err := VerifyModelIdentity(req.ModelID, reportedModel, req.AllowProviderModelFallback)
	if err != nil {
		return Turn{}, err
	}

	var startedAt time.Time
	if reportedStartedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, reportedStartedAt); parseErr == nil {
			startedAt = t
		} else if t, parseErr := time.Parse(time.RFC3339, reportedStartedAt); parseErr == nil {
			startedAt = t
		}
	}
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	turnID := reportedTurnID
	if turnID == "" && req.TurnID != "" {
		turnID = req.TurnID
	}

	status := reportedStatus
	if status == "" {
		status = "in_progress"
	}

	threadID := reportedThreadID
	if threadID == "" {
		threadID = req.ThreadID
	}

	return Turn{
		ID:            turnID,
		ThreadID:      threadID,
		Status:        status,
		ModelIdentity: ident,
		StartedAt:     startedAt,
	}, nil
}

// InterruptTurn sends an interruption signal for an ongoing turn (#184).
func (c *CodexAppServerClient) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	if !c.initOK.Load() {
		return NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	if threadID == "" {
		return NewAppServerError(ErrCodeProtocolMalformed, "threadId is required for turn/interrupt")
	}

	params := map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	}

	_, err := c.call(ctx, "turn/interrupt", params)
	return err
}

// Events returns the receive-only channel of runtime events (#184).
func (c *CodexAppServerClient) Events() <-chan RuntimeEvent {
	return c.events
}

// ApprovalRequests returns the receive-only channel of intercepted approval requests (#184).
func (c *CodexAppServerClient) ApprovalRequests() <-chan ApprovalRequest {
	return c.approvalRequests
}

// RespondApproval replies to an intercepted approval request (#184).
func (c *CodexAppServerClient) RespondApproval(ctx context.Context, response ApprovalResponse) error {
	if !c.initOK.Load() {
		return NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	if response.RequestID == "" {
		return NewAppServerError(ErrCodeProtocolMalformed, "requestId required in approval response")
	}

	c.mu.Lock()
	serverRPCID, hasRPC := c.serverPendingApprovals[response.RequestID]
	if hasRPC {
		delete(c.serverPendingApprovals, response.RequestID)
	}
	c.mu.Unlock()

	if hasRPC {
		respMsg := map[string]any{
			"jsonrpc": "2.0",
			"id":      serverRPCID,
			"result": map[string]any{
				"decision":   response.Decision,
				"reasonCode": response.ReasonCode,
				"detail":     response.Detail,
			},
		}
		raw, err := json.Marshal(respMsg)
		if err != nil {
			return NewAppServerError(ErrCodeProtocolMalformed, "failed to marshal approval response", err)
		}
		c.writeMu.Lock()
		if c.stdin != nil {
			_, _ = c.stdin.Write(append(raw, '\n'))
		}
		c.writeMu.Unlock()
		return nil
	}

	// If not server-initiated RPC or notification style, invoke approval/respond
	params := map[string]any{
		"requestId":  response.RequestID,
		"decision":   response.Decision,
		"reasonCode": response.ReasonCode,
		"detail":     response.Detail,
	}
	_, err := c.call(ctx, "approval/respond", params)
	return err
}

// ListModels queries the model/list JSON-RPC method from Codex App Server (#185).
func (c *CodexAppServerClient) ListModels(ctx context.Context) (json.RawMessage, error) {
	if !c.initOK.Load() {
		return nil, NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	return c.call(ctx, "model/list", map[string]any{})
}

// Call executes a JSON-RPC 2.0 method call against Codex App Server.
func (c *CodexAppServerClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !c.initOK.Load() {
		return nil, NewAppServerError(ErrCodeRuntimeCrashed, "client not initialized")
	}
	return c.call(ctx, method, params)
}

// Close gracefully terminates the App Server process tree and frees all resources (#184).
func (c *CodexAppServerClient) Close(ctx context.Context) error {
	if !c.closed.CompareAndSwap(false, true) {
		return nil
	}

	c.writeMu.Lock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	c.writeMu.Unlock()

	select {
	case <-c.readerDone:
	case <-time.After(codexAppServerGracefulWait):
	}

	var shutdownErr error
	if c.cmd != nil && c.cmd.Process != nil {
		_ = signalCodexProcess(c.cmd, &c.plat, false)
		done := make(chan error, 1)
		go func() { done <- c.cmd.Wait() }()

		select {
		case <-done:
		case <-time.After(codexAppServerForceWait):
			_ = signalCodexProcess(c.cmd, &c.plat, true)
			select {
			case <-done:
			case <-time.After(codexAppServerForceWait):
				shutdownErr = NewAppServerError(ErrCodeShutdownFailed, "process tree termination timed out")
			}
		}
		releaseCodexProcess(&c.plat)
	}

	c.failAllPending(ErrRuntimeCrashed)
	return shutdownErr
}

func (c *CodexAppServerClient) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if c.closed.Load() {
		return nil, NewAppServerError(ErrCodeRuntimeCrashed, "client is closed")
	}

	if ctx == nil {
		ctx = context.Background()
	}
	reqTimeout := c.cfg.RequestTimeout
	if reqTimeout <= 0 {
		reqTimeout = DefaultCodexAppServerRequestTimeout
	}

	var cancel context.CancelFunc
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		ctx, cancel = context.WithTimeout(ctx, reqTimeout)
		defer cancel()
	}

	id := c.nextID.Add(1)
	ch := make(chan jsonRPCMessage, 1)

	c.mu.Lock()
	if c.terminalErr != nil {
		err := c.terminalErr
		c.mu.Unlock()
		return nil, err
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
		return nil, NewAppServerError(ErrCodeProtocolMalformed, "failed to marshal request", err)
	}
	if len(raw) > c.cfg.MaxMessageBytes {
		return nil, NewAppServerError(ErrCodeProtocolOversized, fmt.Sprintf("request size %d exceeds max %d", len(raw), c.cfg.MaxMessageBytes))
	}

	c.writeMu.Lock()
	if c.stdin == nil {
		c.writeMu.Unlock()
		return nil, NewAppServerError(ErrCodeRuntimeCrashed, "stdin is closed")
	}
	_, err = c.stdin.Write(append(raw, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		return nil, NewAppServerError(ErrCodeRuntimeCrashed, "failed to write to stdin", err)
	}

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, NewAppServerError(ErrCodeRequestTimeout, fmt.Sprintf("request %s (id=%d) timed out", method, id), ctx.Err())
		}
		return nil, ctx.Err()
	case <-c.readerDone:
		c.mu.Lock()
		term := c.terminalErr
		c.mu.Unlock()
		if term == nil {
			term = ErrRuntimeCrashed
		}
		return nil, term
	case resp := <-ch:
		if resp.Error != nil {
			return nil, mapServerRPCError(resp.Error)
		}
		return resp.Result, nil
	}
}

func (c *CodexAppServerClient) readLoop() {
	defer func() {
		c.failAllPending(ErrRuntimeCrashed)
		close(c.readerDone)
	}()

	bufSize := c.cfg.MaxMessageBytes + 1
	if bufSize <= 1 {
		bufSize = MaxCodexAppServerMessageBytes + 1
	}
	rd := bufio.NewReaderSize(c.stdout, bufSize)

	for {
		line, isPrefix, err := rd.ReadLine()
		if err != nil {
			break
		}
		if isPrefix {
			// Line was longer than buffer, discard remainder of this line
			c.noteAudit("drop_oversized_line")
			for isPrefix && err == nil {
				_, isPrefix, err = rd.ReadLine()
			}
			continue
		}
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

		// Server notification (method present, id nil)
		if msg.Method != "" && msg.ID == nil {
			c.handleServerNotification(msg)
			continue
		}

		// Server-initiated request (method present, id present)
		if msg.Method != "" && msg.ID != nil {
			c.handleServerRequest(msg)
			continue
		}

		// Response to client request (id present, method empty)
		if msg.ID != nil {
			id := *msg.ID
			c.mu.Lock()
			ch := c.pending[id]
			c.mu.Unlock()
			if ch != nil {
				select {
				case ch <- msg:
				default:
					c.noteAudit("pending_channel_full")
				}
			} else {
				c.noteAudit("unknown_response_id")
			}
		}
	}
}

func (c *CodexAppServerClient) handleServerNotification(msg jsonRPCMessage) {
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params == nil {
		params = map[string]any{}
	}

	switch msg.Method {
	case "event", "runtime/event", "turn/event", "item/delta", "item/started", "item/finished", "turn/started", "turn/finished":
		ev := parseRuntimeEvent(msg.Method, params, msg.Params, c.seqCounter.Add(1))
		select {
		case c.events <- ev:
		default:
			c.noteAudit("events_queue_overflow")
		}
	case "approval/request", "item/approval", "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/mcpToolCall/requestApproval":
		req := parseApprovalRequest(msg.Method, params, msg.Params)
		select {
		case c.approvalRequests <- req:
		default:
			c.noteAudit("approvals_queue_overflow")
		}
	}
}

func (c *CodexAppServerClient) handleServerRequest(msg jsonRPCMessage) {
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params == nil {
		params = map[string]any{}
	}

	switch msg.Method {
	case "approval/request", "item/approval", "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/mcpToolCall/requestApproval":
		req := parseApprovalRequest(msg.Method, params, msg.Params)
		c.mu.Lock()
		c.serverPendingApprovals[req.RequestID] = *msg.ID
		c.mu.Unlock()

		select {
		case c.approvalRequests <- req:
		default:
			c.noteAudit("approvals_queue_overflow")
			c.mu.Lock()
			delete(c.serverPendingApprovals, req.RequestID)
			c.mu.Unlock()

			// Fail-closed immediate JSON-RPC deny error response to unblock server
			errResp := map[string]any{
				"jsonrpc": "2.0",
				"id":      *msg.ID,
				"error": map[string]any{
					"code":    -32000,
					"message": "approval queue overflow; rejected fail-closed",
				},
			}
			raw, _ := json.Marshal(errResp)
			c.writeMu.Lock()
			if c.stdin != nil {
				_, _ = c.stdin.Write(append(raw, '\n'))
			}
			c.writeMu.Unlock()
		}
	default:
		// Method not found response
		errResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      *msg.ID,
			"error": map[string]any{
				"code":    -32601,
				"message": "Method not found",
			},
		}
		raw, _ := json.Marshal(errResp)
		c.writeMu.Lock()
		if c.stdin != nil {
			_, _ = c.stdin.Write(append(raw, '\n'))
		}
		c.writeMu.Unlock()
	}
}

func parseRuntimeEvent(method string, params map[string]any, rawParams json.RawMessage, seq int64) RuntimeEvent {
	evType, _ := params["type"].(string)
	if evType == "" {
		evType = method
	}
	threadID, _ := params["threadId"].(string)
	if threadID == "" {
		threadID, _ = params["thread_id"].(string)
	}
	turnID, _ := params["turnId"].(string)
	if turnID == "" {
		turnID, _ = params["turn_id"].(string)
	}
	itemID, _ := params["itemId"].(string)
	if itemID == "" {
		itemID, _ = params["item_id"].(string)
	}

	// Check if nested item object exists (App Server item events)
	if itemMap, ok := params["item"].(map[string]any); ok {
		if itID, ok := itemMap["id"].(string); ok && itID != "" {
			itemID = itID
		}
		if itType, ok := itemMap["type"].(string); ok && itType != "" {
			switch strings.ToLower(itType) {
			case "commandexecution", "command_execution":
				evType = "item/commandExecution"
			case "filechange", "file_change":
				evType = "item/fileChange"
			case "mcptoolcall", "mcp_tool_call":
				evType = "item/mcpToolCall"
			default:
				evType = "item/" + itType
			}
		}
	}

	return RuntimeEvent{
		EventID:     fmt.Sprintf("ev-%d", seq),
		Type:        evType,
		SequenceNum: seq,
		ThreadID:    threadID,
		TurnID:      turnID,
		ItemID:      itemID,
		Timestamp:   time.Now().UTC(),
		Payload:     rawParams,
	}
}

func parseApprovalRequest(method string, params map[string]any, rawParams json.RawMessage) ApprovalRequest {
	reqID, _ := params["requestId"].(string)
	if reqID == "" {
		reqID, _ = params["request_id"].(string)
	}
	if reqID == "" {
		reqID = fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	threadID, _ := params["threadId"].(string)
	if threadID == "" {
		threadID, _ = params["thread_id"].(string)
	}
	turnID, _ := params["turnId"].(string)
	if turnID == "" {
		turnID, _ = params["turn_id"].(string)
	}

	var kind ApprovalKind
	var toolName, command, filePath string

	// Check if nested item exists in params (App Server protocol shape)
	if itemMap, ok := params["item"].(map[string]any); ok {
		itemType, _ := itemMap["type"].(string)
		switch strings.ToLower(itemType) {
		case "commandexecution", "command_execution", "command":
			kind = ApprovalKindCommand
		case "filechange", "file_change", "file":
			kind = ApprovalKindFile
		default:
			kind = ApprovalKindTool
		}
		command, _ = itemMap["command"].(string)
		filePath, _ = itemMap["path"].(string)
		if filePath == "" {
			filePath, _ = itemMap["filePath"].(string)
		}
		toolName, _ = itemMap["tool"].(string)
		if toolName == "" {
			toolName, _ = itemMap["toolName"].(string)
		}
	} else {
		// Infer from method name if specific
		switch method {
		case "item/commandExecution/requestApproval":
			kind = ApprovalKindCommand
		case "item/fileChange/requestApproval":
			kind = ApprovalKindFile
		case "item/mcpToolCall/requestApproval":
			kind = ApprovalKindTool
		default:
			kindStr, _ := params["kind"].(string)
			switch strings.ToLower(kindStr) {
			case "command", "cmd":
				kind = ApprovalKindCommand
			case "file", "path":
				kind = ApprovalKindFile
			default:
				kind = ApprovalKindTool
			}
		}

		toolName, _ = params["toolName"].(string)
		if toolName == "" {
			toolName, _ = params["tool_name"].(string)
		}
		command, _ = params["command"].(string)
		filePath, _ = params["filePath"].(string)
		if filePath == "" {
			filePath, _ = params["file_path"].(string)
		}
	}

	return ApprovalRequest{
		RequestID: reqID,
		ThreadID:  threadID,
		TurnID:    turnID,
		Kind:      kind,
		ToolName:  toolName,
		Command:   command,
		FilePath:  filePath,
		Detail:    rawParams,
		CreatedAt: time.Now().UTC(),
	}
}

func (c *CodexAppServerClient) failAllPending(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.terminalErr == nil {
		c.terminalErr = err
	}
	for id, ch := range c.pending {
		delete(c.pending, id)
		select {
		case ch <- jsonRPCMessage{Error: &jsonRPCError{Code: -32000, Message: err.Error()}}:
		default:
		}
	}
}

func (c *CodexAppServerClient) noteAudit(entry string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.audit) >= 64 {
		c.audit = c.audit[1:]
	}
	c.audit = append(c.audit, entry)
}

func mapServerRPCError(rpcErr *jsonRPCError) *AppServerError {
	if rpcErr == nil {
		return ErrProtocolMalformed
	}
	msg := strings.TrimSpace(rpcErr.Message)
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "auth required") || strings.Contains(lower, "unauthenticated") || strings.Contains(lower, "not logged in") || rpcErr.Code == 401:
		return NewAppServerError(ErrCodeAuthRequired, msg)
	case strings.Contains(lower, "expired") || strings.Contains(lower, "token expired") || strings.Contains(lower, "session expired"):
		return NewAppServerError(ErrCodeAuthExpired, msg)
	case strings.Contains(lower, "unsupported version") || strings.Contains(lower, "version mismatch"):
		return NewAppServerError(ErrCodeUnsupportedVersion, msg)
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "quota exceeded") || strings.Contains(lower, "too many requests") || rpcErr.Code == 429:
		return NewAppServerError(ErrCodeRateLimited, msg)
	case strings.Contains(lower, "model unavailable") || strings.Contains(lower, "model not found") || strings.Contains(lower, "unknown model"):
		return NewAppServerError(ErrCodeModelUnavailable, msg)
	case strings.Contains(lower, "model identity unproven") || strings.Contains(lower, "identity unproven"):
		return NewAppServerError(ErrCodeModelIdentityUnproven, msg)
	case strings.Contains(lower, "approval unsupported") || strings.Contains(lower, "unsupported approval"):
		return NewAppServerError(ErrCodeApprovalUnsupported, msg)
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return NewAppServerError(ErrCodeRequestTimeout, msg)
	case strings.Contains(lower, "oversized") || strings.Contains(lower, "message too large"):
		return NewAppServerError(ErrCodeProtocolOversized, msg)
	case strings.Contains(lower, "runtime_crashed") || strings.Contains(lower, "process crashed") || strings.Contains(lower, "crashed"):
		return NewAppServerError(ErrCodeRuntimeCrashed, msg)
	default:
		return NewAppServerError(ErrCodeProtocolMalformed, fmt.Sprintf("server error (%d): %s", rpcErr.Code, msg))
	}
}
