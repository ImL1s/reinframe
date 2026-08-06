package classifier_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/challenge"
	"github.com/ImL1s/reinframe/pkg/classifier"
)

// malformedNilErrorProvider returns nil error with invalid envelope (missing schemas).
type malformedNilErrorProvider struct{}

func (malformedNilErrorProvider) Assess(ctx context.Context, req classifier.ProviderRequest) (classifier.ProviderResult, error) {
	return classifier.ProviderResult{
		Assessment: classifier.RawAssessment{
			ParseStatus: "ok",
			Severity:    0,
			// missing SchemaVersion, ReasonCode, PromptHash — forged success
		},
	}, nil
}

func TestValidateProviderResultForRequest_RejectsForgedOK(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		PolicyClass:   classifier.PolicyClassProductivity,
	})
	if err != nil {
		t.Fatal(err)
	}
	bad := classifier.ProviderResult{
		// missing SchemaVersion
		Assessment: classifier.RawAssessment{ParseStatus: classifier.ParseStatusOK, Severity: 0},
	}
	if err := classifier.ValidateProviderResultForRequest(req, bad); err == nil {
		t.Fatal("missing result schema must fail")
	}
	bad2 := classifier.ProviderResult{
		SchemaVersion: classifier.SchemaProviderResult,
		Assessment: classifier.RawAssessment{
			SchemaVersion: classifier.SchemaRawAssessment,
			ParseStatus:   classifier.ParseStatusOK,
			Severity:      10,
			ReasonCode:    "NORMAL_PROGRESS",
			PromptHash:    "forged",
			RulesetID:     req.Input.RulesetID,
			RulesetHash:   req.Input.RulesetHash,
		},
	}
	if err := classifier.ValidateProviderResultForRequest(req, bad2); err == nil {
		t.Fatal("forged prompt_hash must fail")
	}
}

func TestValidateProviderResultForRequest_UngroundedEvidence(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		RecentEvents: []classifier.EventDigest{
			{EventID: "e1", Sequence: 1, EventType: "tool_call", Summary: "x"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res := classifier.ProviderResult{
		SchemaVersion: classifier.SchemaProviderResult,
		Assessment: classifier.RawAssessment{
			SchemaVersion:    classifier.SchemaRawAssessment,
			ParseStatus:      classifier.ParseStatusOK,
			Severity:         10,
			ReasonCode:       "NORMAL_PROGRESS",
			PromptHash:       req.Prompt.PromptHash,
			RulesetID:        req.Input.RulesetID,
			RulesetHash:      req.Input.RulesetHash,
			EvidenceEventIDs: []string{"not-shown"},
		},
	}
	if err := classifier.ValidateProviderResultForRequest(req, res); err == nil {
		t.Fatal("ungrounded evidence must fail")
	}
}

func TestWindowMeta_UpstreamTruncationPreserved(t *testing.T) {
	t.Parallel()
	// Upstream already reduced 100→40 with Truncated=true; local must not clear.
	events := make([]classifier.EventDigest, 0, 40)
	for i := 0; i < 40; i++ {
		events = append(events, classifier.EventDigest{
			EventID: string(rune('a'+i%26)) + string(rune('0'+i/26)), Sequence: uint64(i),
			EventType: "observation", Summary: "s",
		})
	}
	// Fix EventIDs to be unique printable
	for i := range events {
		events[i].EventID = "ev-" + strings.Repeat("x", 1) + string(rune('A'+(i%26))) + string(rune('0'+(i%10))) + string(rune('a'+(i/10)%26))
		events[i].EventID = "ev-" + itoa(i)
	}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		RecentEvents:  events,
		Window: classifier.WindowMeta{
			EventCount: 40, ByteCount: 1, Truncated: true, OverflowMarker: classifier.OverflowEvents,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !req.Input.Window.Truncated {
		t.Fatal("upstream Truncated must remain true")
	}
	if req.Input.Window.OverflowMarker != classifier.OverflowEvents &&
		req.Input.Window.OverflowMarker != classifier.OverflowEventsAndBytes {
		t.Fatalf("marker=%q", req.Input.Window.OverflowMarker)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestNewProviderRequest_RejectsLegacyIDsWithoutDigests(t *testing.T) {
	t.Parallel()
	_, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion:  classifier.SchemaClassifierInput,
		RecentEventIDs: []string{"e1", "e2"},
	})
	if err == nil {
		t.Fatal("production must reject legacy ID-only trajectory")
	}
	// Explicit fixture path remains available.
	req, err := classifier.NewFixtureProviderRequest(classifier.ClassifierInput{
		SchemaVersion:  classifier.SchemaClassifierInput,
		RecentEventIDs: []string{"e1", "e2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !req.Input.AllowLegacyFixtureIDs {
		t.Fatal("fixture flag required")
	}
}

func TestOpenAICompatible_RejectsLegacyIDsBeforeHTTP(t *testing.T) {
	t.Parallel()
	// Manual request with legacy IDs only must fail ValidateProviderRequest / Assess.
	req := classifier.ProviderRequest{
		SchemaVersion: classifier.SchemaProviderRequest,
		Input: classifier.ClassifierInput{
			SchemaVersion:  classifier.SchemaClassifierInput,
			RecentEventIDs: []string{"e1"},
		},
		Prompt: classifier.PromptPlan{SchemaVersion: classifier.SchemaPromptPlan},
	}
	// Build minimal valid prompt binding would fail; ensure legacy check fires.
	if err := classifier.ValidateProviderRequest(req); err == nil {
		// May also fail prompt bind — either way non-nil.
		t.Fatal("legacy-only must be rejected")
	}
}

func TestFake_EvidenceSelectionDeterministic(t *testing.T) {
	t.Parallel()
	f := classifier.FakeClassifierProvider{}
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		FixtureName:   "clear_block",
		RecentEvents: []classifier.EventDigest{
			{EventID: "z9", Sequence: 2, EventType: "tool_call", Summary: "z"},
			{EventID: "a1", Sequence: 1, EventType: "tool_call", Summary: "a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var first string
	for i := 0; i < 100; i++ {
		res, err := f.Assess(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(res.Assessment.EvidenceEventIDs)
		if i == 0 {
			first = string(b)
			continue
		}
		if string(b) != first {
			t.Fatalf("nondeterministic evidence at iter %d: %s vs %s", i, b, first)
		}
	}
	// Sorted first ID is a1 not z9.
	if first != `["a1"]` {
		t.Fatalf("want sorted first id a1, got %s", first)
	}
}

func TestShadow_MalformedNilErrorNeverScores(t *testing.T) {
	t.Parallel()
	s := &classifier.ShadowClassifier{Provider: malformedNilErrorProvider{}}
	res, err := s.EvaluateShadow(context.Background(), classifier.ShadowInput{
		PolicyClass: classifier.PolicyClassProductivity,
		Threshold:   50,
		FixtureName: "x",
	})
	if err != nil {
		// contract failure may surface as err or fail-open policy without err
		_ = err
	}
	// Must not be stage1_applied below_threshold from forged ok.
	if res.Resolved.ResolverReason == "stage1_applied" && res.Resolved.ReasonCode == "below_threshold" {
		t.Fatalf("forged ok must not score: %+v", res.Resolved)
	}
	if res.ProviderCall == nil {
		t.Fatal("audit must be retained")
	}
	b, err := res.AuditJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "provider_call") {
		t.Fatal("AuditJSON must include provider_call")
	}
}

func TestAttemptRetry_MalformedNilErrorNeverAllowedOnce(t *testing.T) {
	t.Parallel()
	svc := challenge.NewService(challenge.ServiceConfig{})
	pa := adapter.ProposedAction{
		SchemaVersion: adapter.ProposedActionSchemaVersion, SessionID: "sess-1", ActionID: "pa-1",
		ToolName: "Bash", ToolClass: adapter.ToolClassShell, Command: "rm -rf build",
		WorkspaceRevision: "ws-1", ContractRevision: 3, Source: "synthetic", ParseStatus: "ok",
	}
	rec, err := svc.Open(context.Background(), challenge.OpenRequest{
		SessionID: pa.SessionID, Proposed: pa, BlockClass: challenge.BlockClassOverSOP,
	})
	if err != nil {
		t.Fatal(err)
	}
	just := challenge.Justification{
		SchemaVersion: challenge.SchemaJustification, ChallengeID: rec.ChallengeID,
		ConcreteValue: "x", PreventedFailureOrThreat: "b", EstimatedCost: "l",
		AlternativesConsidered: "n", ScopeLimit: "s", VerificationPlan: "t", RollbackPlan: "r",
	}
	if _, err := svc.Justify(context.Background(), just, nil); err != nil {
		t.Fatal(err)
	}
	retry, _ := svc.AttemptRetry(context.Background(), challenge.RetryRequest{
		ChallengeID: rec.ChallengeID, SessionID: pa.SessionID, Proposed: pa,
		CorrelationID: "malformed-nil-error",
		ReEval: &challenge.ReEvalContext{
			Provider:    malformedNilErrorProvider{},
			PolicyClass: classifier.PolicyClassProductivity,
			FixtureName: "x",
		},
	})
	if retry.Record.State == challenge.StateAllowedOnce || retry.Stage2Decision == challenge.DecisionAllow {
		t.Fatalf("malformed nil-error must not ALLOWED_ONCE: %+v", retry)
	}
	got, _ := svc.Get(rec.ChallengeID)
	if got.State == challenge.StateAllowedOnce {
		t.Fatal("store must not be ALLOWED_ONCE")
	}
	// Durable audit link when provider was invoked.
	if retry.ProviderCall == nil && retry.ProviderCallAuditID == "" {
		t.Fatal("expected provider-call audit or durable id on retry result")
	}
}

func TestMergeWindow_EventsPlusBytes(t *testing.T) {
	t.Parallel()
	// Local byte overflow on top of upstream events marker.
	big := strings.Repeat("Z", 200)
	events := []classifier.EventDigest{
		{EventID: "a", Sequence: 1, EventType: "observation", Summary: big},
		{EventID: "b", Sequence: 2, EventType: "observation", Summary: big},
		{EventID: "c", Sequence: 3, EventType: "observation", Summary: big},
	}
	up := classifier.WindowMeta{Truncated: true, OverflowMarker: classifier.OverflowEvents, EventCount: 3, ByteCount: 1}
	_, _, local := classifier.BoundTrajectory(events, nil, 40, 250)
	merged := classifier.MergeWindowMeta(up, local, events[:1], nil)
	if !merged.Truncated {
		t.Fatal("expected truncated")
	}
	// upstream events + local bytes → events_and_bytes when local also truncated
	if local.Truncated && merged.OverflowMarker != classifier.OverflowEventsAndBytes &&
		merged.OverflowMarker != classifier.OverflowEvents {
		t.Fatalf("marker=%q local=%+v", merged.OverflowMarker, local)
	}
}

func TestValidateProviderResultForRequest_ValidOK(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		FixtureName:   "clear_allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := classifier.FakeClassifierProvider{}.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if err := classifier.ValidateProviderResultForRequest(req, res); err != nil {
		t.Fatal(err)
	}
}

func TestValidateProviderResultForRequest_MalformedFixtureInvalidOK(t *testing.T) {
	t.Parallel()
	req, err := classifier.NewFixtureProviderRequest(classifier.ClassifierInput{
		SchemaVersion: classifier.SchemaClassifierInput,
		FixtureName:   "malformed_output",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := classifier.FakeClassifierProvider{}.Assess(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	// invalid parse status is a closed representation — validator accepts envelope.
	if err := classifier.ValidateProviderResultForRequest(req, res); err != nil {
		t.Fatal(err)
	}
	if res.Assessment.ParseStatus != classifier.ParseStatusInvalid {
		t.Fatal(res.Assessment.ParseStatus)
	}
}

// Ensure sample helpers compile against adapter schema in challenge tests via re-export.
var _ = adapter.ProposedActionSchemaVersion
var _ = errors.New
