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
	"unicode"
	"unicode/utf8"

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
		// TargetScope is advisory scope labels — keep general clean for non-write classes.
		// For write/edit authorization, FilePath lexical identity is authoritative (below).
		if side == SideEffectWriteFile {
			targets = append(targets, lexicalEditPathIdentity(t))
		} else {
			targets = append(targets, normalizeResource(t))
		}
	}
	if pa.FilePath != "" {
		// Write/edit: bind exact lexical path spelling (not path.Clean).
		// Other classes may still use display/path cleanup for non-auth convenience targets.
		if side == SideEffectWriteFile {
			targets = append(targets, lexicalEditPathIdentity(pa.FilePath))
		} else {
			targets = append(targets, normalizeResource(pa.FilePath))
		}
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
	// ASCII-only shell boundary trim — never strings.TrimSpace (strips NBSP/EM SPACE).
	cmd := trimShellASCIIWhitespace(pa.Command)
	switch pa.ToolClass {
	case adapter.ToolClassRead:
		return SideEffectRead, nil, nil
	case adapter.ToolClassSearch:
		return SideEffectSearch, nil, nil
	case adapter.ToolClassEdit:
		if pa.FilePath != "" {
			return SideEffectWriteFile, []string{lexicalEditPathIdentity(pa.FilePath)}, nil
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
	// Privileged delete identity only for shell tools — custom/other tools that merely
	// project a command string must not open delete_tree that Bash can RelBypass.
	if pa.ToolClass == adapter.ToolClassShell {
		// Compound / quoting / resolution ENV (PATH= etc.): never privileged delete identity.
		// Matches FullSuiteCommand fail-closed on executable-resolution overrides.
		unsafeShell := shellHasCompoundOrQuoting(cmd) || shellHasResolutionEnv(cmd)
		// Recursive (-r/-R/--recursive) is tree deletion; force is not required.
		if isRecursiveRm(cmd) {
			if unsafeShell || rmHasExitOnlyOrUnknownOption(cmd) {
				return SideEffectShellGeneric, extractPaths(cmd), nil
			}
			targets := extractRmTargets(cmd)
			if len(targets) > 0 {
				return SideEffectDeleteTree, targets, nil
			}
			return "", nil, fmt.Errorf("fingerprint: ambiguous recursive rm with no targets")
		}
		// find -delete only when find is bare command-position (case-sensitive).
		if shellArgv0(cmd) == "find" && reFindDelete.MatchString(cmd) {
			if unsafeShell || findFailClosedGlobals(cmd) {
				return SideEffectShellGeneric, extractPaths(cmd), nil
			}
			// Predicate-bearing find (e.g. -name) is not path-only delete_tree equality.
			if findHasExpressionPredicates(cmd) {
				return SideEffectShellGeneric, extractPaths(cmd), nil
			}
			targets := extractFindDeleteTargets(cmd)
			if len(targets) == 0 {
				return SideEffectShellGeneric, extractPaths(cmd), nil
			}
			return SideEffectDeleteTree, targets, nil
		}
		if isPlainRm(cmd) {
			if unsafeShell || rmHasExitOnlyOrUnknownOption(cmd) {
				return SideEffectShellGeneric, extractPaths(cmd), nil
			}
			targets := extractRmTargets(cmd)
			if len(targets) > 0 {
				return SideEffectDeleteFile, targets, nil
			}
			return "", nil, fmt.Errorf("fingerprint: ambiguous rm with no targets")
		}
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
		// ToolName stays in the outer fingerprint only so Bash/Shell variants of the
		// same command share OperationDigest and can be superseded by hard-deny barriers.
		return digestBytes([]byte(encodeFingerprintCanon(map[string]string{
			"cmd":     cmd,
			"args":    encodeStringList(pa.Arguments),
			"class":   pa.ToolClass,
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
		// Authorization binds exact lexical path spelling — not path.Clean (build ≠ build/).
		"path": lexicalEditPathIdentity(pa.FilePath),
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
		if s, ok, err := stringFieldsAgree(m, "old_string", "old_str"); err != nil {
			return "", err
		} else if ok {
			d := digestBytes([]byte(s))
			if oldDig != "" && oldDig != d {
				return "", fmt.Errorf("proposed_action: edit old content conflicts between args and payload")
			}
			oldDig = d
		}
		// Present empty string is a valid write/truncate surface (≠ missing key).
		// All present new-content aliases must agree (new_string vs content).
		if s, ok, err := stringFieldsAgree(m, "new_string", "new_str", "content", "contents"); err != nil {
			return "", err
		} else if ok {
			d := digestBytes([]byte(s))
			if newDig != "" && newDig != d {
				return "", fmt.Errorf("proposed_action: edit new content conflicts between args and payload")
			}
			newDig = d
		}
		// Bind replace_all only as JSON boolean (string "false" must not match false).
		if v, ok := m["replace_all"]; ok {
			b, ok := v.(bool)
			if !ok {
				return "", fmt.Errorf("proposed_action: edit replace_all must be JSON boolean")
			}
			parts["replace_all"] = fmt.Sprintf("%t", b)
		}
		// Path aliases must not diverge from ProposedAction.FilePath (only FilePath binds).
		// Compare lexical spelling so build/ vs build cannot agree via path.Clean.
		// Propagate stringFieldsAgree errors (conflicting/non-string aliases) like content fields.
		if s, ok, err := stringFieldsAgree(m, "file_path", "path"); err != nil {
			return "", err
		} else if ok {
			if lexicalEditPathIdentity(s) != lexicalEditPathIdentity(pa.FilePath) {
				return "", fmt.Errorf("proposed_action: edit payload path conflicts with FilePath")
			}
		}
	}

	if oldDig != "" {
		parts["old"] = oldDig
	}
	if newDig != "" {
		parts["new"] = newDig
	}

	if oldDig == "" && newDig == "" {
		if trimShellASCIIWhitespace(pa.Command) != "" {
			// Quote-preserving: same as shell digests (Fields must not collapse -c literals).
			parts["cmd"] = normalizeCommandForDigest(pa.Command)
		} else {
			return "", fmt.Errorf("proposed_action: edit operation missing content (args/payload/command)")
		}
	}
	return digestBytes([]byte(encodeFingerprintCanon(parts))), nil
}

func stringField(m map[string]any, keys ...string) string {
	s, ok, err := stringFieldsAgree(m, keys...)
	if err != nil || !ok || s == "" {
		return ""
	}
	return s
}

// stringFieldsAgree requires every present alias among keys to be a string and equal.
// Conflicting aliases (e.g. new_string vs content) fail closed.
func stringFieldsAgree(m map[string]any, keys ...string) (value string, present bool, err error) {
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		s, ok := v.(string)
		if !ok {
			return "", false, fmt.Errorf("proposed_action: edit payload key %q must be string", k)
		}
		if !present {
			value, present = s, true
			continue
		}
		if s != value {
			return "", false, fmt.Errorf("proposed_action: edit payload aliases conflict for key set")
		}
	}
	return value, present, nil
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
	fields, ok := shellFields(trimShellASCIIWhitespace(cmd))
	if !ok {
		// Unicode whitespace → fail closed for privileged classification.
		return true
	}
	for _, f := range fields {
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

// isShellASCIIWhitespace reports the closed set of shell field separators we accept
// for boundary/token handling: space, tab, and LF only.
// CR is intentionally excluded — Bash does not treat CR as a blank; trimming/splitting
// on CR would turn `rm -rf build\r` into a privileged `rm -rf build` identity.
func isShellASCIIWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\n':
		return true
	default:
		return false
	}
}

// trimShellASCIIWhitespace trims only space/tab/LF at boundaries.
// Unlike strings.TrimSpace, it does not remove NBSP, EM SPACE, CR, or other Unicode spaces.
func trimShellASCIIWhitespace(s string) string {
	start := 0
	for start < len(s) {
		r, size := utf8.DecodeRuneInString(s[start:])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !isShellASCIIWhitespace(r) {
			break
		}
		start += size
	}
	end := len(s)
	for end > start {
		r, size := utf8.DecodeLastRuneInString(s[:end])
		if r == utf8.RuneError && size == 1 {
			break
		}
		if !isShellASCIIWhitespace(r) {
			break
		}
		end -= size
	}
	return s[start:end]
}

// hasNonShellUnicodeWhitespace is true when any non-ASCII Unicode space appears.
// Such commands must not receive privileged shell side-effect identity.
func hasNonShellUnicodeWhitespace(s string) bool {
	for _, r := range s {
		if r >= 0x80 && unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// hasCarriageReturn is true when CR is present. Bash does not split on CR, so
// privileged shell identity must fail closed (do not treat CR as a field blank).
func hasCarriageReturn(s string) bool {
	return strings.Contains(s, "\r")
}

// shellFields splits on ASCII shell blanks only (space/tab/LF).
// ok is false if non-ASCII Unicode whitespace or CR appears — Bash would not treat
// those as separators, so privileged classification must fail closed.
func shellFields(cmd string) (fields []string, ok bool) {
	if hasNonShellUnicodeWhitespace(cmd) || hasCarriageReturn(cmd) {
		return nil, false
	}
	return strings.FieldsFunc(cmd, isShellASCIIWhitespace), true
}

// shellArgv0 returns the bare command-position name (first non ENV=val field).
// Path-qualified argv0 (./rm, /tmp/rm) returns "" so privileged delete/find
// classification fails closed — never path.Base (would treat ./rm as rm).
// When resolution ENV is present, returns "" (privileged classification unavailable).
func shellArgv0(cmd string) string {
	if shellHasResolutionEnv(cmd) {
		return ""
	}
	fields, ok := shellFields(trimShellASCIIWhitespace(cmd))
	if !ok {
		return ""
	}
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
	fields, ok := shellFields(trimShellASCIIWhitespace(cmd))
	if !ok {
		fields = nil
	}
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
// recursive flags (rewrite-eligible between rm -r and find -delete when targets match).
// Find traversal: last-wins single mode. Rm prompt: last-wins normalized mode.
func extractDeleteScopeFlags(cmd string) []string {
	fields, ok := shellFields(cmd)
	if !ok {
		return nil
	}
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	argv0 := fields[i]
	i++
	var out []string
	if argv0 == "rm" {
		// effectivePrompt: "all" | "once" | "none" | "" (default). Only all/once bind
		// so prompt_none/default can still rewrite-match find -delete.
		effectivePrompt := ""
		// preserve-root last-wins (do not accumulate+sort --preserve-root with --no-preserve-root).
		effectivePreserveRoot := ""
		dirRemoval := false
		oneFileSystem := false
		for ; i < len(fields); i++ {
			f := fields[i]
			if f == "--" {
				break
			}
			switch {
			case f == "--one-file-system":
				oneFileSystem = true
			case f == "--preserve-root":
				effectivePreserveRoot = "--preserve-root"
			case strings.HasPrefix(f, "--preserve-root="):
				effectivePreserveRoot = f // retain exact form e.g. --preserve-root=all
			case f == "--no-preserve-root":
				effectivePreserveRoot = "--no-preserve-root"
			case f == "--dir" || f == "-d":
				dirRemoval = true
			case f == "--force":
				effectivePrompt = "none"
			case f == "-i":
				effectivePrompt = "all"
			case f == "-I":
				effectivePrompt = "once"
			case f == "--interactive":
				effectivePrompt = "all"
			case strings.HasPrefix(f, "--interactive="):
				mode, ok := parseInteractiveValue(strings.TrimPrefix(f, "--interactive="))
				if ok {
					effectivePrompt = mode
				}
			case strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") && f != "-":
				for _, r := range f[1:] {
					switch r {
					case 'f':
						effectivePrompt = "none"
					case 'i':
						effectivePrompt = "all"
					case 'I':
						effectivePrompt = "once"
					case 'd':
						dirRemoval = true
					case 'r', 'R', 'v':
						// recursive/verbose not scope beyond class
					default:
						out = append(out, string(r))
					}
				}
			}
		}
		if oneFileSystem {
			out = append(out, "--one-file-system")
		}
		if effectivePreserveRoot != "" {
			out = append(out, effectivePreserveRoot)
		}
		if dirRemoval {
			out = append(out, "dir_removal")
		}
		if effectivePrompt == "all" || effectivePrompt == "once" {
			out = append(out, "prompt="+effectivePrompt)
		}
	}
	if argv0 == "find" {
		// Last-wins traversal mode only (sorting would erase -P -L vs -L -P).
		effectiveTraversal := ""
		for ; i < len(fields); i++ {
			f := fields[i]
			if f == "--" {
				break
			}
			if f == "-H" || f == "-L" || f == "-P" {
				effectiveTraversal = f
				continue
			}
			if isValidFindOLevel(f) {
				out = append(out, f)
				continue
			}
			if f == "-D" {
				i++ // skip following debugopts if present
				continue
			}
			if strings.HasPrefix(f, "-D") && len(f) > 2 {
				continue
			}
			// path or expression — stop global option scan
			break
		}
		if effectiveTraversal != "" {
			out = append(out, "traversal="+effectiveTraversal)
		}
	}
	sort.Strings(out)
	return out
}

// parseInteractiveValue maps GNU rm --interactive=VALUE aliases to closed modes.
// Returns ok=false for unsupported values (caller fail-closed).
func parseInteractiveValue(v string) (mode string, ok bool) {
	switch v {
	case "always", "yes":
		return "all", true
	case "once":
		return "once", true
	case "never", "no", "none":
		return "none", true
	default:
		return "", false
	}
}

// isValidFindOLevel reports GNU find [-Olevel] with an attached decimal integer.
// Bare `-O` or `-Obuild` is invalid and must not be treated as a skippable global.
func isValidFindOLevel(f string) bool {
	if !strings.HasPrefix(f, "-O") || len(f) < 3 {
		return false
	}
	for _, r := range f[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// findFailClosedGlobals is true for malformed -O or exit-only -D help (no privileged delete).
func findFailClosedGlobals(cmd string) bool {
	if shellArgv0(cmd) != "find" {
		return false
	}
	fields, ok := shellFields(cmd)
	if !ok {
		return true // Unicode whitespace → fail closed for privileged find
	}
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return false
	}
	i++ // skip find
	for i < len(fields) {
		f := fields[i]
		if f == "--" {
			break
		}
		if f == "-H" || f == "-L" || f == "-P" {
			i++
			continue
		}
		if strings.HasPrefix(f, "-O") {
			if !isValidFindOLevel(f) {
				return true
			}
			i++
			continue
		}
		if f == "-D" {
			i++
			if i < len(fields) {
				if fields[i] == "help" {
					return true // exit-only debug help
				}
				i++
			}
			continue
		}
		if strings.HasPrefix(f, "-D") && len(f) > 2 {
			if f == "-Dhelp" {
				return true
			}
			i++
			continue
		}
		if f == "--help" || f == "--version" {
			return true
		}
		break
	}
	return false
}

// rmHasExitOnlyOrUnknownOption is true when rm carries --help/--version or an
// unknown/wrong-case long option before `--`. Such commands must not get privileged
// delete identity (e.g. `rm --help build`, `rm --FORCE build`, `rm --directory build`).
func rmHasExitOnlyOrUnknownOption(cmd string) bool {
	if shellArgv0(cmd) != "rm" {
		return false
	}
	fields, ok := shellFields(cmd)
	if !ok {
		return true
	}
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return false
	}
	i++ // skip rm
	for ; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			break
		}
		if f == "--help" || f == "--version" {
			return true
		}
		if strings.HasPrefix(f, "--") {
			// Case-sensitive closed allowlist (GNU long options are lowercase).
			// Note: --directory is NOT a GNU rm option (only -d / --dir).
			switch {
			case f == "--recursive" || f == "--force" || f == "--verbose" ||
				f == "--dir" ||
				f == "--one-file-system" || f == "--preserve-root" ||
				strings.HasPrefix(f, "--preserve-root=") || f == "--no-preserve-root" ||
				f == "--interactive":
				// known
			case strings.HasPrefix(f, "--interactive="):
				if _, ok := parseInteractiveValue(strings.TrimPrefix(f, "--interactive=")); !ok {
					return true
				}
			default:
				return true
			}
			continue
		}
		// Lone "-" is a filename operand, not an option.
		if f == "-" {
			continue
		}
		// short options: case-sensitive letters as GNU accepts (r/R/f/v/i/I/d)
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			for _, r := range f[1:] {
				switch r {
				case 'r', 'R', 'f', 'v', 'i', 'I', 'd':
				default:
					return true
				}
			}
		}
	}
	return false
}

// findHasExpressionPredicates reports find expression tokens beyond path roots and -delete.
// e.g. -name, -type, -path, ! EXPR make delete scope unequal to plain path -delete.
func findHasExpressionPredicates(cmd string) bool {
	fields, ok := shellFields(cmd)
	if !ok {
		return true
	}
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) || fields[i] != "find" {
		return false
	}
	i++
	// skip global options (only valid forms)
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
		if isValidFindOLevel(f) {
			i++
			continue
		}
		if strings.HasPrefix(f, "-O") {
			break
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
	// skip path roots (not operators like ! or () )
	for i < len(fields) {
		f := fields[i]
		if f == "!" || f == "(" || f == ")" || strings.HasPrefix(f, "-") {
			break
		}
		i++
	}
	// remaining expression: only the sole token -delete is rewrite-eligible.
	expr := fields[i:]
	if len(expr) != 1 || expr[0] != "-delete" {
		return true
	}
	return false
}

// isRecursiveRm reports -r/-R/--recursive before `--` (force not required).
func isRecursiveRm(cmd string) bool {
	if shellArgv0(cmd) != "rm" {
		return false
	}
	fields, ok := shellFields(cmd)
	if !ok {
		return false
	}
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return false
	}
	i++ // skip rm
	for ; i < len(fields); i++ {
		f := fields[i]
		if f == "--" {
			break
		}
		if f == "--recursive" {
			return true
		}
		if f == "-" {
			continue // operand
		}
		if strings.HasPrefix(f, "-") && !strings.HasPrefix(f, "--") {
			if strings.Contains(f, "r") || strings.Contains(f, "R") {
				return true
			}
		}
	}
	return false
}

func isPlainRm(cmd string) bool {
	if shellArgv0(cmd) != "rm" {
		return false
	}
	if isRecursiveRm(cmd) {
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
	if shellArgv0(cmd) != "rm" {
		return nil
	}
	fields, ok := shellFields(cmd)
	if !ok {
		return nil
	}
	var out []string
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	i++ // skip argv0 (rm)
	// After `--`, every token is an operand (including names starting with `-`).
	// Lone "-" is always an operand (filename), never an option.
	afterDashDash := false
	for ; i < len(fields); i++ {
		f := fields[i]
		if !afterDashDash {
			if f == "--" {
				afterDashDash = true
				continue
			}
			if f == "-" {
				out = append(out, normalizeDeleteTarget(f))
				continue
			}
			if strings.HasPrefix(f, "-") {
				continue // option
			}
		}
		out = append(out, normalizeDeleteTarget(f))
	}
	return out
}

func extractFindDeleteTargets(cmd string) []string {
	if shellArgv0(cmd) != "find" {
		return nil
	}
	fields, ok := shellFields(cmd)
	if !ok {
		return nil
	}
	var out []string
	i := skipShellEnvPrefixes(fields)
	if i >= len(fields) {
		return nil
	}
	i++ // skip find
	// GNU find: find [-HLP] [-Olevel] [-D debugopts] [--] [path...] [expression]
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
		if isValidFindOLevel(f) {
			i++
			continue
		}
		if strings.HasPrefix(f, "-O") {
			break
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
	// Path roots until expression (dash option, ! , or parentheses).
	for ; i < len(fields); i++ {
		f := fields[i]
		if f == "!" || f == "(" || f == ")" || strings.HasPrefix(f, "-") {
			break
		}
		out = append(out, normalizeDeleteTarget(f))
	}
	return out
}

func extractPaths(cmd string) []string {
	fields := strings.Fields(cmd)
	var out []string
	for _, f := range fields {
		if f == "-" {
			out = append(out, normalizeResource(f))
			continue
		}
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

// normalizeResource is general path cleanup for non-authorization convenience targets
// (read/search/shell path extraction). It must NOT authorize write/edit identity.
func normalizeResource(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = path.Clean(s)
	s = strings.TrimPrefix(s, "./")
	return s
}

// lexicalEditPathIdentity is the authorization identity for ToolClassEdit / write_file.
// Retains exact bounded lexical spelling: build ≠ build/ ≠ build/. ≠ ./build ≠ Build.
// Does not apply path.Clean, filepath.Clean, or case folding.
func lexicalEditPathIdentity(s string) string {
	return s
}

// normalizeDeleteTarget preserves deletion-sensitive path spelling that path.Clean
// would erase: trailing slash (build/), trailing /. (build/.), and interior . / ..
// components (file/../victim ≠ victim). rm of a non-directory path with /.. may be a
// no-op under -f while the cleaned form names a different real target.
func normalizeDeleteTarget(s string) string {
	// Operand tokens come from shellFields (ASCII-split); only ASCII boundary trim.
	s = trimShellASCIIWhitespace(s)
	s = strings.Trim(s, `"'`)
	if s == "" || s == "-" {
		return s
	}
	raw := s
	// Any . or .. path component: bind exact operand spelling (no path.Clean).
	if deleteOperandHasDotComponent(raw) {
		return raw
	}
	hadSlash := len(raw) > 1 && strings.HasSuffix(raw, "/")
	cleaned := path.Clean(raw)
	cleaned = strings.TrimPrefix(cleaned, "./")
	if hadSlash && cleaned != "/" && !strings.HasSuffix(cleaned, "/") {
		cleaned += "/"
	}
	return cleaned
}

// deleteOperandHasDotComponent reports path segments "." or ".." (including bare
// "." / ".." and trailing "/." "/.."). These must not be cleaned away.
func deleteOperandHasDotComponent(p string) bool {
	if p == "." || p == ".." {
		return true
	}
	// Walk segments without path.Clean.
	start := 0
	for i := 0; i <= len(p); i++ {
		if i == len(p) || p[i] == '/' {
			seg := p[start:i]
			if seg == "." || seg == ".." {
				return true
			}
			start = i + 1
		}
	}
	return false
}

// normalizeCommandForDigest is the identity encoding for shell command digests.
//   - Non-shell Unicode whitespace or CR: retain exact spelling after ASCII-only
//     boundary trim (never strings.TrimSpace / CR→LF rewrite that would strip or
//     collapse shell-foreign control characters).
//   - Quotes/escapes present: retain exact ASCII-trimmed command only (no Fields
//     collapse — e.g. python -c 'if "a  b"' vs 'if "a b"' must differ).
//   - Otherwise: collapse horizontal space/tab; preserve newlines as separators.
func normalizeCommandForDigest(cmd string) string {
	cmd = trimShellASCIIWhitespace(cmd)
	if cmd == "" {
		return ""
	}
	// Unicode shell-foreign whitespace or CR: keep exact bounded spelling.
	if hasNonShellUnicodeWhitespace(cmd) || hasCarriageReturn(cmd) {
		return cmd
	}
	// Quoted/escaped interiors: identity after ASCII boundary trim only.
	if strings.ContainsAny(cmd, `'"\\`) {
		return cmd
	}
	return normalizeCommandPreserveCase(cmd)
}

// normalizeCommandPreserveCase collapses horizontal space/tab only.
// Newlines are preserved as separators so multi-command lines cannot collide with
// space-joined single commands (echo ok\nrm -rf build ≠ echo ok rm -rf build).
// Prefer normalizeCommandForDigest for fingerprint digests.
// Caller must ensure no CR / non-shell Unicode whitespace.
func normalizeCommandPreserveCase(cmd string) string {
	cmd = trimShellASCIIWhitespace(cmd)
	if cmd == "" {
		return ""
	}
	joinASCIIFields := func(p string) string {
		fields, ok := shellFields(p)
		if !ok {
			return p
		}
		return strings.Join(fields, " ")
	}
	if strings.Contains(cmd, "\n") {
		parts := strings.Split(cmd, "\n")
		for i, p := range parts {
			parts[i] = joinASCIIFields(p)
		}
		return strings.Join(parts, "\n")
	}
	return joinASCIIFields(cmd)
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
		// Preserve "." — cwd is a real delete operand (rm -r . build ≠ rm -r build).
		// Only drop empty strings from dedupe.
		if s == "" {
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
