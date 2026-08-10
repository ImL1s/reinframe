package main

import (
	"regexp"
	"runtime/debug"
	"strings"
)

// reinframeBuildIdentity returns the commit bound to the running binary.
// Prefer Go VCS build info / -ldflags -X; never ambient CWD git HEAD.
//
// Release builds should set both:
//
//	go build -ldflags "-X main.reinframeCommit=$(git rev-parse HEAD) -X main.reinframeDirty=false"
//
// reinframeDirty must be explicitly "false" for a clean ldflags identity; empty/true
// means dirty (fail-closed). Arbitrary short strings are rejected as full revisions.
var (
	reinframeCommit string // set via -ldflags for release builds
	reinframeDirty  string // "true" | "false" via -ldflags; empty = unattested dirty
)

// fullVCSRevision re matches full git SHA-1 (40) or SHA-256 (64) hex only.
var fullVCSRevision = regexp.MustCompile(`(?i)^[0-9a-f]{40}$|^[0-9a-f]{64}$`)

// isFullVCSRevision reports whether s is a full VCS object id (SHA-1 or SHA-256).
func isFullVCSRevision(s string) bool {
	s = strings.TrimSpace(s)
	return fullVCSRevision.MatchString(s)
}

// reinframeBuildIdentity returns (revision, dirty, source).
// source is one of: ldflags | vcs | unknown
//
// #218: ldflags never silently claims clean; malformed/truncated revisions
// are returned with dirty=true so liveQualification cannot GO/LIMITED_GO.
func reinframeBuildIdentity() (revision string, dirty bool, source string) {
	if s := strings.TrimSpace(reinframeCommit); s != "" {
		// Malformed revision cannot qualify; surface as dirty ldflags identity.
		if !isFullVCSRevision(s) {
			return s, true, "ldflags"
		}
		// Explicit clean attestation required; missing dirty flag ⇒ dirty.
		switch strings.ToLower(strings.TrimSpace(reinframeDirty)) {
		case "false", "0", "no":
			return s, false, "ldflags"
		case "true", "1", "yes", "":
			return s, true, "ldflags"
		default:
			// Unknown dirty token: fail closed as dirty.
			return s, true, "ldflags"
		}
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return "", false, "unknown"
	}
	var rev string
	var isDirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = strings.TrimSpace(s.Value)
		case "vcs.modified":
			isDirty = s.Value == "true"
		}
	}
	if rev != "" {
		// Truncated VCS revision from exotic toolchains: treat as dirty so
		// qualification requires a full object id.
		if !isFullVCSRevision(rev) {
			return rev, true, "vcs"
		}
		return rev, isDirty, "vcs"
	}
	return "", false, "unknown"
}

// gitHEAD is retained for diagnostics only; qualification must not use ambient CWD HEAD.
// Deprecated for provenance: use reinframeBuildIdentity.
func gitHEAD() string {
	// Intentionally empty for qualification callers that should use reinframeBuildIdentity.
	// Kept as a named function so accidental ambient git use is easier to audit.
	return ""
}
