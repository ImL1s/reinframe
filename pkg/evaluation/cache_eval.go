package evaluation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ImL1s/reinframe/pkg/classifier"
)

// #141 report schema (fake-provider CI mechanics; no real cost claims).
const (
	CacheEvalReportSchema = "reinframe.cache_eval_report.v1"
	CacheEvalLaneFake     = "provider_cache_eval_fake_ci"
)

// CacheEvalMode labels one execution mode row.
type CacheEvalMode string

const (
	ModeUncachedProvider    CacheEvalMode = "uncached_provider"
	ModeProviderCacheWarm   CacheEvalMode = "provider_cache_warm_read" // transport reports cached tokens
	ModeReinframeExactHit   CacheEvalMode = "reinframe_exact_hit"
	ModeSingleflightN       CacheEvalMode = "singleflight_N_callers"
	ModeRequiredMissModel   CacheEvalMode = "required_miss_after_model_change"
	ModeRequiredMissEvents  CacheEvalMode = "required_miss_after_event_change"
	ModeGenericCacheNeutral CacheEvalMode = "generic_openai_compatible_cache_neutral"
)

// CacheEvalRow is one mode × provider result.
type CacheEvalRow struct {
	Mode            CacheEvalMode `json:"mode"`
	ProviderKind    string        `json:"provider_kind"`
	Profile         string        `json:"profile,omitempty"`
	ProviderCalls   int           `json:"provider_calls"`
	CacheHit        bool          `json:"cache_hit"`
	CacheBackend    string        `json:"cache_backend,omitempty"`
	CacheReadTokens int64         `json:"cache_read_tokens,omitempty"`
	Severity        int           `json:"severity,omitempty"`
	OK              bool          `json:"ok"`
	Note            string        `json:"note,omitempty"`
}

// CacheEvalReport is the #141 CI-safe report.
type CacheEvalReport struct {
	SchemaVersion   string         `json:"schema_version"`
	Lane            string         `json:"lane"`
	Commit          string         `json:"commit,omitempty"`
	Disposition     string         `json:"disposition"` // MORE-DATA | LIMITED-GO | NO-GO
	DispositionNote string         `json:"disposition_note"`
	HardGateEnabled bool           `json:"hard_gate_enabled"`
	DefaultCacheOn  bool           `json:"default_cache_on"` // always false from this issue
	Rows            []CacheEvalRow `json:"rows"`
	// Correctness aggregate
	StaleHitRate          float64 `json:"stale_hit_rate"` // must be 0
	InvalidAdmissionCount int     `json:"invalid_admission_count"`
	AllModesOK            bool    `json:"all_modes_ok"`
}

// RunCacheEvalFakeCI runs #141 mechanics against fake HTTP for all native kinds.
// Real-provider opt-in is not performed; disposition MORE-DATA for economics.
func RunCacheEvalFakeCI(ctx context.Context, commit string) (CacheEvalReport, error) {
	rep := CacheEvalReport{
		SchemaVersion:   CacheEvalReportSchema,
		Lane:            CacheEvalLaneFake,
		Commit:          commit,
		HardGateEnabled: false,
		DefaultCacheOn:  false,
		Disposition:     "MORE-DATA",
		DispositionNote: "Fake HTTP fixtures only. No real provider credentials, no billed cost, " +
			"no universal savings percentage. Exact-cache + provider usage mechanics validated. " +
			"Live economics remain unknown until opt-in measured runs. Default cache enablement not authorized.",
	}

	// Shared assessment body helpers.
	okBody := func(sev int, cached int64) string {
		return fmt.Sprintf(`{
			"id":"r",
			"output":[{"type":"message","content":[{"type":"output_text","text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":%d,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}],
			"usage":{"input_tokens":100,"output_tokens":5,"input_tokens_details":{"cached_tokens":%d}}
		}`, sev, cached)
	}
	// Anthropic-shaped body
	anthBody := func(sev int, read int64) string {
		return fmt.Sprintf(`{
			"id":"msg",
			"content":[{"type":"tool_use","name":"reinframe_raw_assessment","input":{"schema_version":"reinframe.raw_assessment.v1","severity":%d,"reason_code":"NORMAL_PROGRESS","evidence_event_ids":[]}}],
			"usage":{"input_tokens":60,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}
		}`, sev, read)
	}
	// Gemini-shaped
	gemBody := func(sev int, cached int64) string {
		return fmt.Sprintf(`{
			"responseId":"g",
			"candidates":[{"content":{"parts":[{"text":"{\"schema_version\":\"reinframe.raw_assessment.v1\",\"severity\":%d,\"reason_code\":\"NORMAL_PROGRESS\",\"evidence_event_ids\":[]}"}]}}],
			"usageMetadata":{"promptTokenCount":50,"candidatesTokenCount":5,"cachedContentTokenCount":%d}
		}`, sev, cached)
	}

	baseReq := func() (classifier.ProviderRequest, error) {
		return classifier.NewProviderRequest(classifier.ClassifierInput{
			SchemaVersion: classifier.SchemaClassifierInput,
			PolicyClass:   classifier.PolicyClassProductivity,
			RulesetID:     "rs", RulesetHash: "rh",
			TaskAnchor: classifier.TaskAnchor{TaskID: "t", Objective: "obj"},
			RecentEvents: []classifier.EventDigest{
				{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "edit", ContentHash: "h1"},
			},
		})
	}

	// --- OpenAI uncached ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(okBody(12, 0)))
		}))
		p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		req, _ := baseReq()
		res, err := p.Assess(ctx, req)
		srv.Close()
		row := CacheEvalRow{Mode: ModeUncachedProvider, ProviderKind: classifier.KindOpenAIResponses,
			Profile: classifier.CapabilitiesProfileOpenAIOffV1, ProviderCalls: int(n.Load()),
			CacheHit: res.Usage.CacheHit, Severity: res.Assessment.Severity,
			OK: err == nil && n.Load() == 1 && !res.Usage.CacheHit && res.Assessment.Severity == 12}
		if !row.OK {
			row.Note = fmt.Sprintf("err=%v calls=%d hit=%v", err, n.Load(), res.Usage.CacheHit)
		}
		rep.Rows = append(rep.Rows, row)
	}

	// --- OpenAI provider warm read (positive cached tokens) ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(okBody(12, 40)))
		}))
		p, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIImplicitV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		req, _ := baseReq()
		res, err := p.Assess(ctx, req)
		srv.Close()
		row := CacheEvalRow{Mode: ModeProviderCacheWarm, ProviderKind: classifier.KindOpenAIResponses,
			ProviderCalls: int(n.Load()), CacheHit: res.Usage.CacheHit, CacheReadTokens: res.Usage.CacheReadTokens,
			OK: err == nil && res.Usage.CacheHit && res.Usage.CacheReadTokens == 40}
		rep.Rows = append(rep.Rows, row)
	}

	// --- Reinframe exact hit (OpenAI) ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(okBody(15, 0)))
		}))
		inner, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		ec, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
			Enabled: true, MaxEntries: 32, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
		}, nil)
		p := classifier.WrapWithExactCache(inner, ec, classifier.ExactCacheIdentity{
			ProviderKind: classifier.KindOpenAIResponses, ModelID: "m",
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			ParserSchema:        classifier.SchemaRawAssessment,
		})
		req, _ := baseReq()
		r1, e1 := p.Assess(ctx, req)
		r2, e2 := p.Assess(ctx, req)
		srv.Close()
		eq := e1 == nil && e2 == nil &&
			r1.Assessment.SchemaVersion == r2.Assessment.SchemaVersion &&
			r1.Assessment.Severity == r2.Assessment.Severity &&
			r1.Assessment.ReasonCode == r2.Assessment.ReasonCode
		row := CacheEvalRow{Mode: ModeReinframeExactHit, ProviderKind: classifier.KindOpenAIResponses,
			ProviderCalls: int(n.Load()), CacheHit: r2.Usage.CacheHit, CacheBackend: r2.Usage.CacheBackend,
			Severity: r1.Assessment.Severity,
			OK:       eq && n.Load() == 1 && r2.Usage.CacheBackend == classifier.ExactCacheLayerReinframeExact}
		rep.Rows = append(rep.Rows, row)
	}

	// --- Singleflight N callers ---
	{
		var n atomic.Int32
		release := make(chan struct{})
		var started sync.WaitGroup
		started.Add(1)
		var once sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			once.Do(func() { started.Done() })
			<-release
			n.Add(1)
			_, _ = io.ReadAll(r.Body)
			_, _ = w.Write([]byte(okBody(11, 0)))
		}))
		inner, _ := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: 5 * time.Second,
		})
		ec, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
			Enabled: true, MaxEntries: 32, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: true,
		}, nil)
		p := classifier.WrapWithExactCache(inner, ec, classifier.ExactCacheIdentity{
			ProviderKind: classifier.KindOpenAIResponses, ModelID: "m",
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			ParserSchema:        classifier.SchemaRawAssessment,
		})
		req, _ := baseReq()
		const N = 8
		var wg sync.WaitGroup
		errs := make(chan error, N)
		wg.Add(N)
		for i := 0; i < N; i++ {
			go func() {
				defer wg.Done()
				_, err := p.Assess(ctx, req)
				errs <- err
			}()
		}
		started.Wait()
		close(release)
		wg.Wait()
		close(errs)
		ok := n.Load() == 1
		for e := range errs {
			if e != nil {
				ok = false
			}
		}
		srv.Close()
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeSingleflightN, ProviderKind: classifier.KindOpenAIResponses,
			ProviderCalls: int(n.Load()), OK: ok, Note: fmt.Sprintf("N=%d", N),
		})
	}

	// --- Required miss: model change (shared exact cache; different ModelID identity) ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = w.Write([]byte(okBody(10, 0)))
		}))
		ec, err := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
			Enabled: true, MaxEntries: 32, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
		}, nil)
		if err != nil {
			srv.Close()
			return rep, err
		}
		innerA, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "model-a", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		innerB, err := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "model-b", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		pA := classifier.WrapWithExactCache(innerA, ec, classifier.ExactCacheIdentity{
			ProviderKind: classifier.KindOpenAIResponses, ModelID: "model-a",
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1, ParserSchema: classifier.SchemaRawAssessment,
		})
		pB := classifier.WrapWithExactCache(innerB, ec, classifier.ExactCacheIdentity{
			ProviderKind: classifier.KindOpenAIResponses, ModelID: "model-b",
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1, ParserSchema: classifier.SchemaRawAssessment,
		})
		req, err := baseReq()
		if err != nil {
			srv.Close()
			return rep, err
		}
		_, _ = pA.Assess(ctx, req)
		_, _ = pB.Assess(ctx, req)
		srv.Close()
		ok := n.Load() == 2
		if !ok {
			rep.StaleHitRate = 1
		}
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeRequiredMissModel, ProviderKind: classifier.KindOpenAIResponses,
			ProviderCalls: int(n.Load()), OK: ok, Note: "model identity change must miss",
		})
	}

	// --- Required miss: event content change ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = w.Write([]byte(okBody(10, 0)))
		}))
		inner, _ := classifier.NewOpenAIResponses(classifier.OpenAIResponsesConfig{
			Model: "m", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		ec, _ := classifier.NewExactAssessmentCache(classifier.ExactCacheConfig{
			Enabled: true, MaxEntries: 32, MaxBytes: 1 << 20, TTL: time.Minute, Singleflight: false,
		}, nil)
		p := classifier.WrapWithExactCache(inner, ec, classifier.ExactCacheIdentity{
			ProviderKind: classifier.KindOpenAIResponses, ModelID: "m",
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIOffV1, ParserSchema: classifier.SchemaRawAssessment,
		})
		in1 := classifier.ClassifierInput{
			SchemaVersion: classifier.SchemaClassifierInput, PolicyClass: classifier.PolicyClassProductivity,
			RulesetID: "rs", RulesetHash: "rh", TaskAnchor: classifier.TaskAnchor{TaskID: "t", Objective: "o"},
			RecentEvents: []classifier.EventDigest{{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "a", ContentHash: "h1"}},
		}
		in2 := in1
		in2.RecentEvents = []classifier.EventDigest{{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "a", ContentHash: "h2"}}
		r1, _ := classifier.NewProviderRequest(in1)
		r2, _ := classifier.NewProviderRequest(in2)
		_, _ = p.Assess(ctx, r1)
		_, _ = p.Assess(ctx, r2)
		srv.Close()
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeRequiredMissEvents, ProviderKind: classifier.KindOpenAIResponses,
			ProviderCalls: int(n.Load()), OK: n.Load() == 2,
		})
	}

	// --- Anthropic warm read ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = w.Write([]byte(anthBody(12, 25)))
		}))
		p, err := classifier.NewAnthropicMessages(classifier.AnthropicMessagesConfig{
			Model: "claude", BaseURL: srv.URL, Path: "/v1/messages", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileAnthropicOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		req, _ := baseReq()
		res, err := p.Assess(ctx, req)
		srv.Close()
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeProviderCacheWarm, ProviderKind: classifier.KindAnthropicMessages,
			ProviderCalls: int(n.Load()), CacheHit: res.Usage.CacheHit, CacheReadTokens: res.Usage.CacheReadTokens,
			OK: err == nil && res.Usage.CacheHit && res.Usage.CacheReadTokens == 25,
		})
	}

	// --- Gemini warm read ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = w.Write([]byte(gemBody(12, 30)))
		}))
		p, err := classifier.NewGeminiGenerateContent(classifier.GeminiGenerateContentConfig{
			Model: "gemini", BaseURL: srv.URL, AllowRemote: true, HTTPClient: srv.Client(),
			CapabilitiesProfile: classifier.CapabilitiesProfileGeminiOffV1,
			APIKeyRef:           "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		req, _ := baseReq()
		res, err := p.Assess(ctx, req)
		srv.Close()
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeProviderCacheWarm, ProviderKind: classifier.KindGeminiGenerateContent,
			ProviderCalls: int(n.Load()), CacheHit: res.Usage.CacheHit, CacheReadTokens: res.Usage.CacheReadTokens,
			OK: err == nil && res.Usage.CacheHit && res.Usage.CacheReadTokens == 30,
		})
	}

	// --- xAI warm read ---
	{
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n.Add(1)
			_, _ = w.Write([]byte(okBody(12, 15)))
		}))
		p, err := classifier.NewXAIResponses(classifier.XAIResponsesConfig{
			Model: "grok", BaseURL: srv.URL, Path: "/v1/responses", AllowRemote: true,
			HTTPClient: srv.Client(), CapabilitiesProfile: classifier.CapabilitiesProfileXAIOffV1,
			APIKeyRef: "${K}", LookupEnv: func(string) (string, bool) { return "k", true },
			MaxRetries: 0, Timeout: time.Second,
		})
		if err != nil {
			srv.Close()
			return rep, err
		}
		req, _ := baseReq()
		res, err := p.Assess(ctx, req)
		srv.Close()
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeProviderCacheWarm, ProviderKind: classifier.KindXAIResponses,
			ProviderCalls: int(n.Load()), CacheHit: res.Usage.CacheHit, CacheReadTokens: res.Usage.CacheReadTokens,
			OK: err == nil && res.Usage.CacheHit && res.Usage.CacheReadTokens == 15,
		})
	}

	// --- Generic openai_compatible remains cache-neutral (rejects non-none profiles) ---
	{
		_, err := classifier.NewOpenAICompatible(classifier.OpenAICompatibleConfig{
			Model: "m", BaseURL: "http://127.0.0.1:1", Path: "/v1/chat/completions", AllowRemote: true,
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAIExplicitPrefixV1,
		})
		rep.Rows = append(rep.Rows, CacheEvalRow{
			Mode: ModeGenericCacheNeutral, ProviderKind: "openai_compatible",
			OK: err != nil, Note: "generic rejects openai cache profiles",
		})
	}

	// Aggregate
	allOK := true
	stale := 0
	for _, row := range rep.Rows {
		if !row.OK {
			allOK = false
			if (row.Mode == ModeRequiredMissModel || row.Mode == ModeRequiredMissEvents) && row.ProviderCalls < 2 {
				stale++
			}
		}
	}
	rep.AllModesOK = allOK
	if len(rep.Rows) > 0 {
		// Fraction of required-miss modes that failed as stale reuse; 0 when all OK.
		missModes := 0
		for _, row := range rep.Rows {
			if row.Mode == ModeRequiredMissModel || row.Mode == ModeRequiredMissEvents {
				missModes++
			}
		}
		if missModes > 0 {
			rep.StaleHitRate = float64(stale) / float64(missModes)
		}
	}
	// Invalid admission not exercised in this fake harness (errors are not cached by design).
	rep.InvalidAdmissionCount = -1 // not_measured sentinel for consumers
	if !allOK {
		rep.Disposition = "MORE-DATA"
		rep.DispositionNote += " One or more fake-CI rows failed; investigate before LIMITED-GO."
	}
	return rep, nil
}
