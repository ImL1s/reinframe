package reviewer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// ReviewerProvider generates a structured ReviewDecision for a ReviewRequest.
//
// Implementations must:
//   - Accept only protocol.ReviewRequest / return protocol.ReviewDecision
//   - Respect ctx cancellation
//   - For non-local backends, redact secret-class material before egress (ADR 003)
type ReviewerProvider interface {
	Generate(ctx context.Context, req protocol.ReviewRequest) (protocol.ReviewDecision, error)
}

// ErrNilContext is returned when Generate is called with a nil context.
var ErrNilContext = errors.New("reviewer: context is nil")

// ErrEmptyRequestID is returned when the request lacks a request_id.
var ErrEmptyRequestID = errors.New("reviewer: request_id is required")

// FakeProvider is a thread-safe in-memory ReviewerProvider for unit tests.
//
// Priority:
//  1. If GenerateFunc is non-nil, it is invoked.
//  2. Else if Err is non-nil, that error is returned.
//  3. Else Decision is returned (RequestID / ReviewerRole / DecidedAt filled when empty).
type FakeProvider struct {
	mu sync.Mutex

	// Decision is the canned response when GenerateFunc and Err are unset.
	Decision protocol.ReviewDecision
	// Err is returned from Generate when GenerateFunc is nil and Err != nil.
	Err error
	// GenerateFunc overrides Decision/Err when non-nil.
	GenerateFunc func(ctx context.Context, req protocol.ReviewRequest) (protocol.ReviewDecision, error)

	// Calls records every request passed to Generate (in order).
	Calls []protocol.ReviewRequest
}

// NewFakeProvider returns a FakeProvider that classifies as NORMAL_PROGRESS.
func NewFakeProvider() *FakeProvider {
	return &FakeProvider{
		Decision: protocol.ReviewDecision{
			Classification:   "NORMAL_PROGRESS",
			TunnelConfidence: 0.0,
			Rationale:        "fake provider default: normal progress",
			TokensUsed:       0,
		},
	}
}

// Generate implements ReviewerProvider.
func (f *FakeProvider) Generate(ctx context.Context, req protocol.ReviewRequest) (protocol.ReviewDecision, error) {
	if ctx == nil {
		return protocol.ReviewDecision{}, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return protocol.ReviewDecision{}, err
	}
	if req.RequestID == "" {
		return protocol.ReviewDecision{}, ErrEmptyRequestID
	}

	f.mu.Lock()
	f.Calls = append(f.Calls, req)
	gen := f.GenerateFunc
	cannedErr := f.Err
	decision := f.Decision
	f.mu.Unlock()

	if gen != nil {
		return gen(ctx, req)
	}
	if cannedErr != nil {
		return protocol.ReviewDecision{}, cannedErr
	}

	out := decision
	if out.DecisionID == "" {
		out.DecisionID = fmt.Sprintf("fake-decision-%s", req.RequestID)
	}
	if out.RequestID == "" {
		out.RequestID = req.RequestID
	}
	if out.ReviewerRole == "" {
		out.ReviewerRole = req.ReviewerRole
	}
	if out.Rationale == "" {
		out.Rationale = "fake provider"
	}
	if out.DecidedAt.IsZero() {
		out.DecidedAt = time.Now().UTC()
	}
	return out, nil
}

// CallCount returns how many times Generate was invoked.
func (f *FakeProvider) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.Calls)
}

// LastCall returns the most recent request, or false if none.
func (f *FakeProvider) LastCall() (protocol.ReviewRequest, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Calls) == 0 {
		return protocol.ReviewRequest{}, false
	}
	return f.Calls[len(f.Calls)-1], true
}

// Reset clears recorded calls (does not clear Decision/Err/GenerateFunc).
func (f *FakeProvider) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = nil
}

// Compile-time check.
var _ ReviewerProvider = (*FakeProvider)(nil)
