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
	// Syntax-rewrite bypass only for rewrite-eligible classes (delete/write), where
	// command surface varies but targets+side-effect define identity.
	// shell_generic/network/git_mutate use cmd-bearing fingerprints; never RelBypass.
	if rewriteEligible(original.SideEffectClass) &&
		original.SideEffectClass == candidate.SideEffectClass &&
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
	// Multi-target rm -rf / rm --recursive --force: capture ALL path operands.
	if isRecursiveForceRm(cmd) {
		targets := extractRmTargets(cmd)
		if len(targets) > 0 {
			return SideEffectDeleteTree, targets
		}
	}
	if m := reFindDelete.FindStringSubmatch(cmd); len(m) >= 2 {
		// find PATH ... -delete may have only one root; still extract all non-flag args after find.
		targets := extractFindDeleteTargets(cmd)
		if len(targets) == 0 {
			targets = []string{normalizeResource(m[1])}
		}
		return SideEffectDeleteTree, targets
	}
	// Plain rm file (not recursive) — all path operands.
	if isPlainRm(cmd) {
		targets := extractRmTargets(cmd)
		if len(targets) > 0 {
			if len(targets) == 1 {
				return SideEffectDeleteFile, targets
			}
			// Multi-file non-recursive rm still binds on full target set.
			return SideEffectDeleteFile, targets
		}
	}
	if pa.ToolClass == adapter.ToolClassShell {
		return SideEffectShellGeneric, extractPaths(cmd)
	}
	// Non-shell tools: include arguments in path extraction when present.
	if len(pa.Arguments) > 0 {
		extra := make([]string, 0, len(pa.Arguments))
		for _, a := range pa.Arguments {
			if a != "" {
				extra = append(extra, normalizeResource(a))
			}
		}
		return SideEffectUnknown, extra
	}
	return SideEffectUnknown, extractPaths(cmd)
}

func isRecursiveForceRm(cmd string) bool {
	low := strings.ToLower(cmd)
	if !strings.Contains(low, "rm") {
		return false
	}
	// rm -rf / -fr / --recursive --force (any order of short flags)
	if reRmRF.MatchString(cmd) || reRmRF2.MatchString(cmd) {
		return true
	}
	// also: rm -r -f path, rm -f -r path
	fields := strings.Fields(low)
	if len(fields) == 0 || fields[0] != "rm" {
		// allow leading env: env rm -rf ...
		hasRm := false
		for _, f := range fields {
			if f == "rm" {
				hasRm = true
				break
			}
		}
		if !hasRm {
			return false
		}
	}
	hasR, hasF := false, false
	for _, f := range fields {
		if f == "--recursive" {
			hasR = true
		}
		if f == "--force" {
			hasF = true
		}
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			if strings.Contains(f, "r") {
				hasR = true
			}
			if strings.Contains(f, "f") {
				hasF = true
			}
		}
	}
	return hasR && hasF
}

func isPlainRm(cmd string) bool {
	low := strings.ToLower(strings.TrimSpace(cmd))
	if !strings.Contains(low, "rm ") && !strings.HasPrefix(low, "rm ") {
		return false
	}
	if isRecursiveForceRm(cmd) {
		return false
	}
	// exclude rmdir
	fields := strings.Fields(low)
	for _, f := range fields {
		if f == "rm" {
			return true
		}
	}
	return false
}

// extractRmTargets returns every non-flag operand after the rm binary.
func extractRmTargets(cmd string) []string {
	fields := strings.Fields(cmd)
	var out []string
	seenRm := false
	for _, f := range fields {
		if !seenRm {
			if strings.EqualFold(f, "rm") {
				seenRm = true
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		out = append(out, normalizeResource(f))
	}
	return out
}

func extractFindDeleteTargets(cmd string) []string {
	fields := strings.Fields(cmd)
	var out []string
	seenFind := false
	for _, f := range fields {
		if !seenFind {
			if strings.EqualFold(f, "find") {
				seenFind = true
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		// first non-flag after find is the root path (and any extras)
		out = append(out, normalizeResource(f))
	}
	return out
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
	// collapse ./prefix — preserve case for case-sensitive filesystems.
	s = strings.TrimPrefix(s, "./")
	return s
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

func rewriteEligible(side string) bool {
	switch side {
	case SideEffectDeleteTree, SideEffectDeleteFile, SideEffectWriteFile:
		return true
	default:
		return false
	}
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
