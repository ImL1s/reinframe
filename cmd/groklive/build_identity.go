package main

import (
	"runtime/debug"
	"strings"
)

// reinframeBuildIdentity returns the commit bound to the running binary.
// Prefer Go VCS build info / -ldflags -X main.reinframeCommit; never ambient CWD git HEAD.
//
// reinframeCommit may be set at link time:
//
//	go build -ldflags "-X main.reinframeCommit=$(git rev-parse HEAD)"
var reinframeCommit string // set via -ldflags for release builds

// reinframeBuildIdentity returns (revision, dirty, source).
// source is one of: ldflags | vcs | unknown
func reinframeBuildIdentity() (revision string, dirty bool, source string) {
	if s := strings.TrimSpace(reinframeCommit); s != "" {
		return s, false, "ldflags"
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
