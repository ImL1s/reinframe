package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RuntimeAuthSnapshotSchemaVersion is the closed schema version for runtime auth snapshot.
const RuntimeAuthSnapshotSchemaVersion = "reinframe.runtime_auth_snapshot.v1"

// CredentialOwner defines who holds and manages runtime credentials.
type CredentialOwner string

const (
	// CredentialOwnerCodexProcess delegates authentication to the child codex process.
	// Reinframe never opens, reads, or parses credential stores (e.g. ~/.codex/auth.json).
	CredentialOwnerCodexProcess CredentialOwner = "codex_process"

	// CredentialOwnerReinframeEnv indicates credentials are provided via Reinframe environment variables.
	CredentialOwnerReinframeEnv CredentialOwner = "reinframe_env"
)

// Valid reports whether the CredentialOwner is one of the closed enum values.
func (co CredentialOwner) Valid() bool {
	switch co {
	case CredentialOwnerCodexProcess, CredentialOwnerReinframeEnv:
		return true
	default:
		return false
	}
}

// RuntimeAuthMode identifies the authentication mechanism in effect.
type RuntimeAuthMode string

const (
	// RuntimeAuthModeChatGPTSubscription indicates login through ChatGPT subscription / OAuth flow.
	RuntimeAuthModeChatGPTSubscription RuntimeAuthMode = "chatgpt_subscription"

	// RuntimeAuthModeAPIKey indicates direct API key usage.
	RuntimeAuthModeAPIKey RuntimeAuthMode = "api_key"

	// RuntimeAuthModeUnknown indicates unresolved or unrecognized auth mode.
	RuntimeAuthModeUnknown RuntimeAuthMode = "unknown"
)

// Valid reports whether the RuntimeAuthMode is one of the closed enum values.
func (m RuntimeAuthMode) Valid() bool {
	switch m {
	case RuntimeAuthModeChatGPTSubscription, RuntimeAuthModeAPIKey, RuntimeAuthModeUnknown:
		return true
	default:
		return false
	}
}

// IsSubscription reports whether the auth mode is ChatGPT subscription.
func (m RuntimeAuthMode) IsSubscription() bool {
	return m == RuntimeAuthModeChatGPTSubscription
}

// IsAPIKey reports whether the auth mode is API key.
func (m RuntimeAuthMode) IsAPIKey() bool {
	return m == RuntimeAuthModeAPIKey
}

// RuntimeAuthState represents the current readiness and validity of the authentication.
type RuntimeAuthState string

const (
	// RuntimeAuthStateAuthenticated means credentials are present, valid, and active.
	RuntimeAuthStateAuthenticated RuntimeAuthState = "authenticated"

	// RuntimeAuthStateUnauthenticated means no credentials exist; operator login action required.
	RuntimeAuthStateUnauthenticated RuntimeAuthState = "unauthenticated"

	// RuntimeAuthStateExpired means credentials have expired; turn progression must halt immediately.
	RuntimeAuthStateExpired RuntimeAuthState = "expired"

	// RuntimeAuthStateUnavailable means runtime auth cannot be probed or binary/service is unavailable.
	RuntimeAuthStateUnavailable RuntimeAuthState = "unavailable"

	// RuntimeAuthStateUnknown means state could not be determined.
	RuntimeAuthStateUnknown RuntimeAuthState = "unknown"
)

// Valid reports whether the RuntimeAuthState is one of the closed enum values.
func (s RuntimeAuthState) Valid() bool {
	switch s {
	case RuntimeAuthStateAuthenticated,
		RuntimeAuthStateUnauthenticated,
		RuntimeAuthStateExpired,
		RuntimeAuthStateUnavailable,
		RuntimeAuthStateUnknown:
		return true
	default:
		return false
	}
}

// Common errors for runtime auth validation and matching.
var (
	ErrInvalidSchemaVersion     = errors.New("auth: invalid schema version")
	ErrInvalidCredentialOwner   = errors.New("auth: invalid credential owner")
	ErrInvalidAuthMode          = errors.New("auth: invalid auth mode")
	ErrInvalidAuthState         = errors.New("auth: invalid auth state")
	ErrMissingScopeHash         = errors.New("auth: scope hash is required")
	ErrMissingAuthGenHash       = errors.New("auth: auth generation hash is required")
	ErrMissingRuntimeProfile    = errors.New("auth: runtime profile is required")
	ErrZeroObservedAt           = errors.New("auth: observed_at timestamp is zero")
	ErrAuthModeMismatch         = errors.New("auth: runtime auth mode mismatch")
	ErrAuthNotReady             = errors.New("auth: runtime auth is not authenticated")
	ErrProhibitedSecretDetected = errors.New("auth: prohibited raw secret token detected in metadata")
)

// RuntimeAuthSnapshot captures a point-in-time, secret-free projection of the runtime auth state.
// Crucially, it MUST NEVER contain secrets, tokens, cookies, or raw auth.json content.
type RuntimeAuthSnapshot struct {
	SchemaVersion      string            `json:"schema_version" redact:"none"`
	CredentialOwner    CredentialOwner   `json:"credential_owner" redact:"none"`
	Mode               RuntimeAuthMode   `json:"mode" redact:"none"`
	State              RuntimeAuthState  `json:"state" redact:"none"`
	RuntimeProfile     string            `json:"runtime_profile" redact:"none"`
	CodexVersion       string            `json:"codex_version,omitempty" redact:"none"`
	ScopeHash          string            `json:"scope_hash" redact:"none"`
	AuthGenerationHash string            `json:"auth_generation_hash" redact:"none"`
	ObservedAt         time.Time         `json:"observed_at" redact:"none"`
	Metadata           map[string]string `json:"metadata,omitempty" redact:"sanitize"`
}

// Validate verifies structural correctness, enum validity, bounded hashes, and absence of raw secrets.
func (s RuntimeAuthSnapshot) Validate() error {
	if s.SchemaVersion != RuntimeAuthSnapshotSchemaVersion {
		return fmt.Errorf("%w: got %q, want %q", ErrInvalidSchemaVersion, s.SchemaVersion, RuntimeAuthSnapshotSchemaVersion)
	}
	if !s.CredentialOwner.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCredentialOwner, s.CredentialOwner)
	}
	if !s.Mode.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAuthMode, s.Mode)
	}
	if !s.State.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidAuthState, s.State)
	}
	if strings.TrimSpace(s.RuntimeProfile) == "" {
		return ErrMissingRuntimeProfile
	}
	if strings.TrimSpace(s.ScopeHash) == "" {
		return ErrMissingScopeHash
	}
	if strings.TrimSpace(s.AuthGenerationHash) == "" {
		return ErrMissingAuthGenHash
	}
	if s.ObservedAt.IsZero() {
		return ErrZeroObservedAt
	}

	// Secret leak prevention: ensure metadata doesn't contain raw secrets
	for k, v := range s.Metadata {
		if isProhibitedSecretValue(k) || isProhibitedSecretValue(v) {
			return ErrProhibitedSecretDetected
		}
	}
	return nil
}

// IsAuthenticated reports whether credentials are valid and active.
func (s RuntimeAuthSnapshot) IsAuthenticated() bool {
	return s.State == RuntimeAuthStateAuthenticated
}

// RequiresOperatorAction reports whether an interactive operator action is needed.
func (s RuntimeAuthSnapshot) RequiresOperatorAction() bool {
	switch s.State {
	case RuntimeAuthStateUnauthenticated, RuntimeAuthStateExpired, RuntimeAuthStateUnavailable:
		return true
	default:
		return false
	}
}

// CacheKeyPartition returns a stable, bounded partition string for caching.
// Any change in Profile, Mode, ScopeHash, or AuthGenerationHash changes the partition key.
func (s RuntimeAuthSnapshot) CacheKeyPartition() string {
	return fmt.Sprintf("prof=%s|owner=%s|mode=%s|scope=%s|authgen=%s",
		s.RuntimeProfile, s.CredentialOwner, s.Mode, s.ScopeHash, s.AuthGenerationHash)
}

// MatchesRequirement verifies if snapshot satisfies a required auth mode (e.g. "chatgpt_subscription").
func (s RuntimeAuthSnapshot) MatchesRequirement(requiredAuth string) error {
	req := strings.TrimSpace(strings.ToLower(requiredAuth))
	if req == "" {
		req = string(RuntimeAuthModeChatGPTSubscription)
	}
	if string(s.Mode) != req {
		return fmt.Errorf("%w: required %q, actual mode is %q (state: %q)", ErrAuthModeMismatch, req, s.Mode, s.State)
	}
	if s.State != RuntimeAuthStateAuthenticated {
		return fmt.Errorf("%w: mode %q is not authenticated (state: %q)", ErrAuthNotReady, s.Mode, s.State)
	}
	return nil
}

// redactedMetadata returns metadata stripped of any potential secret-like tokens.
func (s RuntimeAuthSnapshot) redactedMetadata() map[string]string {
	if len(s.Metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.Metadata))
	for k, v := range s.Metadata {
		if isProhibitedSecretValue(k) || isProhibitedSecretValue(v) {
			out[k] = "[REDACTED]"
		} else {
			out[k] = v
		}
	}
	return out
}

// String provides a diagnostics-safe summary with bounded hashes and zero secrets.
func (s RuntimeAuthSnapshot) String() string {
	return fmt.Sprintf(
		"RuntimeAuthSnapshot{owner:%s mode:%s state:%s profile:%s version:%q scope_hash:%s auth_gen_hash:%s observed:%s}",
		s.CredentialOwner, s.Mode, s.State, s.RuntimeProfile, s.CodexVersion,
		truncateHash(s.ScopeHash, 12), truncateHash(s.AuthGenerationHash, 12),
		s.ObservedAt.UTC().Format(time.RFC3339),
	)
}

// GoString implements fmt.GoStringer.
func (s RuntimeAuthSnapshot) GoString() string {
	return s.String()
}

// Format implements fmt.Formatter to prevent accidental secret leakage via %v / %+v / %#v.
func (s RuntimeAuthSnapshot) Format(f fmt.State, verb rune) {
	switch verb {
	case 'v', 's':
		if f.Flag('#') {
			_, _ = fmt.Fprint(f, s.GoString())
			return
		}
		_, _ = fmt.Fprint(f, s.String())
	default:
		_, _ = fmt.Fprint(f, s.String())
	}
}

// MarshalJSON ensures metadata is redacted if any suspicious strings are present.
func (s RuntimeAuthSnapshot) MarshalJSON() ([]byte, error) {
	type alias RuntimeAuthSnapshot
	out := alias(s)
	if out.SchemaVersion == "" {
		out.SchemaVersion = RuntimeAuthSnapshotSchemaVersion
	}
	out.Metadata = s.redactedMetadata()
	return json.Marshal(out)
}

// ComputeScopeHash computes a deterministic, 64-character SHA-256 hex digest of sorted scope items.
func ComputeScopeHash(scopes []string) string {
	if len(scopes) == 0 {
		h := sha256.Sum256([]byte("<global_unscoped>"))
		return hex.EncodeToString(h[:])
	}
	sorted := make([]string, len(scopes))
	copy(sorted, scopes)
	sort.Strings(sorted)

	hasher := sha256.New()
	for _, sc := range sorted {
		hasher.Write([]byte(strings.TrimSpace(sc)))
		hasher.Write([]byte{'\n'})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// ComputeAuthGenerationHash computes a deterministic, 64-character SHA-256 hex digest
// from non-secret identifiers (e.g. profile, session ID, status epoch/serial).
// Secret tokens or passwords MUST NEVER be passed as arguments.
func ComputeAuthGenerationHash(profile string, generationFingerprint string) string {
	hasher := sha256.New()
	hasher.Write([]byte("reinframe_auth_gen_v1:"))
	hasher.Write([]byte(strings.TrimSpace(profile)))
	hasher.Write([]byte(":"))
	hasher.Write([]byte(strings.TrimSpace(generationFingerprint)))
	return hex.EncodeToString(hasher.Sum(nil))
}

// NewRuntimeAuthSnapshot constructs and validates a new RuntimeAuthSnapshot.
func NewRuntimeAuthSnapshot(
	owner CredentialOwner,
	mode RuntimeAuthMode,
	state RuntimeAuthState,
	profile string,
	codexVersion string,
	scopeHash string,
	authGenHash string,
	observedAt time.Time,
	metadata map[string]string,
) (RuntimeAuthSnapshot, error) {
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	snap := RuntimeAuthSnapshot{
		SchemaVersion:      RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    owner,
		Mode:               mode,
		State:              state,
		RuntimeProfile:     profile,
		CodexVersion:       codexVersion,
		ScopeHash:          scopeHash,
		AuthGenerationHash: authGenHash,
		ObservedAt:         observedAt.UTC(),
		Metadata:           metadata,
	}
	if err := snap.Validate(); err != nil {
		return RuntimeAuthSnapshot{}, err
	}
	return snap, nil
}

// Helper functions for secret scanning & hash truncation.

func isProhibitedSecretValue(s string) bool {
	lower := strings.ToLower(s)
	// Check for common token patterns and secret markers
	if strings.HasPrefix(lower, "sk-") ||
		strings.HasPrefix(lower, "bearer ") ||
		strings.Contains(lower, "refresh_token") ||
		strings.Contains(lower, "oauth_token") ||
		strings.Contains(lower, "session_token") ||
		strings.Contains(lower, "auth.json") ||
		strings.Contains(lower, "api_key") ||
		strings.Contains(lower, "password") ||
		strings.Contains(lower, "client_secret") {
		return true
	}
	// Check for JWT token pattern (ey...)
	if strings.HasPrefix(s, "eyJ") && len(s) > 30 {
		return true
	}
	return false
}

func truncateHash(h string, maxLen int) string {
	if len(h) <= maxLen {
		return h
	}
	return h[:maxLen]
}
