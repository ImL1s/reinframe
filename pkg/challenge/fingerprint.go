package challenge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	Fingerprint     string
	SideEffectClass string
	TargetResources []string
	// OperationDigest is a secret-safe digest of the canonical operation
	// (edit content / shell command / tool args). Empty only for pure side-effect classes.
	OperationDigest string
	ToolClass       string
	ToolName        string
	CanonicalForm   string
}

var (
	reRmRF       = regexp.MustCompile(`(?i)\brm\s+(-[a-zA-Z]*r[a-zA-Z]*f[a-zA-Z]*|-[a-zA-Z]*f[a-zA-Z]*r[a-zA-Z]*)\s+(\S+)`)
	reRmRF2      = regexp.MustCompile(`(?i)\brm\s+--recursive\s+--force\s+(\S+)`)
	reFindDelete = regexp.MustCompile(`(?i)\bfind\s+(\S+)\s+.*-delete\b`)
	reCurl       = regexp.MustCompile(`(?i)\b(curl|wget)\b`)
	reDeploy     = regexp.MustCompile(`(?i)\b(kubectl\s+apply|helm\s+upgrade|terraform\s+apply|gcloud\s+.*deploy|fly\s+deploy|firebase\s+deploy)\b`)
	rePayment    = regexp.MustCompile(`(?i)\b(stripe\s+|charge\s+|paypal\s+|billing\s+)\b`)
	reChmod      = regexp.MustCompile(`(?i)\b(chmod|chown|setfacl|icacls)\b`)
	reGitMut     = regexp.MustCompile(`(?i)\bgit\s+(push|commit|reset\s+--hard|clean\s+-fd)\b`)
	reSecretOut  = regexp.MustCompile(`(?i)\b(cat\s+.*\.(pem|key|env)|curl\s+.*\$\{?\w*(TOKEN|SECRET|PASSWORD|API_KEY))`)
	reCrossWS    = regexp.MustCompile(`(?i)(\.\./|/[Uu]sers/|/home/|C:\\Users\\)`)
)

// ComputeFingerprint builds semantic identity from ProposedAction + ownership.
// Caller must ValidateProposedForChallenge first.
func ComputeFingerprint(in FingerprintInput) (FingerprintResult, error) {
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

	side, targets, err := classifySideEffect(pa)
	if err != nil {
		return FingerprintResult{}, err
	}
	for _, t := range pa.TargetScope {
		targets = append(targets, normalizeResource(t))
	}
	if pa.FilePath != "" {
		targets = append(targets, normalizeResource(pa.FilePath))
	}
	targets = uniqueSorted(targets)

	opDigest, err := operationDigest(pa, side)
	if err != nil {
		return FingerprintResult{}, err
	}

	// Structured closed encoding — length-prefixed fields, no ad-hoc | join of raw values.
	canon := encodeFingerprintCanon(map[string]string{
		"tool_class": pa.ToolClass,
		"tool_name":  pa.ToolName,
		"side":       side,
		"targets":    encodeStringList(targets),
		"op":         opDigest,
		"session":    session,
		"branch":     in.Branch,
		"ws":         ws,
		"contract":   fmt.Sprintf("%d", cr),
	})
	sum := sha256.Sum256([]byte(canon))
	fp := "af-" + hex.EncodeToString(sum[:16])
	return FingerprintResult{
		Fingerprint:     fp,
		SideEffectClass: side,
		TargetResources: targets,
		OperationDigest: opDigest,
		ToolClass:       pa.ToolClass,
		ToolName:        pa.ToolName,
		CanonicalForm:   canon,
	}, nil
}

// ClassifyRelationship compares a candidate action to the challenge original fingerprint.
func ClassifyRelationship(original FingerprintResult, candidate FingerprintResult) string {
	if original.Fingerprint == candidate.Fingerprint {
		return RelSame
	}
	// Syntax-rewrite bypass only for delete classes with matching targets.
	// Write/edit requires identical operation digest — never path-only bypass.
	if rewriteEligible(original.SideEffectClass) &&
		original.SideEffectClass == candidate.SideEffectClass &&
		len(original.TargetResources) > 0 &&
		sameStringSet(original.TargetResources, candidate.TargetResources) {
		return RelBypass
	}
	// Write with same path but different op digest → different (not bypass).
	if original.SideEffectClass == SideEffectWriteFile &&
		candidate.SideEffectClass == SideEffectWriteFile &&
		sameStringSet(original.TargetResources, candidate.TargetResources) &&
		original.OperationDigest != candidate.OperationDigest {
		return RelDifferent
	}
	if original.SideEffectClass == candidate.SideEffectClass &&
		len(candidate.TargetResources) > 0 &&
		strictSubset(candidate.TargetResources, original.TargetResources) {
		return RelReducedScope
	}
	return RelDifferent
}

func classifySideEffect(pa adapter.ProposedAction) (side string, targets []string, err error) {
	cmd := strings.TrimSpace(pa.Command)
	switch pa.ToolClass {
	case adapter.ToolClassRead:
		return SideEffectRead, nil, nil
	case adapter.ToolClassSearch:
		return SideEffectSearch, nil, nil
	case adapter.ToolClassEdit:
		if pa.FilePath != "" {
			return SideEffectWriteFile, []string{normalizeResource(pa.FilePath)}, nil
		}
		return SideEffectWriteFile, nil, nil
	}

	if cmd == "" {
		if pa.ToolClass == adapter.ToolClassShell {
			return SideEffectShellGeneric, nil, nil
		}
		return SideEffectUnknown, nil, nil
	}

	if adapter.FullSuiteCommand(pa) {
		return SideEffectTestSuite, []string{"./..."}, nil
	}
	if reSecretOut.MatchString(cmd) {
		return SideEffectUnknown, extractPaths(cmd), nil
	}
	if reDeploy.MatchString(cmd) {
		return SideEffectDeploy, extractPaths(cmd), nil
	}
	if rePayment.MatchString(cmd) {
		return SideEffectPayment, nil, nil
	}
	if reChmod.MatchString(cmd) {
		return SideEffectPermission, extractPaths(cmd), nil
	}
	if reGitMut.MatchString(cmd) {
		return SideEffectGitMutate, nil, nil
	}
	if reCurl.MatchString(cmd) {
		return SideEffectNetwork, nil, nil
	}
	if isRecursiveForceRm(cmd) {
		targets := extractRmTargets(cmd)
		if len(targets) > 0 {
			return SideEffectDeleteTree, targets, nil
		}
		return "", nil, fmt.Errorf("fingerprint: ambiguous recursive rm with no targets")
	}
	if m := reFindDelete.FindStringSubmatch(cmd); len(m) >= 2 {
		targets := extractFindDeleteTargets(cmd)
		if len(targets) == 0 {
			targets = []string{normalizeResource(m[1])}
		}
		return SideEffectDeleteTree, targets, nil
	}
	if isPlainRm(cmd) {
		targets := extractRmTargets(cmd)
		if len(targets) > 0 {
			return SideEffectDeleteFile, targets, nil
		}
		return "", nil, fmt.Errorf("fingerprint: ambiguous rm with no targets")
	}
	if pa.ToolClass == adapter.ToolClassShell {
		return SideEffectShellGeneric, extractPaths(cmd), nil
	}
	if len(pa.Arguments) > 0 {
		extra := make([]string, 0, len(pa.Arguments))
		for _, a := range pa.Arguments {
			if a != "" {
				extra = append(extra, normalizeResource(a))
			}
		}
		return SideEffectUnknown, extra, nil
	}
	return SideEffectUnknown, extractPaths(cmd), nil
}

// operationDigest returns a secret-safe digest of the operation identity.
func operationDigest(pa adapter.ProposedAction, side string) (string, error) {
	switch side {
	case SideEffectWriteFile:
		return editOperationDigest(pa)
	case SideEffectDeleteTree, SideEffectDeleteFile:
		// Target list carries identity; command surface is rewrite-eligible.
		return "delete_op", nil
	case SideEffectTestSuite:
		return "test_suite", nil
	case SideEffectRead, SideEffectSearch:
		// ToolName + targets distinguish empty-target collapses.
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"tool": pa.ToolName,
			"path": pa.FilePath,
			"args": encodeStringList(pa.Arguments),
		}))), nil
	default:
		// Shell / unknown / network: whitespace-collapsed command only (preserve case).
		cmd := normalizeCommandPreserveCase(pa.Command)
		if cmd == "" && len(pa.Arguments) == 0 && len(pa.RedactedPayload) == 0 {
			// Commandless non-read/search: bind tool name so empty targets do not collapse.
			return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
				"tool":  pa.ToolName,
				"class": pa.ToolClass,
				"args":  encodeStringList(pa.Arguments),
			}))), nil
		}
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"cmd":  cmd,
			"args": encodeStringList(pa.Arguments),
			"tool": pa.ToolName,
		}))), nil
	}
}

// editOperationDigest binds write fingerprints to the actual edit operation, not only FilePath.
// Equivalent lossless surfaces (Arguments content vs RedactedPayload new_string/new_str/content)
// normalize to the same content digests so fingerprints match.
func editOperationDigest(pa adapter.ProposedAction) (string, error) {
	parts := map[string]string{
		// ToolName intentionally omitted from op identity so Edit/StrReplace equivalents can match
		// when path+content digests are the same; ToolClass still in outer fingerprint.
		"path": normalizeResource(pa.FilePath),
	}
	var oldDig, newDig string

	// Arguments: common shapes [new], [old,new], or freeform content tokens.
	if len(pa.Arguments) == 1 {
		newDig = digestBytes([]byte(pa.Arguments[0]))
	} else if len(pa.Arguments) >= 2 {
		oldDig = digestBytes([]byte(pa.Arguments[0]))
		newDig = digestBytes([]byte(pa.Arguments[1]))
	}

	// RedactedPayload closed content keys (aliases map to the same slots).
	if len(pa.RedactedPayload) > 0 && string(pa.RedactedPayload) != "{}" && string(pa.RedactedPayload) != "null" {
		var m map[string]any
		if err := json.Unmarshal(pa.RedactedPayload, &m); err != nil {
			return "", fmt.Errorf("proposed_action: edit redacted_payload is not valid JSON object")
		}
		if s := stringField(m, "old_string", "old_str"); s != "" {
			oldDig = digestBytes([]byte(s))
		}
		if s := stringField(m, "new_string", "new_str", "content", "contents"); s != "" {
			newDig = digestBytes([]byte(s))
		}
	}

	if oldDig != "" {
		parts["old"] = oldDig
	}
	if newDig != "" {
		parts["new"] = newDig
	}
	if oldDig == "" && newDig == "" {
		if strings.TrimSpace(pa.Command) != "" {
			parts["cmd"] = normalizeCommandPreserveCase(pa.Command)
		} else {
			return "", fmt.Errorf("proposed_action: edit operation missing content (args/payload/command)")
		}
	}
	return digestBytes([]byte(encodeFingerprintCanon(parts))), nil
}

func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

func digestBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// encodeFingerprintCanon encodes key→value with sorted keys and length-prefixed values.
func encodeFingerprintCanon(fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "n=%d", len(keys))
	for _, k := range keys {
		v := fields[k]
		_, _ = fmt.Fprintf(&b, ";k=%d:%s;v=%d:%s", len(k), k, len(v), v)
	}
	return b.String()
}

func isRecursiveForceRm(cmd string) bool {
	low := strings.ToLower(cmd)
	if !strings.Contains(low, "rm") {
		return false
	}
	if reRmRF.MatchString(cmd) || reRmRF2.MatchString(cmd) {
		return true
	}
	fields := strings.Fields(low)
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
	if !strings.Contains(low, "rm ") && !strings.HasPrefix(low, "rm ") && low != "rm" {
		return false
	}
	if isRecursiveForceRm(cmd) {
		return false
	}
	fields := strings.Fields(low)
	for _, f := range fields {
		if f == "rm" {
			return true
		}
	}
	return false
}

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
	s = strings.TrimPrefix(s, "./")
	return s
}

// normalizeCommandPreserveCase collapses whitespace only; does not lowercase paths/args.
func normalizeCommandPreserveCase(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	return strings.Join(strings.Fields(cmd), " ")
}

// encodeStringList is a collision-free stable encoding for multi-value fields.
func encodeStringList(items []string) string {
	if len(items) == 0 {
		return "0"
	}
	var b strings.Builder
	_, _ = fmt.Fprintf(&b, "%d", len(items))
	for _, s := range items {
		_, _ = fmt.Fprintf(&b, ";%d:%s", len(s), s)
	}
	return b.String()
}

func rewriteEligible(side string) bool {
	// Write/edit is NOT rewrite-eligible without matching operation digest.
	switch side {
	case SideEffectDeleteTree, SideEffectDeleteFile:
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
