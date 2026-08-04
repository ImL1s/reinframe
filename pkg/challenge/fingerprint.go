package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/ImL1s/reinframe/pkg/adapter"
)

// FingerprintInput is the ownership-bound context for semantic identity.
type FingerprintInput struct {
	Proposed          adapter.ProposedAction
	SessionID         string
	Branch            string
	WorkspaceRevision string
	ContractRevision  int
}

// FingerprintResult is the deterministic semantic identity of an action.
type FingerprintResult struct {
	// Fingerprint is the closed hash used for challenge binding.
	Fingerprint string
	// SideEffectClass is the canonical side-effect bucket.
	SideEffectClass string
	// TargetResources are normalized resource identities (sorted unique).
	TargetResources []string
	// CanonicalForm is a stable human-auditable form (no secrets).
	CanonicalForm string
}

var (
	reRmRF       = regexp.MustCompile(`(?i)\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)\s+(\S+)`)
	reRmRF2      = regexp.MustCompile(`(?i)\brm\s+--recursive\s+--force\s+(\S+)`)
	reFindDelete = regexp.MustCompile(`(?i)\bfind\s+(\S+)\s+.*-delete\b`)
	reRmFile     = regexp.MustCompile(`(?i)\brm\s+(?:-[a-zA-Z]+\s+)*(\S+)`)
	reCurl       = regexp.MustCompile(`(?i)\b(curl|wget)\b`)
	reDeploy     = regexp.MustCompile(`(?i)\b(kubectl\s+apply|helm\s+upgrade|terraform\s+apply|gcloud\s+.*deploy|fly\s+deploy|firebase\s+deploy)\b`)
	rePayment    = regexp.MustCompile(`(?i)\b(stripe\s+|charge\s+|paypal\s+|billing\s+)\b`)
	reChmod      = regexp.MustCompile(`(?i)\b(chmod|chown|setfacl|icacls)\b`)
	reGitMut     = regexp.MustCompile(`(?i)\bgit\s+(push|commit|reset\s+--hard|clean\s+-fd)\b`)
	reSecretOut  = regexp.MustCompile(`(?i)\b(cat\s+.*\.(pem|key|env)|curl\s+.*\$\{?\w*(TOKEN|SECRET|PASSWORD|API_KEY))`)
	reCrossWS    = regexp.MustCompile(`(?i)(\.\./|/[Uu]sers/|/home/|C:\\Users\\)`)
)

// ComputeFingerprint builds semantic identity from ProposedAction + ownership.
// Syntax-only rewrites that share side-effect class + targets produce the same fingerprint.
func ComputeFingerprint(in FingerprintInput) FingerprintResult {
	pa := in.Proposed
	session := in.SessionID
	if session == "" {
		session = pa.SessionID
	}
	ws := in.WorkspaceRevision
	if ws == "" {
		ws = pa.WorkspaceRevision
	}
	cr := in.ContractRevision
	if cr == 0 {
		cr = pa.ContractRevision
	}

	side, targets := classifySideEffect(pa)
	// Merge explicit TargetScope / FilePath.
	for _, t := range pa.TargetScope {
		targets = append(targets, normalizeResource(t))
	}
	if pa.FilePath != "" {
		targets = append(targets, normalizeResource(pa.FilePath))
	}
	targets = uniqueSorted(targets)

	// Strong side-effect classes bind on class+targets so syntax rewrites match.
	// Pathless/generic shells include a normalized command so unrelated actions
	// (e.g. echo vs sleep) do not collapse into one challenge.
	cmdPart := ""
	switch side {
	case SideEffectShellGeneric, SideEffectNetwork, SideEffectGitMutate, SideEffectUnknown, SideEffectNone:
		cmdPart = normalizeCommand(pa.Command)
	case SideEffectTestSuite:
		cmdPart = "test_suite"
	}

	canon := fmt.Sprintf(
		"tool_class=%s|side=%s|targets=%s|cmd=%s|session=%s|branch=%s|ws=%s|contract=%d",
		pa.ToolClass, side, strings.Join(targets, ","), cmdPart, session, in.Branch, ws, cr,
	)
	sum := sha256.Sum256([]byte(canon))
	fp := "af-" + hex.EncodeToString(sum[:16])
	return FingerprintResult{
		Fingerprint:     fp,
		SideEffectClass: side,
		TargetResources: targets,
		CanonicalForm:   canon,
	}
}

// ClassifyRelationship compares a candidate action to the challenge original fingerprint.
func ClassifyRelationship(original FingerprintResult, candidate FingerprintResult) string {
	if original.Fingerprint == candidate.Fingerprint {
		return RelSame
	}
	// Same side-effect + same non-empty target multiset → syntax-rewrite bypass attempt.
	// Pathless classes (empty targets) must NOT collapse via RelBypass — fingerprints already
	// include normalized command for shell_generic/network/git_mutate.
	if original.SideEffectClass == candidate.SideEffectClass &&
		original.SideEffectClass != SideEffectNone &&
		len(original.TargetResources) > 0 &&
		sameStringSet(original.TargetResources, candidate.TargetResources) {
		return RelBypass
	}
	// Reduced scope: same side-effect class, candidate targets strict subset of original.
	if original.SideEffectClass == candidate.SideEffectClass &&
		len(candidate.TargetResources) > 0 &&
		strictSubset(candidate.TargetResources, original.TargetResources) {
		return RelReducedScope
	}
	return RelDifferent
}

func classifySideEffect(pa adapter.ProposedAction) (side string, targets []string) {
	cmd := strings.TrimSpace(pa.Command)
	switch pa.ToolClass {
	case adapter.ToolClassRead:
		return SideEffectRead, nil
	case adapter.ToolClassSearch:
		return SideEffectSearch, nil
	case adapter.ToolClassEdit:
		if pa.FilePath != "" {
			return SideEffectWriteFile, []string{normalizeResource(pa.FilePath)}
		}
		return SideEffectWriteFile, nil
	}

	if cmd == "" {
		if pa.ToolClass == adapter.ToolClassShell {
			return SideEffectShellGeneric, nil
		}
		return SideEffectUnknown, nil
	}

	if adapter.FullSuiteCommand(pa) {
		return SideEffectTestSuite, []string{"./..."}
	}
	if reSecretOut.MatchString(cmd) {
		return SideEffectUnknown, extractPaths(cmd)
	}
	if reDeploy.MatchString(cmd) {
		return SideEffectDeploy, extractPaths(cmd)
	}
	if rePayment.MatchString(cmd) {
		return SideEffectPayment, nil
	}
	if reChmod.MatchString(cmd) {
		return SideEffectPermission, extractPaths(cmd)
	}
	if reGitMut.MatchString(cmd) {
		return SideEffectGitMutate, nil
	}
	if reCurl.MatchString(cmd) {
		return SideEffectNetwork, nil
	}
	if m := reRmRF.FindStringSubmatch(cmd); len(m) >= 3 {
		return SideEffectDeleteTree, []string{normalizeResource(m[2])}
	}
	if m := reRmRF2.FindStringSubmatch(cmd); len(m) >= 2 {
		return SideEffectDeleteTree, []string{normalizeResource(m[1])}
	}
	if m := reFindDelete.FindStringSubmatch(cmd); len(m) >= 2 {
		return SideEffectDeleteTree, []string{normalizeResource(m[1])}
	}
	// Plain rm file (not -rf) — after -rf patterns.
	if strings.Contains(strings.ToLower(cmd), "rm ") && !strings.Contains(cmd, "-r") {
		if m := reRmFile.FindStringSubmatch(cmd); len(m) >= 2 {
			return SideEffectDeleteFile, []string{normalizeResource(m[1])}
		}
	}
	if pa.ToolClass == adapter.ToolClassShell {
		return SideEffectShellGeneric, extractPaths(cmd)
	}
	return SideEffectUnknown, extractPaths(cmd)
}

func extractPaths(cmd string) []string {
	fields := strings.Fields(cmd)
	var out []string
	for _, f := range fields {
		if strings.HasPrefix(f, "-") {
			continue
		}
		// skip common binaries
		low := strings.ToLower(f)
		if low == "rm" || low == "find" || low == "go" || low == "git" || low == "curl" || low == "wget" {
			continue
		}
		if strings.Contains(f, "/") || strings.Contains(f, ".") || strings.HasPrefix(f, "build") {
			out = append(out, normalizeResource(f))
		}
	}
	return out
}

func normalizeResource(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = path.Clean(s)
	// collapse ./prefix
	s = strings.TrimPrefix(s, "./")
	return strings.ToLower(s)
}

// normalizeCommand is a closed, deterministic shell surface for fingerprinting
// generic commands (not used for delete_tree rewrite equivalence).
func normalizeCommand(cmd string) string {
	cmd = strings.TrimSpace(strings.ToLower(cmd))
	if cmd == "" {
		return ""
	}
	// Collapse whitespace only — do not invent semantic equality for arbitrary shell.
	fields := strings.Fields(cmd)
	return strings.Join(fields, " ")
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	m := map[string]struct{}{}
	for _, s := range in {
		if s == "" || s == "." {
			continue
		}
		m[s] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func strictSubset(sub, super []string) bool {
	if len(sub) == 0 || len(sub) >= len(super) {
		return false
	}
	set := map[string]struct{}{}
	for _, s := range super {
		set[s] = struct{}{}
	}
	for _, s := range sub {
		if _, ok := set[s]; !ok {
			return false
		}
	}
	return true
}

// LooksLikeCrossWorkspace is a hard-deny signal helper.
func LooksLikeCrossWorkspace(pa adapter.ProposedAction) bool {
	if reCrossWS.MatchString(pa.Command) || reCrossWS.MatchString(pa.FilePath) {
		return true
	}
	for _, t := range pa.TargetScope {
		if reCrossWS.MatchString(t) {
			return true
		}
	}
	return false
}

// LooksLikeSecretExfil is a hard-deny signal helper.
func LooksLikeSecretExfil(pa adapter.ProposedAction) bool {
	return reSecretOut.MatchString(pa.Command)
}
