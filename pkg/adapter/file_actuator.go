package adapter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

// FileActuator is a non-fake InterventionActuator that appends interventions as
// JSON lines to a local file (or writes to a writer). Target harnesses (or a
// local IPC stub) can tail the file for ZOOM_OUT_PROMPT advice.
//
// #97 product-shaped channel: file/JSONL. Does not inject into a live Claude/Codex
// process by itself — CapAdviceDelivery still requires a host consumer of this file.
//
// Deliver returns Accepted=true with AckStatus=pending (requires explicit ACK),
// matching honest advisory lifecycle (#81). Auto-ack theater is not used.
type FileActuator struct {
	// Path is the JSONL advice channel. Required unless WriteFile is set for tests.
	Path string
	// Now overrides clock (tests).
	Now func() time.Time

	// writeMu serializes appends.
	writeMu sync.Mutex
	// lastLine is the most recent written JSON (tests).
	lastLine string
	// callCount increments on each Deliver.
	callCount int
}

// AdviceEnvelope is one JSONL record written by FileActuator.
type AdviceEnvelope struct {
	Schema         string `json:"schema"` // reinframe.advice.v1
	InterventionID string `json:"intervention_id"`
	SessionID      string `json:"session_id"`
	ActionType     string `json:"action_type"`
	AdvicePrompt   string `json:"advice_prompt,omitempty"`
	DeliveryMode   string `json:"delivery_mode"`
	RequiresAck    bool   `json:"requires_ack"`
	DeliveredAt    string `json:"delivered_at"`
	Fingerprint    string `json:"fingerprint,omitempty"`
}

// Deliver implements InterventionActuator.
func (f *FileActuator) Deliver(ctx context.Context, intervention protocol.Intervention) (InterventionResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		now := f.now()
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   DefaultDeliveryMode(intervention.ActionType),
			DeliveredAt:    now,
			AckStatus:      AckStatusTimedOut,
			ErrorClass:     ErrorClassTimeout,
			Message:        "context done before deliver",
		}, err
	}

	mode := DefaultDeliveryMode(intervention.ActionType)
	if intervention.DeliveryModeHint != "" {
		mode = intervention.DeliveryModeHint
	}
	now := f.now()
	env := AdviceEnvelope{
		Schema:         "reinframe.advice.v1",
		InterventionID: intervention.InterventionID,
		SessionID:      intervention.SessionID,
		ActionType:     intervention.ActionType,
		AdvicePrompt:   intervention.AdvicePrompt,
		DeliveryMode:   mode,
		RequiresAck:    true,
		DeliveredAt:    now.Format(time.RFC3339Nano),
		Fingerprint:    intervention.Fingerprint,
	}
	line, err := json.Marshal(env)
	if err != nil {
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   mode,
			DeliveredAt:    now,
			AckStatus:      AckStatusRejected,
			ErrorClass:     ErrorClassTransport,
			Message:        err.Error(),
		}, err
	}
	line = append(line, '\n')

	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if f.Path == "" {
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   mode,
			DeliveredAt:    now,
			AckStatus:      AckStatusUnsupported,
			ErrorClass:     ErrorClassTransport,
			Message:        "file actuator: Path required",
		}, fmt.Errorf("file actuator: Path required")
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil && !os.IsExist(err) {
		// Dir may be "." — MkdirAll(".") is fine; if Path has no dir, Dir is "."
		if filepath.Dir(f.Path) != "." {
			return InterventionResult{
				InterventionID: intervention.InterventionID,
				Accepted:       false,
				DeliveryMode:   mode,
				DeliveredAt:    now,
				AckStatus:      AckStatusRejected,
				ErrorClass:     ErrorClassTransport,
				Message:        err.Error(),
			}, err
		}
	}
	fh, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   mode,
			DeliveredAt:    now,
			AckStatus:      AckStatusRejected,
			ErrorClass:     ErrorClassTransport,
			Message:        err.Error(),
		}, err
	}
	defer func() { _ = fh.Close() }()
	if _, err := fh.Write(line); err != nil {
		return InterventionResult{
			InterventionID: intervention.InterventionID,
			Accepted:       false,
			DeliveryMode:   mode,
			DeliveredAt:    now,
			AckStatus:      AckStatusRejected,
			ErrorClass:     ErrorClassTransport,
			Message:        err.Error(),
		}, err
	}
	f.lastLine = string(line)
	f.callCount++

	return InterventionResult{
		InterventionID: intervention.InterventionID,
		Accepted:       true,
		DeliveryMode:   mode,
		DeliveredAt:    now,
		AckStatus:      AckStatusPending, // explicit ACK required
		ErrorClass:     ErrorClassNone,
		Message:        "written to advice channel",
	}, nil
}

// CallCount returns successful Deliver writes (tests).
func (f *FileActuator) CallCount() int {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	return f.callCount
}

// LastLine returns the last written JSONL line including newline (tests).
func (f *FileActuator) LastLine() string {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	return f.lastLine
}

func (f *FileActuator) now() time.Time {
	if f.Now != nil {
		return f.Now().UTC()
	}
	return time.Now().UTC()
}
