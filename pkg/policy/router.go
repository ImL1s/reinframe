package policy

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// LaneID identifies a discrete execution transport and credential lane (#186).
type LaneID string

const (
	// LaneCodexSubscription is Lane A: Delegated ChatGPT subscription runtime via child codex process.
	LaneCodexSubscription LaneID = "codex_subscription_runtime"

	// LaneOpenAIResponses is Lane B: Direct OpenAI Responses API via environment API key (/v1/responses).
	LaneOpenAIResponses LaneID = "openai_responses_api"

	// LaneA is an alias for LaneCodexSubscription.
	LaneA = LaneCodexSubscription

	// LaneB is an alias for LaneOpenAIResponses.
	LaneB = LaneOpenAIResponses
)

// Valid reports whether the LaneID is one of the closed enum values.
func (l LaneID) Valid() bool {
	switch l {
	case LaneCodexSubscription, LaneOpenAIResponses:
		return true
	default:
		return false
	}
}

// IsSubscription reports whether the lane is the Codex subscription runtime lane.
func (l LaneID) IsSubscription() bool {
	return l == LaneCodexSubscription
}

// IsAPI reports whether the lane is the OpenAI Responses API lane.
func (l LaneID) IsAPI() bool {
	return l == LaneOpenAIResponses
}

// Role identifies the caller's functional role in the supervision harness (#186).
type Role string

const (
	// RoleAgent represents external agent execution / child process supervision.
	RoleAgent Role = "agent"

	// RoleClassifier represents Reinframe internal Stage-1 tunnel/severity assessment.
	RoleClassifier Role = "classifier"

	// RoleReviewer represents Reinframe optional slow-path reviewer advice.
	RoleReviewer Role = "reviewer"
)

// Valid reports whether the Role is one of the closed enum values.
func (r Role) Valid() bool {
	switch r {
	case RoleAgent, RoleClassifier, RoleReviewer:
		return true
	default:
		return false
	}
}

// ExecutionIntent defines the purpose of the invocation (#186).
type ExecutionIntent string

const (
	// IntentAgentTurn is execution of an agent turn / tool action.
	IntentAgentTurn ExecutionIntent = "agent_turn"

	// IntentClassifierAssessment is Stage-1 tunnel/drift/churn assessment.
	IntentClassifierAssessment ExecutionIntent = "classifier_assessment"

	// IntentReviewerAdvice is slow-path reviewer prompt generation.
	IntentReviewerAdvice ExecutionIntent = "reviewer_advice"

	// IntentDirectAPI is direct API invocation.
	IntentDirectAPI ExecutionIntent = "direct_api"

	// IntentAppServerSession is an interactive or background app-server session.
	IntentAppServerSession ExecutionIntent = "app_server_session"

	// IntentBenchmarkRun is an offline evaluation or challenge benchmark run.
	IntentBenchmarkRun ExecutionIntent = "benchmark_run"
)

// Valid reports whether the ExecutionIntent is valid.
func (i ExecutionIntent) Valid() bool {
	switch i {
	case IntentAgentTurn,
		IntentClassifierAssessment,
		IntentReviewerAdvice,
		IntentDirectAPI,
		IntentAppServerSession,
		IntentBenchmarkRun:
		return true
	default:
		return false
	}
}

// CredentialClass classifies the credential tier expected for a route (#186).
type CredentialClass string

const (
	// CredentialClassSubscription expects ChatGPT subscription credentials owned by child process.
	CredentialClassSubscription CredentialClass = "chatgpt_subscription"

	// CredentialClassAPIKey expects direct API key credentials owned by Reinframe environment.
	CredentialClassAPIKey CredentialClass = "api_key"

	// CredentialClassUnspecified indicates caller did not constrain credential class.
	CredentialClassUnspecified CredentialClass = "unspecified"
)

// Valid reports whether CredentialClass is valid.
func (c CredentialClass) Valid() bool {
	switch c {
	case CredentialClassSubscription, CredentialClassAPIKey, CredentialClassUnspecified:
		return true
	default:
		return false
	}
}

// ToAuthMode converts CredentialClass to protocol.RuntimeAuthMode.
func (c CredentialClass) ToAuthMode() protocol.RuntimeAuthMode {
	switch c {
	case CredentialClassSubscription:
		return protocol.RuntimeAuthModeChatGPTSubscription
	case CredentialClassAPIKey:
		return protocol.RuntimeAuthModeAPIKey
	default:
		return protocol.RuntimeAuthModeUnknown
	}
}

// FallbackPolicy defines whether and how fallback is permitted on route failure (#186).
type FallbackPolicy string

const (
	// FallbackFailClosed strictly forbids any fallback; fail closed on error (default).
	FallbackFailClosed FallbackPolicy = "fail_closed"

	// FallbackSameLaneOnly permits fallback only within the same lane (e.g. alternate model in same catalog).
	FallbackSameLaneOnly FallbackPolicy = "same_lane_only"

	// FallbackCrossLaneProhibited explicitly documents cross-lane prohibition across billing boundaries.
	FallbackCrossLaneProhibited FallbackPolicy = "cross_lane_prohibited"
)

// Valid reports whether FallbackPolicy is valid.
func (p FallbackPolicy) Valid() bool {
	switch p {
	case FallbackFailClosed, FallbackSameLaneOnly, FallbackCrossLaneProhibited, "":
		return true
	default:
		return false
	}
}

// Closed billing boundary constants.
const (
	BillingBoundarySubscription = "chatgpt_subscription_quota"
	BillingBoundaryAPITokens     = "openai_api_tokens"
)

// Closed default endpoints.
const (
	DefaultSubscriptionEndpoint = "codex_process"
	DefaultAPIResponsesEndpoint  = "https://api.openai.com/v1/responses"
	DefaultAPIResponsesPath      = "/v1/responses"
)

// Fail-closed typed routing errors (#186).
var (
	// ErrNoRouteAvailable is returned when no enabled or valid route satisfies the request.
	ErrNoRouteAvailable = errors.New("policy_router: no route available for request")

	// ErrCrossLaneFallbackProhibited is returned when cross-lane fallback is attempted across billing boundaries.
	ErrCrossLaneFallbackProhibited = errors.New("policy_router: cross-lane fallback is prohibited across billing boundaries")

	// ErrLaneAuthMismatch is returned when credential class or auth mode does not match the lane.
	ErrLaneAuthMismatch = errors.New("policy_router: credential class or auth mode mismatch for selected lane")

	// ErrCapabilityNotMet is returned when required capabilities are not satisfied by lane or model.
	ErrCapabilityNotMet = errors.New("policy_router: required capabilities not satisfied by lane or model")

	// ErrModelUnavailable is returned when requested model does not exist or meet support state.
	ErrModelUnavailable = errors.New("policy_router: requested model is unavailable or does not meet required support state")

	// ErrModelSubstitutionProhibited is returned when silent model substitution is attempted without opt-in.
	ErrModelSubstitutionProhibited = errors.New("policy_router: silent model substitution is prohibited")

	// ErrClassifierOAuthProhibited is returned when a classifier request attempts to route to subscription OAuth.
	ErrClassifierOAuthProhibited = errors.New("policy_router: classifier requests cannot route to ChatGPT subscription OAuth")

	// ErrAgentAPIKeyProhibited is returned when agent execution attempts to route to API key without explicit configuration.
	ErrAgentAPIKeyProhibited = errors.New("policy_router: agent requests cannot route to API key lane without explicit configuration")
)

// ModelSelection specifies exact model requirements for route resolution (#186).
type ModelSelection struct {
	// ModelID is the exact model identifier (e.g. "gpt-5.3-codex", "gpt-5.3-codex-spark").
	ModelID string `json:"model_id"`

	// AllowSubstitution forbids silent fallback when false (default false: zero silent substitution).
	AllowSubstitution bool `json:"allow_substitution"`

	// RequiredSupportState specifies minimum lifecycle qualification (e.g. ModelSupportStateSelectable).
	RequiredSupportState protocol.ModelSupportState `json:"required_support_state,omitempty"`

	// ReasoningEffort optionally specifies reasoning effort ("low", "medium", "high").
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// FallbackModelID is an optional alternate model within the SAME lane if AllowSubstitution is true.
	FallbackModelID string `json:"fallback_model_id,omitempty"`
}

// RouteRequest defines the input parameters for dual-lane route resolution (#186).
type RouteRequest struct {
	// Intent is the execution intent (e.g. IntentAgentTurn, IntentClassifierAssessment).
	Intent ExecutionIntent `json:"intent"`

	// Role is the caller's functional role (RoleAgent, RoleClassifier, RoleReviewer).
	Role Role `json:"role"`

	// CredentialClass classifies the expected credential tier.
	CredentialClass CredentialClass `json:"credential_class,omitempty"`

	// RequiredCapabilities is the uint64 bitmask of required protocol capability flags.
	RequiredCapabilities uint64 `json:"required_capabilities,omitempty"`

	// ModelSelection specifies model constraints and substitution posture.
	ModelSelection ModelSelection `json:"model_selection"`

	// FallbackPolicy controls fallback behavior on failure.
	FallbackPolicy FallbackPolicy `json:"fallback_policy,omitempty"`

	// TargetLane allows explicit lane targeting (optional).
	TargetLane LaneID `json:"target_lane,omitempty"`

	// AllowAgentAPIKey is an explicit opt-in permitting agent execution to route to the API lane.
	AllowAgentAPIKey bool `json:"allow_agent_api_key,omitempty"`

	// Metadata carries optional non-secret context descriptors.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RouteDecision is the resolved, validated routing outcome (#186).
type RouteDecision struct {
	// LaneID identifies the resolved lane (LaneCodexSubscription or LaneOpenAIResponses).
	LaneID LaneID `json:"lane_id"`

	// CredentialOwner identifies credential custodian (codex_process vs reinframe_env).
	CredentialOwner protocol.CredentialOwner `json:"credential_owner"`

	// AuthMode is the active runtime authentication mode.
	AuthMode protocol.RuntimeAuthMode `json:"auth_mode"`

	// Endpoint is the execution target (e.g. "codex_process" or "/v1/responses").
	Endpoint string `json:"endpoint"`

	// SelectedModel is the confirmed model identifier.
	SelectedModel string `json:"selected_model"`

	// RequiredCapabilities is the capability bitmask required by the request.
	RequiredCapabilities uint64 `json:"required_capabilities,omitempty"`

	// ResolvedCapabilities is the capability bitmask supported by the selected lane and model.
	ResolvedCapabilities uint64 `json:"resolved_capabilities"`

	// BillingBoundary documents the closed billing boundary.
	BillingBoundary string `json:"billing_boundary"`

	// Role is the bound role.
	Role Role `json:"role"`

	// Intent is the bound execution intent.
	Intent ExecutionIntent `json:"intent"`

	// FallbackPolicy documents the active fallback policy.
	FallbackPolicy FallbackPolicy `json:"fallback_policy"`

	// FallbackAllowed is true only when same-lane fallback is permissible.
	FallbackAllowed bool `json:"fallback_allowed"`

	// Metadata carries non-secret route metadata.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// LaneSubscriptionConfig configures Lane A (Codex Subscription Runtime).
type LaneSubscriptionConfig struct {
	// Enabled controls whether Lane A is active.
	Enabled bool

	// CredentialOwner specifies custodian (defaults to CredentialOwnerCodexProcess).
	CredentialOwner protocol.CredentialOwner

	// AuthMode specifies auth mode (defaults to RuntimeAuthModeChatGPTSubscription).
	AuthMode protocol.RuntimeAuthMode

	// Endpoint specifies execution endpoint (defaults to DefaultSubscriptionEndpoint).
	Endpoint string

	// Capabilities is the uint64 bitmask of supported capabilities.
	Capabilities uint64

	// Catalog is the active entitlement-aware model catalog snapshot.
	Catalog *protocol.ModelCatalogSnapshot

	// AuthSnapshot is the latest non-secret auth probe snapshot.
	AuthSnapshot *protocol.RuntimeAuthSnapshot

	// BillingBoundary defaults to BillingBoundarySubscription.
	BillingBoundary string
}

// LaneAPIResponsesConfig configures Lane B (OpenAI Responses API).
type LaneAPIResponsesConfig struct {
	// Enabled controls whether Lane B is active.
	Enabled bool

	// CredentialOwner specifies custodian (defaults to CredentialOwnerReinframeEnv).
	CredentialOwner protocol.CredentialOwner

	// AuthMode specifies auth mode (defaults to RuntimeAuthModeAPIKey).
	AuthMode protocol.RuntimeAuthMode

	// BaseURL defaults to "https://api.openai.com".
	BaseURL string

	// Path defaults to DefaultAPIResponsesPath ("/v1/responses").
	Path string

	// APIKeyRef is the env placeholder (e.g. "${OPENAI_API_KEY}").
	APIKeyRef string

	// HasAPIKey indicates whether an API key is available in the environment.
	HasAPIKey bool

	// Capabilities is the uint64 bitmask of supported capabilities.
	Capabilities uint64

	// AvailableModels lists known/configured models for this lane.
	AvailableModels []string

	// Catalog is an optional model catalog snapshot for API models.
	Catalog *protocol.ModelCatalogSnapshot

	// AllowAgentRouting permits agent execution on this lane when true (default false).
	AllowAgentRouting bool

	// BillingBoundary defaults to BillingBoundaryAPITokens.
	BillingBoundary string
}

// RouterConfig configures the DualLaneRouter.
type RouterConfig struct {
	LaneSubscription   LaneSubscriptionConfig
	LaneAPIResponses   LaneAPIResponsesConfig
	StrictBillingBound bool
	DefaultFallback    FallbackPolicy
}

// DualLaneRouter enforces dual-lane isolation and deterministic route resolution (#186).
type DualLaneRouter struct {
	mu      sync.RWMutex
	cfg     RouterConfig
	subLane LaneSubscriptionConfig
	apiLane LaneAPIResponsesConfig
}

// NewDualLaneRouter constructs a new DualLaneRouter with validated defaults.
func NewDualLaneRouter(cfg RouterConfig) *DualLaneRouter {
	sub := cfg.LaneSubscription
	if sub.CredentialOwner == "" {
		sub.CredentialOwner = protocol.CredentialOwnerCodexProcess
	}
	if sub.AuthMode == "" {
		sub.AuthMode = protocol.RuntimeAuthModeChatGPTSubscription
	}
	if sub.Endpoint == "" {
		sub.Endpoint = DefaultSubscriptionEndpoint
	}
	if sub.BillingBoundary == "" {
		sub.BillingBoundary = BillingBoundarySubscription
	}

	api := cfg.LaneAPIResponses
	if api.CredentialOwner == "" {
		api.CredentialOwner = protocol.CredentialOwnerReinframeEnv
	}
	if api.AuthMode == "" {
		api.AuthMode = protocol.RuntimeAuthModeAPIKey
	}
	if api.BaseURL == "" {
		api.BaseURL = "https://api.openai.com"
	}
	if api.Path == "" {
		api.Path = DefaultAPIResponsesPath
	}
	if api.BillingBoundary == "" {
		api.BillingBoundary = BillingBoundaryAPITokens
	}

	return &DualLaneRouter{
		cfg:     cfg,
		subLane: sub,
		apiLane: api,
	}
}

// ResolveRoute resolves an incoming RouteRequest to a concrete RouteDecision under strict isolation rules.
func (r *DualLaneRouter) ResolveRoute(ctx context.Context, req RouteRequest) (RouteDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.resolveRouteLocked(ctx, req)
}

func (r *DualLaneRouter) resolveRouteLocked(ctx context.Context, req RouteRequest) (RouteDecision, error) {
	// 1. Normalize and infer Role & Intent
	role := req.Role
	intent := req.Intent

	if role == "" {
		switch intent {
		case IntentAgentTurn, IntentAppServerSession:
			role = RoleAgent
		case IntentClassifierAssessment:
			role = RoleClassifier
		case IntentReviewerAdvice:
			role = RoleReviewer
		default:
			return RouteDecision{}, fmt.Errorf("%w: role is required and cannot be inferred from intent %q", ErrNoRouteAvailable, intent)
		}
	}
	if !role.Valid() {
		return RouteDecision{}, fmt.Errorf("%w: invalid role %q", ErrNoRouteAvailable, role)
	}

	if intent == "" {
		switch role {
		case RoleAgent:
			intent = IntentAgentTurn
		case RoleClassifier:
			intent = IntentClassifierAssessment
		case RoleReviewer:
			intent = IntentReviewerAdvice
		}
	}
	if !intent.Valid() {
		return RouteDecision{}, fmt.Errorf("%w: invalid execution intent %q", ErrNoRouteAvailable, intent)
	}

	// 2. Strict Role Isolation Check
	if role == RoleClassifier || intent == IntentClassifierAssessment {
		if req.TargetLane == LaneCodexSubscription || req.CredentialClass == CredentialClassSubscription {
			return RouteDecision{}, fmt.Errorf("%w: classifier role cannot use subscription OAuth: %w", ErrClassifierOAuthProhibited, ErrLaneAuthMismatch)
		}
	}

	if role == RoleAgent || intent == IntentAgentTurn || intent == IntentAppServerSession {
		if req.TargetLane == LaneOpenAIResponses || req.CredentialClass == CredentialClassAPIKey {
			if !req.AllowAgentAPIKey && !r.apiLane.AllowAgentRouting {
				return RouteDecision{}, fmt.Errorf("%w: agent role requires explicit allow_agent_api_key opt-in: %w", ErrAgentAPIKeyProhibited, ErrLaneAuthMismatch)
			}
		}
	}

	// 3. Determine Candidate Lane
	var targetLane LaneID
	if req.TargetLane != "" {
		if !req.TargetLane.Valid() {
			return RouteDecision{}, fmt.Errorf("%w: invalid target lane %q", ErrNoRouteAvailable, req.TargetLane)
		}
		targetLane = req.TargetLane
	} else {
		// Inferred lane based on Role and CredentialClass
		switch role {
		case RoleClassifier, RoleReviewer:
			targetLane = LaneOpenAIResponses
		case RoleAgent:
			if req.CredentialClass == CredentialClassAPIKey && (req.AllowAgentAPIKey || r.apiLane.AllowAgentRouting) {
				targetLane = LaneOpenAIResponses
			} else {
				targetLane = LaneCodexSubscription
			}
		default:
			if req.CredentialClass == CredentialClassSubscription {
				targetLane = LaneCodexSubscription
			} else if req.CredentialClass == CredentialClassAPIKey {
				targetLane = LaneOpenAIResponses
			} else {
				return RouteDecision{}, fmt.Errorf("%w: cannot infer lane for role %q", ErrNoRouteAvailable, role)
			}
		}
	}

	// 4. Verify CredentialClass Compatibility with Selected Lane
	if req.CredentialClass != "" && req.CredentialClass != CredentialClassUnspecified {
		if targetLane == LaneCodexSubscription && req.CredentialClass != CredentialClassSubscription {
			return RouteDecision{}, fmt.Errorf("%w: lane %s requires subscription credentials, got %q", ErrLaneAuthMismatch, targetLane, req.CredentialClass)
		}
		if targetLane == LaneOpenAIResponses && req.CredentialClass != CredentialClassAPIKey {
			return RouteDecision{}, fmt.Errorf("%w: lane %s requires api_key credentials, got %q", ErrLaneAuthMismatch, targetLane, req.CredentialClass)
		}
	}

	// 5. Check Lane Readiness & Capabilities
	fallbackPolicy := req.FallbackPolicy
	if fallbackPolicy == "" {
		if r.cfg.DefaultFallback != "" {
			fallbackPolicy = r.cfg.DefaultFallback
		} else {
			fallbackPolicy = FallbackFailClosed
		}
	}

	switch targetLane {
	case LaneCodexSubscription:
		return r.resolveSubscriptionLane(req, role, intent, fallbackPolicy)
	case LaneOpenAIResponses:
		return r.resolveAPIResponsesLane(req, role, intent, fallbackPolicy)
	default:
		return RouteDecision{}, fmt.Errorf("%w: unrecognized lane %q", ErrNoRouteAvailable, targetLane)
	}
}

func (r *DualLaneRouter) resolveSubscriptionLane(
	req RouteRequest,
	role Role,
	intent ExecutionIntent,
	fallbackPolicy FallbackPolicy,
) (RouteDecision, error) {
	if !r.subLane.Enabled {
		return RouteDecision{}, fmt.Errorf("%w: subscription lane %s is disabled", ErrNoRouteAvailable, LaneCodexSubscription)
	}

	// Check runtime auth status if snapshot exists
	if r.subLane.AuthSnapshot != nil {
		if r.subLane.AuthSnapshot.State != protocol.RuntimeAuthStateAuthenticated {
			return RouteDecision{}, fmt.Errorf("%w: subscription runtime auth is not authenticated (state: %s)", ErrLaneAuthMismatch, r.subLane.AuthSnapshot.State)
		}
		if r.subLane.AuthSnapshot.Mode != protocol.RuntimeAuthModeChatGPTSubscription {
			return RouteDecision{}, fmt.Errorf("%w: subscription runtime auth mode mismatch (mode: %s)", ErrLaneAuthMismatch, r.subLane.AuthSnapshot.Mode)
		}
	}

	// Check lane-level required capabilities
	if req.RequiredCapabilities != 0 {
		if (r.subLane.Capabilities & req.RequiredCapabilities) != req.RequiredCapabilities {
			return RouteDecision{}, fmt.Errorf("%w: lane %s lacks required capabilities (have 0x%x, want 0x%x)", ErrCapabilityNotMet, LaneCodexSubscription, r.subLane.Capabilities, req.RequiredCapabilities)
		}
	}

	// Resolve Model
	selectedModel := req.ModelSelection.ModelID
	var resolvedCaps uint64 = r.subLane.Capabilities

	if r.subLane.Catalog != nil {
		if selectedModel == "" {
			if def, ok := r.subLane.Catalog.DefaultModel(); ok {
				selectedModel = def.ModelID
			} else if len(r.subLane.Catalog.Models) > 0 {
				selectedModel = r.subLane.Catalog.Models[0].ModelID
			}
		}

		if selectedModel != "" {
			desc, found := r.subLane.Catalog.GetModel(selectedModel)
			if !found {
				if !req.ModelSelection.AllowSubstitution {
					return RouteDecision{}, fmt.Errorf("%w: model %q not found in subscription catalog: %w", ErrModelUnavailable, selectedModel, ErrModelSubstitutionProhibited)
				}
				// If substitution requested, try FallbackModelID
				if req.ModelSelection.FallbackModelID != "" {
					fbDesc, fbFound := r.subLane.Catalog.GetModel(req.ModelSelection.FallbackModelID)
					if fbFound {
						desc = fbDesc
						selectedModel = fbDesc.ModelID
					} else {
						return RouteDecision{}, fmt.Errorf("%w: fallback model %q not found in catalog", ErrModelUnavailable, req.ModelSelection.FallbackModelID)
					}
				} else {
					return RouteDecision{}, fmt.Errorf("%w: model %q not found in subscription catalog", ErrModelUnavailable, selectedModel)
				}
			}

			// Validate Model Support State
			if req.ModelSelection.RequiredSupportState != "" {
				if !desc.SupportState.Satisfies(req.ModelSelection.RequiredSupportState) {
					return RouteDecision{}, fmt.Errorf("%w: model %q has support state %q, requires %q", ErrModelUnavailable, selectedModel, desc.SupportState, req.ModelSelection.RequiredSupportState)
				}
			} else {
				// Default requires at least Selectable
				if !desc.IsSelectable() {
					return RouteDecision{}, fmt.Errorf("%w: model %q has support state %q and is not selectable", ErrModelUnavailable, selectedModel, desc.SupportState)
				}
			}

			// Validate Model Capabilities
			if req.RequiredCapabilities != 0 && desc.Capabilities != 0 {
				if (desc.Capabilities & req.RequiredCapabilities) != req.RequiredCapabilities {
					return RouteDecision{}, fmt.Errorf("%w: model %q lacks required capabilities (have 0x%x, want 0x%x)", ErrCapabilityNotMet, selectedModel, desc.Capabilities, req.RequiredCapabilities)
				}
			}
			if desc.Capabilities != 0 {
				resolvedCaps = desc.Capabilities
			}
		}
	} else if selectedModel == "" {
		selectedModel = "gpt-5.3-codex"
	}

	endpoint := r.subLane.Endpoint
	if endpoint == "" {
		endpoint = DefaultSubscriptionEndpoint
	}

	return RouteDecision{
		LaneID:               LaneCodexSubscription,
		CredentialOwner:      r.subLane.CredentialOwner,
		AuthMode:             r.subLane.AuthMode,
		Endpoint:             endpoint,
		SelectedModel:        selectedModel,
		RequiredCapabilities: req.RequiredCapabilities,
		ResolvedCapabilities: resolvedCaps,
		BillingBoundary:      r.subLane.BillingBoundary,
		Role:                 role,
		Intent:               intent,
		FallbackPolicy:       fallbackPolicy,
		FallbackAllowed:      fallbackPolicy == FallbackSameLaneOnly,
		Metadata:             copyMetadata(req.Metadata),
	}, nil
}

func (r *DualLaneRouter) resolveAPIResponsesLane(
	req RouteRequest,
	role Role,
	intent ExecutionIntent,
	fallbackPolicy FallbackPolicy,
) (RouteDecision, error) {
	if !r.apiLane.Enabled {
		return RouteDecision{}, fmt.Errorf("%w: api responses lane %s is disabled", ErrNoRouteAvailable, LaneOpenAIResponses)
	}

	// Verify API key readiness
	if !r.apiLane.HasAPIKey && strings.TrimSpace(r.apiLane.APIKeyRef) == "" {
		return RouteDecision{}, fmt.Errorf("%w: api responses lane has no valid api key configured", ErrLaneAuthMismatch)
	}

	// Check lane-level required capabilities
	if req.RequiredCapabilities != 0 {
		if (r.apiLane.Capabilities & req.RequiredCapabilities) != req.RequiredCapabilities {
			return RouteDecision{}, fmt.Errorf("%w: lane %s lacks required capabilities (have 0x%x, want 0x%x)", ErrCapabilityNotMet, LaneOpenAIResponses, r.apiLane.Capabilities, req.RequiredCapabilities)
		}
	}

	selectedModel := req.ModelSelection.ModelID
	var resolvedCaps uint64 = r.apiLane.Capabilities

	if r.apiLane.Catalog != nil {
		if selectedModel == "" {
			if def, ok := r.apiLane.Catalog.DefaultModel(); ok {
				selectedModel = def.ModelID
			} else if len(r.apiLane.Catalog.Models) > 0 {
				selectedModel = r.apiLane.Catalog.Models[0].ModelID
			}
		}
		if selectedModel != "" {
			desc, found := r.apiLane.Catalog.GetModel(selectedModel)
			if !found {
				if !req.ModelSelection.AllowSubstitution {
					return RouteDecision{}, fmt.Errorf("%w: model %q not found in API catalog: %w", ErrModelUnavailable, selectedModel, ErrModelSubstitutionProhibited)
				}
				if req.ModelSelection.FallbackModelID != "" {
					fbDesc, fbFound := r.apiLane.Catalog.GetModel(req.ModelSelection.FallbackModelID)
					if fbFound {
						desc = fbDesc
						selectedModel = fbDesc.ModelID
					} else {
						return RouteDecision{}, fmt.Errorf("%w: fallback model %q not found in API catalog", ErrModelUnavailable, req.ModelSelection.FallbackModelID)
					}
				} else {
					return RouteDecision{}, fmt.Errorf("%w: model %q not found in API catalog", ErrModelUnavailable, selectedModel)
				}
			}
			if req.ModelSelection.RequiredSupportState != "" {
				if !desc.SupportState.Satisfies(req.ModelSelection.RequiredSupportState) {
					return RouteDecision{}, fmt.Errorf("%w: model %q has support state %q, requires %q", ErrModelUnavailable, selectedModel, desc.SupportState, req.ModelSelection.RequiredSupportState)
				}
			}
			if req.RequiredCapabilities != 0 && desc.Capabilities != 0 {
				if (desc.Capabilities & req.RequiredCapabilities) != req.RequiredCapabilities {
					return RouteDecision{}, fmt.Errorf("%w: model %q lacks required capabilities", ErrCapabilityNotMet, selectedModel)
				}
			}
			if desc.Capabilities != 0 {
				resolvedCaps = desc.Capabilities
			}
		}
	} else if len(r.apiLane.AvailableModels) > 0 {
		if selectedModel == "" {
			selectedModel = r.apiLane.AvailableModels[0]
		} else {
			var found bool
			for _, m := range r.apiLane.AvailableModels {
				if strings.EqualFold(m, selectedModel) {
					found = true
					break
				}
			}
			if !found {
				if !req.ModelSelection.AllowSubstitution {
					return RouteDecision{}, fmt.Errorf("%w: model %q not in available API models: %w", ErrModelUnavailable, selectedModel, ErrModelSubstitutionProhibited)
				}
				if req.ModelSelection.FallbackModelID != "" {
					selectedModel = req.ModelSelection.FallbackModelID
				} else {
					return RouteDecision{}, fmt.Errorf("%w: model %q not in available API models", ErrModelUnavailable, selectedModel)
				}
			}
		}
	} else if selectedModel == "" {
		selectedModel = "gpt-4o"
	}

	endpoint := r.apiLane.Path
	if endpoint == "" {
		endpoint = DefaultAPIResponsesPath
	}
	if r.apiLane.BaseURL != "" && !strings.HasPrefix(endpoint, "http") {
		endpoint = strings.TrimRight(r.apiLane.BaseURL, "/") + "/" + strings.TrimLeft(endpoint, "/")
	}

	return RouteDecision{
		LaneID:               LaneOpenAIResponses,
		CredentialOwner:      r.apiLane.CredentialOwner,
		AuthMode:             r.apiLane.AuthMode,
		Endpoint:             endpoint,
		SelectedModel:        selectedModel,
		RequiredCapabilities: req.RequiredCapabilities,
		ResolvedCapabilities: resolvedCaps,
		BillingBoundary:      r.apiLane.BillingBoundary,
		Role:                 role,
		Intent:               intent,
		FallbackPolicy:       fallbackPolicy,
		FallbackAllowed:      fallbackPolicy == FallbackSameLaneOnly,
		Metadata:             copyMetadata(req.Metadata),
	}, nil
}

// FallbackOption configures fallback resolution parameters.
type FallbackOption struct {
	FallbackModelID      string
	RequiredCapabilities uint64
}

// ResolveFallback adjudicates a failure on an active route under strict billing isolation invariants (#186).
//
// Invariant: Rate-limit (429), auth expiry (401), or any failure on Lane A must NEVER
// silently fall back to Lane B or cross billing boundaries.
func (r *DualLaneRouter) ResolveFallback(
	ctx context.Context,
	orig RouteDecision,
	failure error,
	opts ...FallbackOption,
) (RouteDecision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RouteDecision{}, err
	}
	if failure == nil {
		return orig, nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	// 1. If original policy is FailClosed or empty, reject immediately
	if orig.FallbackPolicy == FallbackFailClosed || orig.FallbackPolicy == "" || orig.FallbackPolicy == FallbackCrossLaneProhibited {
		return RouteDecision{}, fmt.Errorf("%w: fallback disabled by policy %q (failure: %v)", ErrCrossLaneFallbackProhibited, orig.FallbackPolicy, failure)
	}

	// 2. Strict Billing Boundary and Cross-Lane Fallback Check:
	// Cross-lane fallback (Subscription -> API or API -> Subscription) is ALWAYS prohibited.
	if orig.FallbackPolicy != FallbackSameLaneOnly {
		return RouteDecision{}, fmt.Errorf("%w: cross-lane fallback is strictly prohibited across billing boundaries", ErrCrossLaneFallbackProhibited)
	}

	// 3. Same-Lane Fallback Evaluation
	var fbModel string
	var reqCaps uint64 = orig.RequiredCapabilities
	if len(opts) > 0 {
		if opts[0].FallbackModelID != "" {
			fbModel = opts[0].FallbackModelID
		}
		if opts[0].RequiredCapabilities != 0 {
			reqCaps = opts[0].RequiredCapabilities
		}
	}

	if fbModel == "" || strings.EqualFold(fbModel, orig.SelectedModel) {
		return RouteDecision{}, fmt.Errorf("%w: no alternative same-lane fallback model specified for lane %s", ErrNoRouteAvailable, orig.LaneID)
	}

	// Re-resolve route with same-lane fallback model
	req := RouteRequest{
		Intent:               orig.Intent,
		Role:                 orig.Role,
		TargetLane:           orig.LaneID,
		RequiredCapabilities: reqCaps,
		ModelSelection: ModelSelection{
			ModelID:           fbModel,
			AllowSubstitution: false,
		},
		FallbackPolicy: FallbackFailClosed, // Prevent infinite fallback loops
	}
	if orig.Role == RoleAgent && orig.LaneID == LaneOpenAIResponses {
		req.AllowAgentAPIKey = true
	}

	newDec, err := r.resolveRouteLocked(ctx, req)
	if err != nil {
		return RouteDecision{}, fmt.Errorf("%w: same-lane fallback to model %q failed: %v", ErrNoRouteAvailable, fbModel, err)
	}

	// Ensure the lane didn't change
	if newDec.LaneID != orig.LaneID || newDec.BillingBoundary != orig.BillingBoundary {
		return RouteDecision{}, fmt.Errorf("%w: same-lane fallback resolved across billing boundaries (%s != %s)", ErrCrossLaneFallbackProhibited, newDec.BillingBoundary, orig.BillingBoundary)
	}

	return newDec, nil
}


// UpdateSubscriptionCatalog dynamically updates the model catalog for Lane A.
func (r *DualLaneRouter) UpdateSubscriptionCatalog(snap protocol.ModelCatalogSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := snap
	r.subLane.Catalog = &cp
}

// UpdateSubscriptionAuth dynamically updates the auth snapshot for Lane A.
func (r *DualLaneRouter) UpdateSubscriptionAuth(snap protocol.RuntimeAuthSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := snap
	r.subLane.AuthSnapshot = &cp
}

// UpdateAPICatalog dynamically updates the model catalog for Lane B.
func (r *DualLaneRouter) UpdateAPICatalog(snap protocol.ModelCatalogSnapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := snap
	r.apiLane.Catalog = &cp
}

// SetAPIAvailability updates the API key readiness for Lane B.
func (r *DualLaneRouter) SetAPIAvailability(hasKey bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.apiLane.HasAPIKey = hasKey
}

// Config returns a copy of the active RouterConfig.
func (r *DualLaneRouter) Config() RouterConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cfg
}

// Helper functions for error classification

// IsRateLimitError reports whether an error or status code indicates a 429 rate limit or quota exhaustion.
func IsRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate_limit") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "quota_exceeded") ||
		strings.Contains(msg, "too many requests")
}

// IsAuthExpiryError reports whether an error indicates a 401 unauthorized or expired token/session.
func IsAuthExpiryError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "unauthenticated") ||
		strings.Contains(msg, "expired") ||
		strings.Contains(msg, "token_expired") ||
		strings.Contains(msg, "session_expired")
}

// IsCrossLaneFallbackProhibited reports whether err is or wraps ErrCrossLaneFallbackProhibited.
func IsCrossLaneFallbackProhibited(err error) bool {
	return errors.Is(err, ErrCrossLaneFallbackProhibited)
}

// IsNoRouteAvailable reports whether err is or wraps ErrNoRouteAvailable.
func IsNoRouteAvailable(err error) bool {
	return errors.Is(err, ErrNoRouteAvailable)
}

// IsLaneAuthMismatch reports whether err is or wraps ErrLaneAuthMismatch.
func IsLaneAuthMismatch(err error) bool {
	return errors.Is(err, ErrLaneAuthMismatch)
}

// IsCapabilityNotMet reports whether err is or wraps ErrCapabilityNotMet.
func IsCapabilityNotMet(err error) bool {
	return errors.Is(err, ErrCapabilityNotMet)
}

func copyMetadata(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
