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
	// Syntax-rewrite bypass only for delete classes with matching targets AND
	// matching operation digests (flags/predicates/env must not be dropped).
	// Write/edit requires identical operation digest — never path-only bypass.
	if rewriteEligible(original.SideEffectClass) &&
		original.SideEffectClass == candidate.SideEffectClass &&
		len(original.TargetResources) > 0 &&
		sameStringSet(original.TargetResources, candidate.TargetResources) &&
		original.OperationDigest != "" &&
		original.OperationDigest == candidate.OperationDigest {
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
		// Compound shells that merely contain ./... must not collapse to test_suite identity.
		if shellHasCompoundOrQuoting(cmd) || shellHasResolutionEnv(cmd) {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
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
	// Compound / quoting / resolution ENV (PATH= etc.): never privileged delete identity.
	// Matches FullSuiteCommand fail-closed on executable-resolution overrides.
	unsafeShell := shellHasCompoundOrQuoting(cmd) || shellHasResolutionEnv(cmd)
	if isRecursiveForceRm(cmd) {
		if unsafeShell {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
		targets := extractRmTargets(cmd)
		if len(targets) > 0 {
			return SideEffectDeleteTree, targets, nil
		}
		return "", nil, fmt.Errorf("fingerprint: ambiguous recursive rm with no targets")
	}
	// find -delete only when find is in command position.
	if strings.EqualFold(shellArgv0(cmd), "find") && reFindDelete.MatchString(cmd) {
		if unsafeShell {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
		// Predicate-bearing find (e.g. -name) is not path-only delete_tree equality.
		if findHasExpressionPredicates(cmd) {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
		targets := extractFindDeleteTargets(cmd)
		if len(targets) == 0 {
			if m := reFindDelete.FindStringSubmatch(cmd); len(m) >= 2 {
				targets = []string{normalizeResource(m[1])}
			}
		}
		if len(targets) == 0 {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
		return SideEffectDeleteTree, targets, nil
	}
	if isPlainRm(cmd) {
		if unsafeShell {
			return SideEffectShellGeneric, extractPaths(cmd), nil
		}
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
		// Bind residual flags/expression (not constant "delete_op") so predicate
		// and flag differences cannot RelBypass via path-only match.
		return shellPrivilegedOpDigest(pa.Command, "delete"), nil
	case SideEffectTestSuite:
		// Bind validated suite invocation (not bare constant) so residual flags
		// that passed FullSuiteCommand still cannot collide across variants.
		return shellPrivilegedOpDigest(pa.Command, "test_suite"), nil
	case SideEffectRead, SideEffectSearch:
		// ToolName + targets + bounded payload distinguish empty-target collapses.
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"tool":    pa.ToolName,
			"path":    pa.FilePath,
			"args":    encodeStringList(pa.Arguments),
			"payload": boundedPayloadDigest(pa.RedactedPayload),
		}))), nil
	default:
		// Shell / unknown / network: bind command with quote-preserving digest.
		// strings.Fields collapses spaces inside quoted -c literals; that is unsafe.
		cmd := normalizeCommandForDigest(pa.Command)
		if cmd == "" && len(pa.Arguments) == 0 && len(pa.RedactedPayload) == 0 {
			// Commandless non-read/search: bind tool name so empty targets do not collapse.
			return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
				"tool":  pa.ToolName,
				"class": pa.ToolClass,
				"args":  encodeStringList(pa.Arguments),
			}))), nil
		}
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"cmd":     cmd,
			"args":    encodeStringList(pa.Arguments),
			"tool":    pa.ToolName,
			"payload": boundedPayloadDigest(pa.RedactedPayload),
		}))), nil
	}
}

// boundedPayloadDigest hashes redacted JSON if present; empty when absent.
func boundedPayloadDigest(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return ""
	}
	// Canonical JSON re-encode when possible for stable digests.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return digestBytes(raw)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return digestBytes(raw)
	}
	return digestBytes(b)
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

	// Closed edit shapes only:
	//  - Arguments: [new] or [old,new] (len > 2 rejected)
	//  - RedactedPayload: old_string/new_string (aliases); conflict with args rejected
	if len(pa.Arguments) > 2 {
		return "", fmt.Errorf("proposed_action: edit arguments length %d unsupported (want 1 or 2)", len(pa.Arguments))
	}
	if len(pa.Arguments) == 1 {
		newDig = digestBytes([]byte(pa.Arguments[0]))
	} else if len(pa.Arguments) == 2 {
		oldDig = digestBytes([]byte(pa.Arguments[0]))
		newDig = digestBytes([]byte(pa.Arguments[1]))
	}

	// Closed payload keys only — unknown control fields are rejected (e.g. replace_all).
	allowedEditPayloadKeys := map[string]struct{}{
		"old_string": {}, "old_str": {},
		"new_string": {}, "new_str": {}, "content": {}, "contents": {},
		"file_path": {}, "path": {},
		"replace_all": {},
	}
	if len(pa.RedactedPayload) > 0 && string(pa.RedactedPayload) != "{}" && string(pa.RedactedPayload) != "null" {
		var m map[string]any
		if err := json.Unmarshal(pa.RedactedPayload, &m); err != nil {
			return "", fmt.Errorf("proposed_action: edit redacted_payload is not valid JSON object")
		}
		for k := range m {
			if _, ok := allowedEditPayloadKeys[k]; !ok {
				return "", fmt.Errorf("proposed_action: edit payload key %q unsupported", k)
			}
		}
		if s := stringField(m, "old_string", "old_str"); s != "" {
			d := digestBytes([]byte(s))
			if oldDig != "" && oldDig != d {
				return "", fmt.Errorf("proposed_action: edit old content conflicts between args and payload")
			}
			oldDig = d
		}
		if s := stringField(m, "new_string", "new_str", "content", "contents"); s != "" {
			d := digestBytes([]byte(s))
			if newDig != "" && newDig != d {
				return "", fmt.Errorf("proposed_action: edit new content conflicts between args and payload")
			}
			newDig = d
		}
		// Bind replace_all so false vs true cannot share fingerprint.
		if v, ok := m["replace_all"]; ok {
			parts["replace_all"] = fmt.Sprintf("%v", v)
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

// shellHasCompoundOrQuoting reports shell metacharacters that make Fields-based
// delete parsing unsafe (compound ops, quoting, escapes).
func shellHasCompoundOrQuoting(cmd string) bool {
	if strings.ContainsAny(cmd, ";|&\n\r`'\"\\") {
		return true
	}
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "||") {
		return true
	}
	// Token glued to flag after compound was split poorly: e.g. -delete;id
	for _, f := range strings.Fields(cmd) {
		if strings.ContainsAny(f, ";|&") {
			return true
		}
	}
	return false
}

// envAssignPrefix matches shell ENV=value prefixes only (valid identifier names).
// Rejects spoof tokens like `1BAD=x` that must not skip past command position.
var envAssignPrefix = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// resolutionEnvNames affect executable lookup; privileged delete/suite must fail closed.
var resolutionEnvNames = map[string]struct{}{
	"PATH": {}, "LD_LIBRARY_PATH": {}, "LD_PRELOAD": {}, "DYLD_LIBRARY_PATH": {},
	"DYLD_INSERT_LIBRARIES": {}, "HOME": {}, "GOROOT": {}, "GOPATH": {},
	"GOBIN": {}, "GOTOOLDIR": {}, "BASH_ENV": {}, "ENV": {}, "CDPATH": {},
}

// shellHasResolutionEnv reports ENV= prefixes that can redirect which binary runs.
func shellHasResolutionEnv(cmd string) bool {
	for _, f := range strings.Fields(strings.TrimSpace(cmd)) {
		if !envAssignPrefix.MatchString(f) {
			// Stop at first non-ENV field (argv0 area); only leading assigns count.
			if strings.Contains(f, "=") && !strings.HasPrefix(f, "-") {
				// invalid assign token — treat as present noise, continue scan until bare field
				continue
			}
			break
		}
		eq := strings.IndexByte(f, '=')
		name := strings.ToUpper(f[:eq])
		if _, ok := resolutionEnvNames[name]; ok {
			return true
		}
		// Any leading ENV= is also fail-closed for privileged ops (matches suite rule).
		return true
	}
	return false
}

// shellArgv0 returns the bare command-position name (first non ENV=val field).
// Path-qualified argv0 (./rm, /tmp/rm) returns "" so privileged delete/find
// classification fails closed — never path.Base (would treat ./rm as rm).
// When resolution ENV is present, returns "" (privileged classification unavailable).
func shellArgv0(cmd string) string {
	if shellHasResolutionEnv(cmd) {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(cmd))
	i := 0
	for i < len(fields) {
		f := fields[i]
		// ENV=value prefixes only — name must be a valid shell identifier.
		if envAssignPrefix.MatchString(f) {
			i++
			continue
		}
		break
	}
	if i >= len(fields) {
		return ""
	}
	argv0 := fields[i]
	// Reject path-qualified tools (matches FullSuiteCommand trusted-argv0 rule).
	if strings.ContainsAny(argv0, `/\`) {
		return ""
	}
	return argv0
}

// shellPrivilegedOpDigest binds operation identity for privileged shell sides.
// Pure delete uses kind + scope-altering flags only (not -r/-f), so syntax rewrites
// (rm -rf ↔ find -delete) still match, while --one-file-system etc. do not.
// test_suite binds residual argv so flag variants cannot collapse.
func shellPrivilegedOpDigest(cmd, kind string) string {
	if kind == "delete" {
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"kind":        "delete",
			"scope_flags": encodeStringList(extractDeleteScopeFlags(cmd)),
		})))
	}
	fields := strings.Fields(strings.TrimSpace(cmd))
	i := skipShellEnvPrefixes(fields)
	rest := []string{}
	if i < len(fields) {
		rest = append([]string(nil), fields[i+1:]...)
	}
	return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
		"kind": kind,
		"rest": encodeStringList(rest),
		"cmd":  normalizeCommandForDigest(cmd),
	})))
}

// extractDeleteScopeFlags returns flags that change deletion scope/safety beyond
// recursive/force (which are rewrite-eligible between rm and find -delete).
func extractDeleteScopeFlags(cmd string) []string {
	fields := strings.Fields(cmd)
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	argv0 := strings.ToLower(fields[i])
	i++
	var out []string
	if argv0 == "rm" {
		for ; i < len(fields); i++ {
			f := fields[i]
			low := strings.ToLower(f)
			switch {
			case low == "--one-file-system" || low == "--preserve-root" || low == "--no-preserve-root" ||
				low == "-i" || low == "-I" || low == "--interactive" || strings.HasPrefix(low, "--interactive="):
				out = append(out, low)
			case strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--"):
				// short clusters: ignore r/f/v; any other letter is scope-altering (e.g. -i)
				for _, r := range low[1:] {
					if r != 'r' && r != 'f' && r != 'v' {
						out = append(out, string(r))
					}
				}
			}
		}
	}
	// find sole -delete: no scope flags (predicates already fail closed earlier)
	sort.Strings(out)
	return out
}

// findHasExpressionPredicates reports find expression tokens beyond path roots and -delete.
// e.g. -name, -type, -path make delete scope unequal to plain path -delete.
func findHasExpressionPredicates(cmd string) bool {
	fields := strings.Fields(cmd)
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) || !strings.EqualFold(fields[i], "find") {
		return false
	}
	i++
	// skip global options
	for i < len(fields) {
		f := fields[i]
		if f == "--" {
			i++
			break
		}
		if f == "-H" || f == "-L" || f == "-P" || strings.HasPrefix(f, "-O") {
			i++
			continue
		}
		if f == "-D" {
			i++
			if i < len(fields) {
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "-D") && len(f) > 2 {
			i++
			continue
		}
		break
	}
	// skip path roots
	for i < len(fields) && !strings.HasPrefix(fields[i], "-") {
		i++
	}
	// remaining expression: only the sole token -delete is rewrite-eligible.
	// Boolean glue (-o/-a), printers (-print), and other actions make scope unequal
	// (e.g. find build -print -o -delete must not share delete_tree with find build -delete).
	expr := fields[i:]
	if len(expr) != 1 || !strings.EqualFold(expr[0], "-delete") {
		return true
	}
	return false
}

func isRecursiveForceRm(cmd string) bool {
	// Command-position only — never match `echo rm -rf build`.
	if !strings.EqualFold(shellArgv0(cmd), "rm") {
		return false
	}
	low := strings.ToLower(cmd)
	if reRmRF.MatchString(cmd) || reRmRF2.MatchString(cmd) {
		return true
	}
	fields := strings.Fields(low)
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
	if !strings.EqualFold(shellArgv0(cmd), "rm") {
		return false
	}
	if isRecursiveForceRm(cmd) {
		return false
	}
	return true
}

func skipShellEnvPrefixes(fields []string) int {
	i := 0
	for i < len(fields) {
		if envAssignPrefix.MatchString(fields[i]) {
			i++
			continue
		}
		break
	}
	return i
}

func extractRmTargets(cmd string) []string {
	if !strings.EqualFold(shellArgv0(cmd), "rm") {
		return nil
	}
	fields := strings.Fields(cmd)
	var out []string
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	i++ // skip argv0 (rm)
	for ; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "-") {
			continue
		}
		out = append(out, normalizeResource(f))
	}
	return out
}

func extractFindDeleteTargets(cmd string) []string {
	if !strings.EqualFold(shellArgv0(cmd), "find") {
		return nil
	}
	fields := strings.Fields(cmd)
	var out []string
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	i++ // skip find
	// GNU find: find [-HLP] [-Olevel] [-D debugopts] [--] [path...] [expression]
	// Skip leading global options and optional `--` before path roots.
	// -D takes a following debugopts argument; -Dsearch may be attached.
	for i < len(fields) {
		f := fields[i]
		if f == "--" {
			i++
			break
		}
		if f == "-H" || f == "-L" || f == "-P" {
			i++
			continue
		}
		if strings.HasPrefix(f, "-O") {
			i++
			continue
		}
		if f == "-D" {
			i++ // -D
			if i < len(fields) {
				i++ // debugopts
			}
			continue
		}
		if strings.HasPrefix(f, "-D") && len(f) > 2 {
			// attached form -Dsearch
			i++
			continue
		}
		break
	}
	// Path roots until expression (first remaining '-' token).
	for ; i < len(fields); i++ {
		f := fields[i]
		if strings.HasPrefix(f, "-") {
			break
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

// normalizeCommandForDigest is the identity encoding for shell command digests.
//   - Quotes/escapes present: retain exact trimmed command (no Fields collapse inside
//     quoted literals — e.g. python -c 'if "a  b"' vs 'if "a b"' must differ).
//   - Otherwise: collapse horizontal whitespace; preserve newlines as separators.
func normalizeCommandForDigest(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	cmd = strings.ReplaceAll(cmd, "\r\n", "\n")
	cmd = strings.ReplaceAll(cmd, "\r", "\n")
	// Quoted/escaped interiors: do not collapse spaces (semantic in -c literals).
	if strings.ContainsAny(cmd, `'"\\`) {
		return cmd
	}
	return normalizeCommandPreserveCase(cmd)
}

// normalizeCommandPreserveCase collapses horizontal whitespace only.
// Newlines/carriage returns are preserved as separators so multi-command
// lines cannot collide with space-joined single commands
// (echo ok\nrm -rf build ≠ echo ok rm -rf build).
// Prefer normalizeCommandForDigest for fingerprint digests.
func normalizeCommandPreserveCase(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	cmd = strings.ReplaceAll(cmd, "\r\n", "\n")
	cmd = strings.ReplaceAll(cmd, "\r", "\n")
	if strings.Contains(cmd, "\n") {
		parts := strings.Split(cmd, "\n")
		for i, p := range parts {
			parts[i] = strings.Join(strings.Fields(p), " ")
		}
		return strings.Join(parts, "\n")
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
