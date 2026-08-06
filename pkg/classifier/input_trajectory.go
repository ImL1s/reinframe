package classifier

import (
	"fmt"
	"sort"
)

// Trajectory bounds for recent-N event packets (#132 / wire contract §5).
// Defaults: N=40 events, B=48KiB (docs/specs/action_alignment_wire_contract.md).
const (
	MaxRecentEvents        = 40 // total recent+related window cap (wire default N)
	MaxRelatedEvents       = 32 // related may not alone exceed this within N
	MaxEventSummaryBytes   = 512
	MaxEventTypeBytes      = 64
	MaxEventIDBytes        = 128
	MaxContentHashBytes    = 128
	MaxTaskIDBytes         = 128
	MaxObjectiveBytes      = 1024
	MaxAcceptanceItems     = 16
	MaxAcceptanceItemBytes = 256
	MaxTrajectoryBytes     = 48 << 10 // ~48 KiB total digest payload
)

// Closed overflow markers for WindowMeta.
const (
	OverflowNone           = ""
	OverflowEvents         = "events"
	OverflowBytes          = "bytes"
	OverflowEventsAndBytes = "events_and_bytes"
)

// Closed event type allowlist (provider-neutral).
var ValidEventTypes = map[string]struct{}{
	"tool_call":    {},
	"tool_result":  {},
	"user_message": {},
	"assistant":    {},
	"observation":  {},
	"error":        {},
	"system":       {},
	"unknown":      {},
}

// TaskAnchor is the pinned task objective and acceptance conditions.
type TaskAnchor struct {
	TaskID     string
	Objective  string
	Acceptance []string
}

// EventDigest is a bounded redacted semantic summary of one event (not opaque ID only).
type EventDigest struct {
	EventID     string
	Sequence    uint64
	EventType   string
	Summary     string
	ContentHash string
	RelatedTo   string
}

// WindowMeta describes truncation of the recent-N trajectory packet.
type WindowMeta struct {
	EventCount     int
	ByteCount      int
	Truncated      bool
	OverflowMarker string
}

// ValidateTaskAnchor checks closed bounds.
func ValidateTaskAnchor(t TaskAnchor) error {
	if len(t.TaskID) > MaxTaskIDBytes {
		return fmt.Errorf("classifier: task_id too long")
	}
	if len(t.Objective) > MaxObjectiveBytes {
		return fmt.Errorf("classifier: objective too long")
	}
	if len(t.Acceptance) > MaxAcceptanceItems {
		return fmt.Errorf("classifier: too many acceptance items")
	}
	for _, a := range t.Acceptance {
		if len(a) > MaxAcceptanceItemBytes {
			return fmt.Errorf("classifier: acceptance item too long")
		}
	}
	return nil
}

// ValidateEventDigests enforces bounds, closed types, deterministic order, uniqueness.
func ValidateEventDigests(events []EventDigest, maxN int) error {
	if maxN <= 0 {
		maxN = MaxRecentEvents
	}
	if len(events) > maxN {
		return fmt.Errorf("classifier: too many events")
	}
	seen := map[string]struct{}{}
	var prevSeq uint64
	for i, e := range events {
		if e.EventID == "" || len(e.EventID) > MaxEventIDBytes {
			return fmt.Errorf("classifier: invalid event_id")
		}
		if _, dup := seen[e.EventID]; dup {
			return fmt.Errorf("classifier: duplicate event_id")
		}
		seen[e.EventID] = struct{}{}
		if len(e.EventType) > MaxEventTypeBytes {
			return fmt.Errorf("classifier: event_type too long")
		}
		if e.EventType != "" {
			if _, ok := ValidEventTypes[e.EventType]; !ok {
				return fmt.Errorf("classifier: unknown event_type")
			}
		}
		if len(e.Summary) > MaxEventSummaryBytes {
			return fmt.Errorf("classifier: event summary too long")
		}
		if len(e.ContentHash) > MaxContentHashBytes {
			return fmt.Errorf("classifier: content_hash too long")
		}
		if i > 0 && e.Sequence < prevSeq {
			return fmt.Errorf("classifier: event sequence not deterministic ascending")
		}
		prevSeq = e.Sequence
	}
	return nil
}

// ValidateWindowMeta checks consistency of truncation marker.
func ValidateWindowMeta(w WindowMeta) error {
	if w.EventCount < 0 || w.ByteCount < 0 {
		return fmt.Errorf("classifier: invalid window counts")
	}
	switch w.OverflowMarker {
	case OverflowNone, OverflowEvents, OverflowBytes, OverflowEventsAndBytes:
	default:
		return fmt.Errorf("classifier: unknown overflow_marker")
	}
	if w.Truncated && w.OverflowMarker == OverflowNone {
		return fmt.Errorf("classifier: truncated window requires overflow_marker")
	}
	if !w.Truncated && w.OverflowMarker != OverflowNone {
		return fmt.Errorf("classifier: overflow_marker without truncated")
	}
	return nil
}

// SortEventDigests sorts by Sequence then EventID for deterministic hashing.
func SortEventDigests(events []EventDigest) {
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].Sequence != events[j].Sequence {
			return events[i].Sequence < events[j].Sequence
		}
		return events[i].EventID < events[j].EventID
	})
}

// BoundTrajectory enforces max N events and max B bytes on recent+related digests.
// Returns truncated copies and filled WindowMeta.
func BoundTrajectory(recent, related []EventDigest, maxN, maxB int) (outRecent, outRelated []EventDigest, win WindowMeta) {
	if maxN <= 0 {
		maxN = MaxRecentEvents
	}
	if maxB <= 0 {
		maxB = MaxTrajectoryBytes
	}
	SortEventDigests(recent)
	SortEventDigests(related)

	eventsOverflow := false
	bytesOverflow := false
	byteCount := 0
	take := func(src []EventDigest, limit int) []EventDigest {
		var out []EventDigest
		for _, e := range src {
			if len(out) >= limit {
				eventsOverflow = true
				break
			}
			sz := len(e.EventID) + len(e.EventType) + len(e.Summary) + len(e.ContentHash) + len(e.RelatedTo) + 16
			if byteCount+sz > maxB {
				bytesOverflow = true
				break
			}
			out = append(out, e)
			byteCount += sz
		}
		return out
	}
	// Prefer recent first, then related within remaining budget.
	outRecent = take(recent, maxN)
	remain := maxN - len(outRecent)
	if remain < 0 {
		remain = 0
	}
	if remain > MaxRelatedEvents {
		remain = MaxRelatedEvents
	}
	outRelated = take(related, remain)

	win.EventCount = len(outRecent) + len(outRelated)
	win.ByteCount = byteCount
	sourceOverflow := len(recent) > len(outRecent) || len(related) > len(outRelated)
	// Pure count truncation (maxN) is an events overflow even if the take-loop
	// exited on limit without setting eventsOverflow mid-iteration edge cases.
	if sourceOverflow && !bytesOverflow {
		eventsOverflow = true
	}
	win.Truncated = eventsOverflow || bytesOverflow || sourceOverflow
	switch {
	case eventsOverflow && bytesOverflow:
		win.OverflowMarker = OverflowEventsAndBytes
	case eventsOverflow:
		win.OverflowMarker = OverflowEvents
	case bytesOverflow:
		win.OverflowMarker = OverflowBytes
	case sourceOverflow:
		win.OverflowMarker = OverflowEvents
	}
	return outRecent, outRelated, win
}

// EvidenceAllowlistFromDigests returns IDs actually shown to the provider.
func EvidenceAllowlistFromDigests(recent, related []EventDigest) map[string]struct{} {
	m := make(map[string]struct{}, len(recent)+len(related))
	for _, e := range recent {
		m[e.EventID] = struct{}{}
	}
	for _, e := range related {
		m[e.EventID] = struct{}{}
	}
	return m
}

// DigestByteCount returns the canonical byte size used by BoundTrajectory.
func DigestByteCount(events []EventDigest) int {
	n := 0
	for _, e := range events {
		n += len(e.EventID) + len(e.EventType) + len(e.Summary) + len(e.ContentHash) + len(e.RelatedTo) + 16
	}
	return n
}

// MergeWindowMeta merges upstream provenance with local N/B bounding results.
// Never clears upstream Truncated merely because the already-reduced slice fits.
func MergeWindowMeta(upstream, local WindowMeta, recent, related []EventDigest) WindowMeta {
	out := WindowMeta{
		EventCount: len(recent) + len(related),
		ByteCount:  DigestByteCount(recent) + DigestByteCount(related),
	}
	// Prefer exact shown counts from local when local computed them; else recompute.
	if local.EventCount > 0 || local.ByteCount > 0 || local.Truncated {
		out.EventCount = local.EventCount
		out.ByteCount = local.ByteCount
	}
	out.Truncated = upstream.Truncated || local.Truncated
	out.OverflowMarker = mergeOverflowMarkers(upstream.OverflowMarker, local.OverflowMarker)
	if out.Truncated && out.OverflowMarker == OverflowNone {
		// Prefer local marker, then upstream, then events as safe default.
		if local.OverflowMarker != OverflowNone {
			out.OverflowMarker = local.OverflowMarker
		} else if upstream.OverflowMarker != OverflowNone {
			out.OverflowMarker = upstream.OverflowMarker
		} else {
			out.OverflowMarker = OverflowEvents
		}
	}
	if !out.Truncated {
		out.OverflowMarker = OverflowNone
	}
	return out
}

func mergeOverflowMarkers(a, b string) string {
	if a == OverflowNone {
		return b
	}
	if b == OverflowNone {
		return a
	}
	if a == b {
		return a
	}
	// events + bytes → events_and_bytes
	hasE := a == OverflowEvents || a == OverflowEventsAndBytes || b == OverflowEvents || b == OverflowEventsAndBytes
	hasB := a == OverflowBytes || a == OverflowEventsAndBytes || b == OverflowBytes || b == OverflowEventsAndBytes
	if hasE && hasB {
		return OverflowEventsAndBytes
	}
	if hasE {
		return OverflowEvents
	}
	if hasB {
		return OverflowBytes
	}
	return a
}

// ValidateLegacyFixtureIDs enforces closed bounds for fixture-only ID lists.
func ValidateLegacyFixtureIDs(recent, related []string) error {
	const maxID = MaxEventIDBytes
	const maxN = MaxRecentEvents
	if len(recent) > maxN || len(related) > maxN {
		return fmt.Errorf("classifier: too many legacy event ids")
	}
	seen := map[string]struct{}{}
	check := func(ids []string) error {
		for _, id := range ids {
			if id == "" || len(id) > maxID {
				return fmt.Errorf("classifier: invalid legacy event id")
			}
			if _, dup := seen[id]; dup {
				return fmt.Errorf("classifier: duplicate legacy event id")
			}
			seen[id] = struct{}{}
		}
		return nil
	}
	if err := check(recent); err != nil {
		return err
	}
	if err := check(related); err != nil {
		return err
	}
	return nil
}

// ValidateWindowMetaExact cross-checks Window against shown digests.
func ValidateWindowMetaExact(w WindowMeta, recent, related []EventDigest) error {
	if err := ValidateWindowMeta(w); err != nil {
		return err
	}
	wantN := len(recent) + len(related)
	wantB := DigestByteCount(recent) + DigestByteCount(related)
	if w.EventCount != wantN {
		return fmt.Errorf("classifier: window event_count mismatch")
	}
	if w.ByteCount != wantB {
		return fmt.Errorf("classifier: window byte_count mismatch")
	}
	return nil
}
