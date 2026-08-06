package classifier

import (
	"context"
	"errors"
)

// classifyOpError distinguishes parent context abort from adapter-owned timeout
// and ordinary runtime failures. Never returns nil when err is non-nil.
//
//	parent canceled/deadline  → raw parent error (propagated)
//	adapter-owned timeout     → ProviderError{Class: "timeout"}
//	ordinary failure          → ProviderError or original non-context error
func classifyOpError(parent, op context.Context, err error) error {
	// Prefer parent identity when parent is aborted.
	if parent != nil {
		if perr := parent.Err(); perr != nil {
			return perr
		}
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			// Parent still live but canceled signal — treat as cancel if parent now canceled,
			// else propagate cancel (request canceled via opCtx inheritance).
			if parent != nil && parent.Err() != nil {
				return parent.Err()
			}
			return err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			// Adapter-owned overall budget while parent remains live.
			if parent == nil || parent.Err() == nil {
				return newProviderError("timeout", "provider request timeout", false, 0)
			}
			return parent.Err()
		}
		// Ordinary non-context error — keep typed ProviderError if present.
		var pe *ProviderError
		if errors.As(err, &pe) {
			return pe
		}
		return newProviderError("transport", "operation failed", false, 0)
	}
	// err == nil: check derived op context (e.g. deadline not yet on err arg).
	if op != nil {
		if oerr := op.Err(); oerr != nil {
			if parent != nil && parent.Err() != nil {
				return parent.Err()
			}
			if errors.Is(oerr, context.DeadlineExceeded) {
				return newProviderError("timeout", "provider request timeout", false, 0)
			}
			return oerr
		}
	}
	return nil
}

// isParentContextAbort reports raw parent cancel/deadline (not adapter timeout).
func isParentContextAbort(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// isAdapterTimeout reports typed adapter-owned timeout.
func isAdapterTimeout(err error) bool {
	var pe *ProviderError
	return errors.As(err, &pe) && pe != nil && pe.Class == "timeout"
}
