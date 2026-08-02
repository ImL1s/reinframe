package reviewer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
	"github.com/ImL1s/reinframe/pkg/reviewer"
)

func sampleRequest() protocol.ReviewRequest {
	return protocol.ReviewRequest{
		RequestID:      "req-1",
		ReviewerRole:   "TunnelClassifier",
		EvidencePackID: "ep-1",
		Model:          "fake-model",
		Prompt:         "classify session for tunnel vision",
		RequestedAt:    time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
	}
}

func TestFakeProvider_GenerateFillsDefaults(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()
	req := sampleRequest()

	dec, err := p.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if dec.RequestID != req.RequestID {
		t.Fatalf("RequestID = %q, want %q", dec.RequestID, req.RequestID)
	}
	if dec.ReviewerRole != req.ReviewerRole {
		t.Fatalf("ReviewerRole = %q, want %q", dec.ReviewerRole, req.ReviewerRole)
	}
	if dec.DecisionID == "" {
		t.Fatal("DecisionID should be filled")
	}
	if dec.Classification != "NORMAL_PROGRESS" {
		t.Fatalf("Classification = %q", dec.Classification)
	}
	if dec.DecidedAt.IsZero() {
		t.Fatal("DecidedAt should be set")
	}
	if p.CallCount() != 1 {
		t.Fatalf("CallCount = %d, want 1", p.CallCount())
	}
	last, ok := p.LastCall()
	if !ok || last.RequestID != "req-1" {
		t.Fatalf("LastCall = %#v ok=%v", last, ok)
	}
}

func TestFakeProvider_CustomDecisionAndErr(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()
	p.Decision = protocol.ReviewDecision{
		DecisionID:       "d-99",
		Classification:   "TUNNEL_VISION",
		TunnelConfidence: 0.91,
		Rationale:        "repeated error fingerprint",
		SuggestedAdvice:  "ZOOM_OUT and replan",
		TokensUsed:       12,
		DecidedAt:        time.Date(2026, 8, 2, 13, 0, 0, 0, time.UTC),
	}

	dec, err := p.Generate(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if dec.Classification != "TUNNEL_VISION" || dec.TunnelConfidence != 0.91 {
		t.Fatalf("unexpected decision: %#v", dec)
	}
	if dec.SuggestedAdvice != "ZOOM_OUT and replan" {
		t.Fatalf("SuggestedAdvice = %q", dec.SuggestedAdvice)
	}

	wantErr := errors.New("provider down")
	p.Err = wantErr
	_, err = p.Generate(context.Background(), sampleRequest())
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

func TestFakeProvider_GenerateFuncOverride(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()
	p.Err = errors.New("should not see this")
	p.GenerateFunc = func(ctx context.Context, req protocol.ReviewRequest) (protocol.ReviewDecision, error) {
		return protocol.ReviewDecision{
			DecisionID:     "from-func",
			RequestID:      req.RequestID,
			ReviewerRole:   req.ReviewerRole,
			Classification: "SCOPE_DRIFT",
			Rationale:      "func override",
			TokensUsed:     1,
			DecidedAt:      time.Now().UTC(),
		}, nil
	}

	dec, err := p.Generate(context.Background(), sampleRequest())
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if dec.DecisionID != "from-func" || dec.Classification != "SCOPE_DRIFT" {
		t.Fatalf("unexpected: %#v", dec)
	}
}

func TestFakeProvider_NilContextAndEmptyRequestID(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()

	_, err := p.Generate(nil, sampleRequest()) //nolint:staticcheck // intentional nil ctx
	if !errors.Is(err, reviewer.ErrNilContext) {
		t.Fatalf("nil ctx err = %v", err)
	}

	req := sampleRequest()
	req.RequestID = ""
	_, err = p.Generate(context.Background(), req)
	if !errors.Is(err, reviewer.ErrEmptyRequestID) {
		t.Fatalf("empty request_id err = %v", err)
	}
}

func TestFakeProvider_ContextCanceled(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := p.Generate(ctx, sampleRequest())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestFakeProvider_Reset(t *testing.T) {
	t.Parallel()
	p := reviewer.NewFakeProvider()
	if _, err := p.Generate(context.Background(), sampleRequest()); err != nil {
		t.Fatal(err)
	}
	p.Reset()
	if p.CallCount() != 0 {
		t.Fatalf("CallCount after Reset = %d", p.CallCount())
	}
}

func TestReviewerProviderInterfaceAssignment(t *testing.T) {
	t.Parallel()
	var _ reviewer.ReviewerProvider = reviewer.NewFakeProvider()
}
