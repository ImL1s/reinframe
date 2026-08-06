package evaluation

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// Model lane constants (#140 Lane B — fake transports only).
const (
	ModelLaneOffline      = "lane_b_model_offline_fake"
	ModelLaneReportSchema = "reinframe.challenge_model_lane_report.v1"
)

// ModelLaneReport is the Lane B offline report (no live Claude / no real API).
type ModelLaneReport struct {
	SchemaVersion   string `json:"schema_version"`
	Lane            string `json:"lane"`
	Commit          string `json:"commit,omitempty"`
	Disposition     string `json:"disposition"`
	DispositionNote string `json:"disposition_note"`
	HardGateEnabled bool   `json:"hard_gate_enabled"`

	// Fake classifier challenge re-eval
	ChallengeReEvalOK bool `json:"challenge_reeval_ok"`

	// Native OpenAI Responses (httptest) structured assessment
	OpenAIAssessOK      bool `json:"openai_assess_ok"`
	OpenAIProviderCalls int  `json:"openai_provider_calls"`

	// Exact cache on/off comparison on identical request
	ExactCacheProviderCalls int  `json:"exact_cache_provider_calls"`
	ExactCacheHitOK         bool `json:"exact_cache_hit_ok"`
	Stage2InvariantOK       bool `json:"stage2_invariant_ok"`

	MalformedRejected bool `json:"malformed_rejected"`
	AuthNotRetried    bool `json:"auth_not_retried"`
}

// RunModelLaneB executes offline model-backed checks with fake HTTP only.
func RunModelLaneB(ctx context.Context, commit string) (ModelLaneReport, error) {
	rep := ModelLaneReport{
		SchemaVersion:   ModelLaneReportSchema,
		Lane:            ModelLaneOffline,
		Commit:          commit,
		HardGateEnabled: false,
		Disposition:     "MORE-DATA",
		DispositionNote: "Fake transports only; no live provider credentials. " +
			"Claude host lane excluded. No hard-gate enablement. " +
			"Numeric savings not claimed.",
	}

	// 1) Challenge re-eval with FakeClassifierProvider + Stage2 exception.
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := adapterProposed("sess-ml", "go test ./pkg/evaluation")
	rec, err := svc.Open(ctx, challenge.OpenRequest{
		SessionID: "sess-ml", Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		return rep, err
	}
	j := challenge.Justification{
		SchemaVersion: challenge.SchemaJustification, ChallengeID: rec.ChallengeID,
		ConcreteValue: "unblocks CI", PreventedFailureOrThreat: "failing suite",
		EstimatedCost: "1m", AlternativesConsidered: "subset", ScopeLimit: "pkg/evaluation",
		VerificationPlan: "go test", RollbackPlan: "revert",
	}
	if _, err := svc.Justify(ctx, j, nil); err != nil {
		return rep, err
	}
	res, err := svc.AttemptRetry(ctx, challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: "sess-ml", Proposed: pa,
		CorrelationID: "ml-1",
		ReEval: &challenge.ReEvalContext{
			// clear_block → high severity; UserException must flip Stage2 ALLOW.
			UserException: true,
			FixtureName:   "clear_block",
			Provider:      classifier.FakeClassifierProvider{ProviderKind: "fake"},
		},
	})
	if err != nil {
		return rep, err
	}
	rep.ChallengeReEvalOK = res.Stage2Decision == challenge.DecisionAllow &&
		res.Record.State == challenge.StateAllowedOnce

	// 2) Native OpenAI Responses via httptest.
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{
			"id":"resp_ml",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":12,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":10,"output_tokens":2,"input_tokens_details":{"cached_tokens":0}}
		}`))
	}))
	defer srv.Close()

	p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-test", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
		MaxRetries: 0, Timeout: time.Second,
	})
	if err != nil {
		return rep, err
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs", RulesetHash: "rh",
		TaskAnchor: classifier.TaskAnchor{TaskID: "t", Objective: "o"},
	})
	if err != nil {
		return rep, err
	}
	pres, err := p.Assess(ctx, req)
	if err != nil {
		return rep, err
	}
	rep.OpenAIAssessOK = pres.Assessment.Severity == 12 && pres.Assessment.ReasonCode == "NORMAL_PROGRESS"
	rep.OpenAIProviderCalls = int(calls.Load())

	// 3) Exact cache: identical request → one provider call.
	var cacheCalls atomic.Int32
	csrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cacheCalls.Add(1)
		_, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{
			"id":"resp_cache",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":20,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":5,"output_tokens":1}
		}`))
	}))
	defer csrv.Close()
	inner, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "gpt-cache", BaseURL: csrv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: csrv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
		MaxRetries: 0, Timeout: time.Second,
	})
	if err != nil {
		return rep, err
	}
	ec, err := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
		Enabled: true, MaxEntries: 16, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
	}, nil)
	if err != nil {
		return rep, err
	}
	wrapped := classifier.WrapWithExactCache(inner, ec, classifier.ExactCacheIdentity{
		ProviderKind: classifier.KindOpenAIResponses, ModelID: "gpt-cache",
		CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		ParserSchema:        classifier.SchemaRawAssessment,
	})
	req2, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
		RulesetID:     "rs", RulesetHash: "rh",
		TaskAnchor: classifier.TaskAnchor{TaskID: "t2", Objective: "obj"},
	})
	if err != nil {
		return rep, err
	}
	a1, err := wrapped.Assess(ctx, req2)
	if err != nil {
		return rep, err
	}
	a2, err := wrapped.Assess(ctx, req2)
	if err != nil {
		return rep, err
	}
	rep.ExactCacheProviderCalls = int(cacheCalls.Load())
	rep.ExactCacheHitOK = cacheCalls.Load() == 1 &&
		!a1.Usage.CacheHit &&
		a2.Usage.CacheHit &&
		a2.Usage.CacheBackend == classifier.ExactCacheLayerReinframeExact &&
		a2.Usage.InputTokens == 0 &&
		a1.Assessment.Severity == a2.Assessment.Severity
	// Stage-1 assessment equality only; ResolvedDecision is never stored in exact cache.
	rep.Stage2InvariantOK = a1.Assessment.ReasonCode == a2.Assessment.ReasonCode &&
		a2.Usage.CacheBackend == classifier.ExactCacheLayerReinframeExact

	// 4) Malformed assessment rejected.
	msrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","output":[{"type":"message","content":[{"type":"output_text","text":"not-json"}]}]}`))
	}))
	defer msrv.Close()
	mp, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: msrv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: msrv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
		MaxRetries: 0, Timeout: time.Second,
	})
	if err != nil {
		return rep, err
	}
	_, merr := mp.Assess(ctx, req)
	rep.MalformedRejected = merr != nil

	// 5) 401 not retried.
	var hits atomic.Int32
	asrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer asrv.Close()
	ap, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
		Model: "m", BaseURL: asrv.URL, Path: "/v1/responses", AllowRemote: true,
		HTTPClient: asrv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
		APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
		MaxRetries: 2, Timeout: time.Second,
		Sleep: func(context.Context, time.Duration) error { return nil },
	})
	if err != nil {
		return rep, err
	}
	_, _ = ap.Assess(ctx, req)
	rep.AuthNotRetried = hits.Load() == 1

	return rep, nil
}

func adapterProposed(session, cmd string) adapter.ProposedAction {
	return adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion,
		SessionID:     session, ActionID: "pa-ml",
		ToolName: "Bash", ToolClass: adapter.ToolClassShell, Command: cmd,
		WorkspaceRevision: "ws-1", Source: "synthetic", ParseStatus: "ok",
	}
}
