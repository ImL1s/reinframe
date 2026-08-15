package policy_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/policy"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func makeTestCatalog() protocol.ModelCatalogSnapshot {
	models := []protocol.ModelDescriptor{
		{
			ModelID:      "gpt-5.3-codex",
			DisplayName:  "GPT-5.3 Codex",
			SupportState: protocol.ModelSupportStateLiveQualified,
			Capabilities: uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapHooks | protocol.CapToolGate | protocol.CapAdviceDelivery),
			IsDefault:    true,
		},
		{
			ModelID:      "gpt-5.3-codex-spark",
			DisplayName:  "GPT-5.3 Codex Spark (Research Preview)",
			SupportState: protocol.ModelSupportStateSelectable,
			Capabilities: uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapHooks | protocol.CapToolGate),
			IsDefault:    false,
		},
		{
			ModelID:      "gpt-5.3-codex-unverified",
			DisplayName:  "GPT-5.3 Codex Unverified",
			SupportState: protocol.ModelSupportStateDiscovered,
			Capabilities: uint64(protocol.CapEventStream),
			IsDefault:    false,
		},
	}
	snap, err := protocol.NewModelCatalogSnapshot("auth-gen-test", "scope-test", models, time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		panic(err)
	}
	return snap
}

func makeTestAuthSnapshot(state protocol.RuntimeAuthState, mode protocol.RuntimeAuthMode) protocol.RuntimeAuthSnapshot {
	snap, err := protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwnerCodexProcess,
		mode,
		state,
		"default",
		"1.0.0",
		"scope-hash-1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		"auth-gen-hash-1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		time.Now().UTC(),
		nil,
	)
	if err != nil {
		panic(err)
	}
	return snap
}

func defaultTestRouter() *policy.DualLaneRouter {
	cat := makeTestCatalog()
	authSnap := makeTestAuthSnapshot(protocol.RuntimeAuthStateAuthenticated, protocol.RuntimeAuthModeChatGPTSubscription)

	return policy.NewDualLaneRouter(policy.RouterConfig{
		LaneSubscription: policy.LaneSubscriptionConfig{
			Enabled:      true,
			Capabilities: uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapHooks | protocol.CapToolGate | protocol.CapAdviceDelivery | protocol.CapCLIControl),
			Catalog:      &cat,
			AuthSnapshot: &authSnap,
		},
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:         true,
			HasAPIKey:       true,
			APIKeyRef:       "${OPENAI_API_KEY}",
			Capabilities:    uint64(protocol.CapEventStream | protocol.CapToolInspection | protocol.CapHooks | protocol.CapToolGate),
			AvailableModels: []string{"gpt-4o", "gpt-4o-mini", "o3-mini", "gpt-5.3-codex-spark"},
		},
		StrictBillingBound: true,
		DefaultFallback:    policy.FallbackFailClosed,
	})
}

// 1. Subscription Lane Resolution (Lane A)
func TestDualLaneRouter_SubscriptionLaneResolution(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
	}

	dec, err := r.ResolveRoute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful resolution, got error: %v", err)
	}

	if dec.LaneID != policy.LaneCodexSubscription {
		t.Errorf("expected lane %s, got %s", policy.LaneCodexSubscription, dec.LaneID)
	}
	if dec.CredentialOwner != protocol.CredentialOwnerCodexProcess {
		t.Errorf("expected credential owner %s, got %s", protocol.CredentialOwnerCodexProcess, dec.CredentialOwner)
	}
	if dec.AuthMode != protocol.RuntimeAuthModeChatGPTSubscription {
		t.Errorf("expected auth mode %s, got %s", protocol.RuntimeAuthModeChatGPTSubscription, dec.AuthMode)
	}
	if dec.BillingBoundary != policy.BillingBoundarySubscription {
		t.Errorf("expected billing boundary %s, got %s", policy.BillingBoundarySubscription, dec.BillingBoundary)
	}
	if dec.SelectedModel != "gpt-5.3-codex" {
		t.Errorf("expected model gpt-5.3-codex, got %s", dec.SelectedModel)
	}
	if dec.Endpoint != policy.DefaultSubscriptionEndpoint {
		t.Errorf("expected endpoint %s, got %s", policy.DefaultSubscriptionEndpoint, dec.Endpoint)
	}
}

// Default model resolution in Subscription lane
func TestDualLaneRouter_SubscriptionLaneDefaultModel(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "", // empty -> resolve default from catalog
		},
	}

	dec, err := r.ResolveRoute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful resolution, got error: %v", err)
	}

	if dec.SelectedModel != "gpt-5.3-codex" {
		t.Errorf("expected default model gpt-5.3-codex, got %s", dec.SelectedModel)
	}
}

// 2. API Responses Lane Resolution (Lane B)
func TestDualLaneRouter_APIResponsesLaneResolution(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o",
		},
	}

	dec, err := r.ResolveRoute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful resolution, got error: %v", err)
	}

	if dec.LaneID != policy.LaneOpenAIResponses {
		t.Errorf("expected lane %s, got %s", policy.LaneOpenAIResponses, dec.LaneID)
	}
	if dec.CredentialOwner != protocol.CredentialOwnerReinframeEnv {
		t.Errorf("expected credential owner %s, got %s", protocol.CredentialOwnerReinframeEnv, dec.CredentialOwner)
	}
	if dec.AuthMode != protocol.RuntimeAuthModeAPIKey {
		t.Errorf("expected auth mode %s, got %s", protocol.RuntimeAuthModeAPIKey, dec.AuthMode)
	}
	if dec.BillingBoundary != policy.BillingBoundaryAPITokens {
		t.Errorf("expected billing boundary %s, got %s", policy.BillingBoundaryAPITokens, dec.BillingBoundary)
	}
	if dec.SelectedModel != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", dec.SelectedModel)
	}
	if dec.Endpoint != "https://api.openai.com/v1/responses" {
		t.Errorf("expected endpoint https://api.openai.com/v1/responses, got %s", dec.Endpoint)
	}
}

// Reviewer role routes to Lane B
func TestDualLaneRouter_ReviewerRoleRoutesToAPILane(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleReviewer,
		Intent: policy.IntentReviewerAdvice,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o-mini",
		},
	}

	dec, err := r.ResolveRoute(context.Background(), req)
	if err != nil {
		t.Fatalf("expected successful resolution, got error: %v", err)
	}

	if dec.LaneID != policy.LaneOpenAIResponses {
		t.Errorf("expected lane %s, got %s", policy.LaneOpenAIResponses, dec.LaneID)
	}
	if dec.AuthMode != protocol.RuntimeAuthModeAPIKey {
		t.Errorf("expected auth mode %s, got %s", protocol.RuntimeAuthModeAPIKey, dec.AuthMode)
	}
}

// 3. Strict Isolation Invariants
// Invariant: Classifier cannot route to Subscription OAuth
func TestDualLaneRouter_ClassifierCannotRouteToSubscriptionOAuth(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	tests := []struct {
		name string
		req  policy.RouteRequest
	}{
		{
			name: "explicit target lane subscription",
			req: policy.RouteRequest{
				Role:       policy.RoleClassifier,
				Intent:     policy.IntentClassifierAssessment,
				TargetLane: policy.LaneCodexSubscription,
			},
		},
		{
			name: "explicit credential class subscription",
			req: policy.RouteRequest{
				Role:            policy.RoleClassifier,
				Intent:          policy.IntentClassifierAssessment,
				CredentialClass: policy.CredentialClassSubscription,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.ResolveRoute(context.Background(), tt.req)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, policy.ErrClassifierOAuthProhibited) {
				t.Errorf("expected ErrClassifierOAuthProhibited, got: %v", err)
			}
			if !errors.Is(err, policy.ErrLaneAuthMismatch) {
				t.Errorf("expected ErrLaneAuthMismatch, got: %v", err)
			}
		})
	}
}

// Invariant: Agent cannot route to API key without explicit opt-in
func TestDualLaneRouter_AgentCannotRouteToAPIKeyWithoutOptIn(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// 1. Without opt-in -> must fail closed
	reqWithoutOptIn := policy.RouteRequest{
		Role:             policy.RoleAgent,
		Intent:           policy.IntentAgentTurn,
		TargetLane:       policy.LaneOpenAIResponses,
		AllowAgentAPIKey: false,
	}

	_, err := r.ResolveRoute(context.Background(), reqWithoutOptIn)
	if err == nil {
		t.Fatalf("expected error for agent routing to API lane without opt-in")
	}
	if !errors.Is(err, policy.ErrAgentAPIKeyProhibited) {
		t.Errorf("expected ErrAgentAPIKeyProhibited, got: %v", err)
	}

	// 2. With explicit AllowAgentAPIKey opt-in -> must succeed
	reqWithOptIn := policy.RouteRequest{
		Role:             policy.RoleAgent,
		Intent:           policy.IntentAgentTurn,
		TargetLane:       policy.LaneOpenAIResponses,
		AllowAgentAPIKey: true,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o",
		},
	}

	dec, err := r.ResolveRoute(context.Background(), reqWithOptIn)
	if err != nil {
		t.Fatalf("expected success with explicit opt-in, got: %v", err)
	}
	if dec.LaneID != policy.LaneOpenAIResponses {
		t.Errorf("expected lane %s, got %s", policy.LaneOpenAIResponses, dec.LaneID)
	}
}

// 4. Rejection of Cross-Lane Fallback on 429 Rate Limit or 401 Auth Expiry
func TestDualLaneRouter_RejectionOfCrossLaneFallback_RateLimit429(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// Initial route resolved on Lane A
	origRoute, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
		FallbackPolicy: policy.FallbackFailClosed,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	rateLimitErr := errors.New("HTTP 429: Rate limit exceeded: too many requests for subscription quota")
	if !policy.IsRateLimitError(rateLimitErr) {
		t.Errorf("expected IsRateLimitError to be true")
	}

	// Attempt fallback with FailClosed
	_, fbErr := r.ResolveFallback(context.Background(), origRoute, rateLimitErr)
	if fbErr == nil {
		t.Fatalf("expected fallback rejection on 429 rate limit")
	}
	if !errors.Is(fbErr, policy.ErrCrossLaneFallbackProhibited) {
		t.Errorf("expected ErrCrossLaneFallbackProhibited, got: %v", fbErr)
	}

	// Even if request configured SameLaneOnly, cross-lane fallback is strictly prohibited
	origRouteSameLane := origRoute
	origRouteSameLane.FallbackPolicy = policy.FallbackSameLaneOnly

	// No alternate model provided -> fail closed, no cross-lane fallback
	_, fbErrNoAlt := r.ResolveFallback(context.Background(), origRouteSameLane, rateLimitErr)
	if fbErrNoAlt == nil {
		t.Fatalf("expected error when no alternate same-lane model")
	}
	if !errors.Is(fbErrNoAlt, policy.ErrNoRouteAvailable) {
		t.Errorf("expected ErrNoRouteAvailable, got: %v", fbErrNoAlt)
	}
}

func TestDualLaneRouter_RejectionOfCrossLaneFallback_AuthExpiry401(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	origRoute, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
		FallbackPolicy: policy.FallbackFailClosed,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	authExpiryErr := errors.New("HTTP 401: Unauthorized - subscription OAuth token expired")
	if !policy.IsAuthExpiryError(authExpiryErr) {
		t.Errorf("expected IsAuthExpiryError to be true")
	}

	_, fbErr := r.ResolveFallback(context.Background(), origRoute, authExpiryErr)
	if fbErr == nil {
		t.Fatalf("expected fallback rejection on 401 auth expiry")
	}
	if !errors.Is(fbErr, policy.ErrCrossLaneFallbackProhibited) {
		t.Errorf("expected ErrCrossLaneFallbackProhibited, got: %v", fbErr)
	}
}

// Same-lane fallback works within Lane A if alternative model is available in catalog
func TestDualLaneRouter_SameLaneFallbackWithinLaneA(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	origRoute, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
		FallbackPolicy: policy.FallbackSameLaneOnly,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	simulatedErr := errors.New("service degradation on primary model")
	fbRoute, err := r.ResolveFallback(context.Background(), origRoute, simulatedErr, policy.FallbackOption{
		FallbackModelID: "gpt-5.3-codex-spark",
	})
	if err != nil {
		t.Fatalf("expected successful same-lane fallback, got: %v", err)
	}

	if fbRoute.LaneID != policy.LaneCodexSubscription {
		t.Errorf("expected same lane %s, got %s", policy.LaneCodexSubscription, fbRoute.LaneID)
	}
	if fbRoute.SelectedModel != "gpt-5.3-codex-spark" {
		t.Errorf("expected fallback model gpt-5.3-codex-spark, got %s", fbRoute.SelectedModel)
	}
	if fbRoute.BillingBoundary != policy.BillingBoundarySubscription {
		t.Errorf("expected billing boundary %s, got %s", policy.BillingBoundarySubscription, fbRoute.BillingBoundary)
	}
}

// 5. Capability and Model Support State Validation
func TestDualLaneRouter_CapabilityValidation(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// Request capability that Lane A does not support (e.g. CapLocalModels = 1<<18)
	req := policy.RouteRequest{
		Role:                 policy.RoleAgent,
		Intent:               policy.IntentAgentTurn,
		RequiredCapabilities: uint64(protocol.CapLocalModels),
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
	}

	_, err := r.ResolveRoute(context.Background(), req)
	if err == nil {
		t.Fatalf("expected ErrCapabilityNotMet for unsupported capability")
	}
	if !errors.Is(err, policy.ErrCapabilityNotMet) {
		t.Errorf("expected ErrCapabilityNotMet, got: %v", err)
	}
}

// Support State validation: model requires LiveQualified but is only Discovered
func TestDualLaneRouter_ModelSupportStateValidation(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// gpt-5.3-codex-unverified has SupportState: Discovered
	req := policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID:              "gpt-5.3-codex-unverified",
			RequiredSupportState: protocol.ModelSupportStateLiveQualified,
		},
	}

	_, err := r.ResolveRoute(context.Background(), req)
	if err == nil {
		t.Fatalf("expected ErrModelUnavailable for unverified model")
	}
	if !errors.Is(err, policy.ErrModelUnavailable) {
		t.Errorf("expected ErrModelUnavailable, got: %v", err)
	}
}

// Zero Silent Substitution: unknown model fails closed when AllowSubstitution is false
func TestDualLaneRouter_ZeroSilentSubstitution(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID:           "non-existent-future-model",
			AllowSubstitution: false, // Strict: zero silent substitution
		},
	}

	_, err := r.ResolveRoute(context.Background(), req)
	if err == nil {
		t.Fatalf("expected ErrModelUnavailable for missing model without substitution")
	}
	if !errors.Is(err, policy.ErrModelUnavailable) {
		t.Errorf("expected ErrModelUnavailable, got: %v", err)
	}
	if !errors.Is(err, policy.ErrModelSubstitutionProhibited) {
		t.Errorf("expected ErrModelSubstitutionProhibited, got: %v", err)
	}
}

// 6. Dynamic State & Auth Updates
func TestDualLaneRouter_DynamicAuthUpdate(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// Route initially works
	req := policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-5.3-codex",
		},
	}
	_, err := r.ResolveRoute(context.Background(), req)
	if err != nil {
		t.Fatalf("initial route failed: %v", err)
	}

	// Update auth state to Expired
	expiredSnap := makeTestAuthSnapshot(protocol.RuntimeAuthStateExpired, protocol.RuntimeAuthModeChatGPTSubscription)
	r.UpdateSubscriptionAuth(expiredSnap)

	// Route must now fail closed
	_, errExpired := r.ResolveRoute(context.Background(), req)
	if errExpired == nil {
		t.Fatalf("expected error after auth expired")
	}
	if !errors.Is(errExpired, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch, got: %v", errExpired)
	}
}

// 7. Concurrency & Race Safety
func TestDualLaneRouter_ConcurrencyAndRaceSafety(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	const workers = 20
	const iterations = 50
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		workerID := i
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch workerID % 3 {
				case 0:
					// Agent turn on Lane A
					_, _ = r.ResolveRoute(context.Background(), policy.RouteRequest{
						Role:   policy.RoleAgent,
						Intent: policy.IntentAgentTurn,
						ModelSelection: policy.ModelSelection{
							ModelID: "gpt-5.3-codex",
						},
					})
				case 1:
					// Classifier assessment on Lane B
					_, _ = r.ResolveRoute(context.Background(), policy.RouteRequest{
						Role:   policy.RoleClassifier,
						Intent: policy.IntentClassifierAssessment,
						ModelSelection: policy.ModelSelection{
							ModelID: "gpt-4o",
						},
					})
				default:
					// Dynamic catalog / auth update
					if j%2 == 0 {
						cat := makeTestCatalog()
						r.UpdateSubscriptionCatalog(cat)
					} else {
						authSnap := makeTestAuthSnapshot(protocol.RuntimeAuthStateAuthenticated, protocol.RuntimeAuthModeChatGPTSubscription)
						r.UpdateSubscriptionAuth(authSnap)
					}
				}
			}
		}()
	}

	wg.Wait()
}

// 8. Helper Functions Verification
func TestDualLaneRouter_HelperFunctions(t *testing.T) {
	t.Parallel()

	// Rate limit helper
	if !policy.IsRateLimitError(errors.New("429 Too Many Requests")) {
		t.Errorf("expected IsRateLimitError true for 429")
	}
	if !policy.IsRateLimitError(errors.New("rate_limit_exceeded")) {
		t.Errorf("expected IsRateLimitError true for rate_limit_exceeded")
	}
	if policy.IsRateLimitError(errors.New("normal error")) {
		t.Errorf("expected IsRateLimitError false for normal error")
	}

	// Auth expiry helper
	if !policy.IsAuthExpiryError(errors.New("401 Unauthorized")) {
		t.Errorf("expected IsAuthExpiryError true for 401")
	}
	if !policy.IsAuthExpiryError(errors.New("session_expired")) {
		t.Errorf("expected IsAuthExpiryError true for session_expired")
	}
	if policy.IsAuthExpiryError(errors.New("normal error")) {
		t.Errorf("expected IsAuthExpiryError false for normal error")
	}

	// Error matchers
	if !policy.IsCrossLaneFallbackProhibited(fmt.Errorf("wrapped: %w", policy.ErrCrossLaneFallbackProhibited)) {
		t.Errorf("expected IsCrossLaneFallbackProhibited true")
	}
	if !policy.IsNoRouteAvailable(fmt.Errorf("wrapped: %w", policy.ErrNoRouteAvailable)) {
		t.Errorf("expected IsNoRouteAvailable true")
	}
	if !policy.IsLaneAuthMismatch(fmt.Errorf("wrapped: %w", policy.ErrLaneAuthMismatch)) {
		t.Errorf("expected IsLaneAuthMismatch true")
	}
	if !policy.IsCapabilityNotMet(fmt.Errorf("wrapped: %w", policy.ErrCapabilityNotMet)) {
		t.Errorf("expected IsCapabilityNotMet true")
	}
}

// 9. Disabled Lane and Missing Auth Edge Cases
func TestDualLaneRouter_DisabledLanes(t *testing.T) {
	t.Parallel()

	// 1. Subscription lane disabled
	rSubDisabled := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneSubscription: policy.LaneSubscriptionConfig{
			Enabled: false,
		},
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:   true,
			HasAPIKey: true,
		},
	})
	_, errSub := rSubDisabled.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
	})
	if errSub == nil || !errors.Is(errSub, policy.ErrNoRouteAvailable) {
		t.Errorf("expected ErrNoRouteAvailable when subscription lane is disabled, got: %v", errSub)
	}

	// 2. API responses lane disabled
	rAPIDisabled := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneSubscription: policy.LaneSubscriptionConfig{
			Enabled: true,
		},
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled: false,
		},
	})
	_, errAPI := rAPIDisabled.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
	})
	if errAPI == nil || !errors.Is(errAPI, policy.ErrNoRouteAvailable) {
		t.Errorf("expected ErrNoRouteAvailable when API lane is disabled, got: %v", errAPI)
	}
}

func TestDualLaneRouter_MissingAPIKey(t *testing.T) {
	t.Parallel()
	r := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:   true,
			HasAPIKey: false,
			APIKeyRef: "", // No key ref or key
		},
	})

	_, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
	})
	if err == nil || !errors.Is(err, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch when API key missing, got: %v", err)
	}
}

func TestDualLaneRouter_SubscriptionAuthStates(t *testing.T) {
	t.Parallel()

	states := []protocol.RuntimeAuthState{
		protocol.RuntimeAuthStateUnauthenticated,
		protocol.RuntimeAuthStateUnavailable,
	}

	for _, st := range states {
		t.Run(string(st), func(t *testing.T) {
			authSnap := makeTestAuthSnapshot(st, protocol.RuntimeAuthModeChatGPTSubscription)
			r := policy.NewDualLaneRouter(policy.RouterConfig{
				LaneSubscription: policy.LaneSubscriptionConfig{
					Enabled:      true,
					AuthSnapshot: &authSnap,
				},
			})

			_, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
				Role:   policy.RoleAgent,
				Intent: policy.IntentAgentTurn,
			})
			if err == nil || !errors.Is(err, policy.ErrLaneAuthMismatch) {
				t.Errorf("expected ErrLaneAuthMismatch for auth state %s, got: %v", st, err)
			}
		})
	}
}

func TestDualLaneRouter_SubscriptionAuthModeMismatch(t *testing.T) {
	t.Parallel()
	authSnap := makeTestAuthSnapshot(protocol.RuntimeAuthStateAuthenticated, protocol.RuntimeAuthModeAPIKey)
	r := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneSubscription: policy.LaneSubscriptionConfig{
			Enabled:      true,
			AuthSnapshot: &authSnap,
		},
	})

	_, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
	})
	if err == nil || !errors.Is(err, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch when subscription snapshot has api_key mode, got: %v", err)
	}
}

func TestDualLaneRouter_CredentialClassMismatch(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	// 1. Explicit APIKey targeting Subscription lane
	_, err1 := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:            policy.RoleAgent,
		Intent:          policy.IntentAgentTurn,
		TargetLane:      policy.LaneCodexSubscription,
		CredentialClass: policy.CredentialClassAPIKey,
	})
	if err1 == nil || !errors.Is(err1, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch for api_key on subscription lane, got: %v", err1)
	}

	// 2. Explicit Subscription targeting API lane
	_, err2 := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:            policy.RoleClassifier,
		Intent:          policy.IntentClassifierAssessment,
		TargetLane:      policy.LaneOpenAIResponses,
		CredentialClass: policy.CredentialClassSubscription,
	})
	if err2 == nil || !errors.Is(err2, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch for subscription credential on API lane, got: %v", err2)
	}
}

// 10. Context Cancellation
func TestDualLaneRouter_ContextCancellation(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := r.ResolveRoute(ctx, policy.RouteRequest{
		Role:   policy.RoleAgent,
		Intent: policy.IntentAgentTurn,
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}

	_, fbErr := r.ResolveFallback(ctx, policy.RouteDecision{}, errors.New("any error"))
	if !errors.Is(fbErr, context.Canceled) {
		t.Errorf("expected context.Canceled on ResolveFallback, got: %v", fbErr)
	}
}

// 11. Fallback from API Lane to Subscription Lane is Prohibited
func TestDualLaneRouter_APILaneFallbackToSubscriptionProhibited(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	origRoute, err := r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o",
		},
		FallbackPolicy: policy.FallbackCrossLaneProhibited,
	})
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	rateLimitErr := errors.New("HTTP 429 Too Many Requests on API key quota")
	_, fbErr := r.ResolveFallback(context.Background(), origRoute, rateLimitErr)
	if fbErr == nil || !errors.Is(fbErr, policy.ErrCrossLaneFallbackProhibited) {
		t.Errorf("expected ErrCrossLaneFallbackProhibited, got: %v", fbErr)
	}
}

// 12. Dynamic Updates to API Lane (SetAPIAvailability & UpdateAPICatalog)
func TestDualLaneRouter_DynamicAPILaneUpdates(t *testing.T) {
	t.Parallel()
	r := defaultTestRouter()

	req := policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o",
		},
	}

	// 1. Disable API availability
	r.SetAPIAvailability(false)
	// Temporarily clear APIKeyRef for test of no key
	rNoKey := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:   true,
			HasAPIKey: true,
		},
	})
	rNoKey.SetAPIAvailability(false)
	_, errDisabled := rNoKey.ResolveRoute(context.Background(), req)
	if errDisabled == nil || !errors.Is(errDisabled, policy.ErrLaneAuthMismatch) {
		t.Errorf("expected ErrLaneAuthMismatch after SetAPIAvailability(false), got: %v", errDisabled)
	}

	// 2. Enable API availability
	rNoKey.SetAPIAvailability(true)
	decEnabled, errEnabled := rNoKey.ResolveRoute(context.Background(), req)
	if errEnabled != nil {
		t.Errorf("expected success after SetAPIAvailability(true), got: %v", errEnabled)
	}
	if decEnabled.LaneID != policy.LaneOpenAIResponses {
		t.Errorf("expected LaneOpenAIResponses, got %s", decEnabled.LaneID)
	}

	// 3. Update API catalog
	apiCat := makeTestCatalog()
	r.UpdateAPICatalog(apiCat)
	cfg := r.Config()
	if !cfg.StrictBillingBound {
		t.Errorf("expected StrictBillingBound true")
	}
}

// 13. Enum Methods & Validation
func TestDualLaneRouter_EnumMethods(t *testing.T) {
	t.Parallel()

	// LaneID
	if !policy.LaneCodexSubscription.Valid() || !policy.LaneOpenAIResponses.Valid() {
		t.Errorf("expected valid LaneIDs")
	}
	if policy.LaneID("invalid_lane").Valid() {
		t.Errorf("expected invalid LaneID to return false")
	}
	if !policy.LaneCodexSubscription.IsSubscription() || policy.LaneCodexSubscription.IsAPI() {
		t.Errorf("LaneCodexSubscription methods incorrect")
	}
	if !policy.LaneOpenAIResponses.IsAPI() || policy.LaneOpenAIResponses.IsSubscription() {
		t.Errorf("LaneOpenAIResponses methods incorrect")
	}

	// Role
	if !policy.RoleAgent.Valid() || !policy.RoleClassifier.Valid() || !policy.RoleReviewer.Valid() {
		t.Errorf("expected valid Roles")
	}
	if policy.Role("invalid_role").Valid() {
		t.Errorf("expected invalid Role to return false")
	}

	// ExecutionIntent
	if !policy.IntentAgentTurn.Valid() || !policy.IntentClassifierAssessment.Valid() || !policy.IntentReviewerAdvice.Valid() || !policy.IntentDirectAPI.Valid() || !policy.IntentAppServerSession.Valid() || !policy.IntentBenchmarkRun.Valid() {
		t.Errorf("expected valid ExecutionIntents")
	}
	if policy.ExecutionIntent("invalid_intent").Valid() {
		t.Errorf("expected invalid ExecutionIntent to return false")
	}

	// CredentialClass
	if !policy.CredentialClassSubscription.Valid() || !policy.CredentialClassAPIKey.Valid() || !policy.CredentialClassUnspecified.Valid() {
		t.Errorf("expected valid CredentialClasses")
	}
	if policy.CredentialClass("invalid_cred").Valid() {
		t.Errorf("expected invalid CredentialClass to return false")
	}
	if policy.CredentialClassSubscription.ToAuthMode() != protocol.RuntimeAuthModeChatGPTSubscription {
		t.Errorf("expected RuntimeAuthModeChatGPTSubscription")
	}
	if policy.CredentialClassAPIKey.ToAuthMode() != protocol.RuntimeAuthModeAPIKey {
		t.Errorf("expected RuntimeAuthModeAPIKey")
	}
	if policy.CredentialClassUnspecified.ToAuthMode() != protocol.RuntimeAuthModeUnknown {
		t.Errorf("expected RuntimeAuthModeUnknown")
	}

	// FallbackPolicy
	if !policy.FallbackFailClosed.Valid() || !policy.FallbackSameLaneOnly.Valid() || !policy.FallbackCrossLaneProhibited.Valid() {
		t.Errorf("expected valid FallbackPolicies")
	}
	if policy.FallbackPolicy("invalid_policy").Valid() {
		t.Errorf("expected invalid FallbackPolicy to return false")
	}
}

func TestDualLaneRouter_APILaneSelectableAndFallbackValidation(t *testing.T) {
	t.Parallel()

	// 1. API lane with catalog where a model is unselectable (discovered only)
	models := []protocol.ModelDescriptor{
		{
			ModelID:      "gpt-4o",
			DisplayName:  "GPT-4o",
			SupportState: protocol.ModelSupportStateSelectable,
			Capabilities: 15,
		},
		{
			ModelID:      "gpt-4o-unselectable",
			DisplayName:  "Discovered Only Model",
			SupportState: protocol.ModelSupportStateDiscovered,
			Capabilities: 15,
		},
	}
	snap, err := protocol.NewModelCatalogSnapshot(
		"authgen1",
		"scope1",
		models,
		time.Now().UTC(),
		time.Now().UTC().Add(time.Hour),
	)
	if err != nil {
		t.Fatal(err)
	}

	r := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:         true,
			HasAPIKey:       true,
			Catalog:         &snap,
			AvailableModels: []string{"gpt-4o", "gpt-4o-mini"},
		},
	})

	// Unselectable model without explicit RequiredSupportState should fail
	_, err = r.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
		ModelSelection: policy.ModelSelection{
			ModelID: "gpt-4o-unselectable",
		},
	})
	if err == nil {
		t.Fatal("expected error requesting unselectable model in API lane by default")
	}
	if !errors.Is(err, policy.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got: %v", err)
	}

	// 2. Fallback model ID not in AvailableModels should fail
	rNoCat := policy.NewDualLaneRouter(policy.RouterConfig{
		LaneAPIResponses: policy.LaneAPIResponsesConfig{
			Enabled:         true,
			HasAPIKey:       true,
			AvailableModels: []string{"gpt-4o", "gpt-4o-mini"},
		},
	})

	_, errFb := rNoCat.ResolveRoute(context.Background(), policy.RouteRequest{
		Role:   policy.RoleClassifier,
		Intent: policy.IntentClassifierAssessment,
		ModelSelection: policy.ModelSelection{
			ModelID:           "non-existent-model",
			AllowSubstitution: true,
			FallbackModelID:   "invalid-fallback-model",
		},
	})
	if errFb == nil {
		t.Fatal("expected error when fallback model not in AvailableModels allowlist")
	}
	if !errors.Is(errFb, policy.ErrModelUnavailable) {
		t.Fatalf("expected ErrModelUnavailable, got: %v", errFb)
	}
}

