package protocol_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestCredentialOwner_Valid(t *testing.T) {
	t.Parallel()

	validOwners := []protocol.CredentialOwner{
		protocol.CredentialOwnerCodexProcess,
		protocol.CredentialOwnerReinframeEnv,
	}
	for _, o := range validOwners {
		if !o.Valid() {
			t.Errorf("expected %q to be valid", o)
		}
	}

	invalidOwners := []protocol.CredentialOwner{
		"",
		"root",
		"user",
		"reinframe_daemon",
	}
	for _, o := range invalidOwners {
		if o.Valid() {
			t.Errorf("expected %q to be invalid", o)
		}
	}
}

func TestRuntimeAuthMode_Valid(t *testing.T) {
	t.Parallel()

	validModes := []protocol.RuntimeAuthMode{
		protocol.RuntimeAuthModeChatGPTSubscription,
		protocol.RuntimeAuthModeAPIKey,
		protocol.RuntimeAuthModeUnknown,
	}
	for _, m := range validModes {
		if !m.Valid() {
			t.Errorf("expected %q to be valid", m)
		}
	}

	if !protocol.RuntimeAuthModeChatGPTSubscription.IsSubscription() {
		t.Error("expected IsSubscription() to be true for chatgpt_subscription")
	}
	if protocol.RuntimeAuthModeAPIKey.IsSubscription() {
		t.Error("expected IsSubscription() to be false for api_key")
	}
	if !protocol.RuntimeAuthModeAPIKey.IsAPIKey() {
		t.Error("expected IsAPIKey() to be true for api_key")
	}
	if protocol.RuntimeAuthModeChatGPTSubscription.IsAPIKey() {
		t.Error("expected IsAPIKey() to be false for chatgpt_subscription")
	}

	invalidModes := []protocol.RuntimeAuthMode{"", "oauth2", "bearer", "cookie"}
	for _, m := range invalidModes {
		if m.Valid() {
			t.Errorf("expected %q to be invalid", m)
		}
	}
}

func TestRuntimeAuthState_Valid(t *testing.T) {
	t.Parallel()

	validStates := []protocol.RuntimeAuthState{
		protocol.RuntimeAuthStateAuthenticated,
		protocol.RuntimeAuthStateUnauthenticated,
		protocol.RuntimeAuthStateExpired,
		protocol.RuntimeAuthStateUnavailable,
		protocol.RuntimeAuthStateUnknown,
	}
	for _, s := range validStates {
		if !s.Valid() {
			t.Errorf("expected %q to be valid", s)
		}
	}

	invalidStates := []protocol.RuntimeAuthState{"", "ready", "logged_in", "error"}
	for _, s := range invalidStates {
		if s.Valid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestComputeScopeHash(t *testing.T) {
	t.Parallel()

	// Empty scope should be deterministic
	h1 := protocol.ComputeScopeHash(nil)
	h2 := protocol.ComputeScopeHash([]string{})
	if h1 != h2 {
		t.Fatalf("nil and empty scopes gave different hashes: %q != %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got len=%d", len(h1))
	}

	// Order independence: sorted determinism
	s1 := []string{"src/auth", "pkg/protocol", "cmd/codex"}
	s2 := []string{"pkg/protocol", "cmd/codex", "src/auth"}
	hs1 := protocol.ComputeScopeHash(s1)
	hs2 := protocol.ComputeScopeHash(s2)
	if hs1 != hs2 {
		t.Fatalf("order altered hash: %q != %q", hs1, hs2)
	}

	// Scope changes change hash
	s3 := []string{"src/auth", "pkg/protocol"}
	hs3 := protocol.ComputeScopeHash(s3)
	if hs1 == hs3 {
		t.Fatalf("different scopes yielded identical hash: %q", hs1)
	}
}

func TestComputeAuthGenerationHash(t *testing.T) {
	t.Parallel()

	g1 := protocol.ComputeAuthGenerationHash("default", "gen-12345")
	g2 := protocol.ComputeAuthGenerationHash("default", "gen-12345")
	if g1 != g2 {
		t.Fatalf("same profile & seed produced different hashes: %q != %q", g1, g2)
	}
	if len(g1) != 64 {
		t.Fatalf("expected 64-char hex hash, got len=%d", len(g1))
	}

	g3 := protocol.ComputeAuthGenerationHash("default", "gen-99999")
	if g1 == g3 {
		t.Fatalf("different seeds yielded identical hash: %q", g1)
	}

	g4 := protocol.ComputeAuthGenerationHash("staging", "gen-12345")
	if g1 == g4 {
		t.Fatalf("different profiles yielded identical hash: %q", g1)
	}
}

func TestRuntimeAuthSnapshot_Validation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scopeHash := protocol.ComputeScopeHash([]string{"pkg/protocol"})
	authGenHash := protocol.ComputeAuthGenerationHash("default", "gen-1")

	validSnap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          scopeHash,
		AuthGenerationHash: authGenHash,
		ObservedAt:         now,
		Metadata: map[string]string{
			"endpoint": "https://api.openai.com",
		},
	}

	if err := validSnap.Validate(); err != nil {
		t.Fatalf("expected valid snapshot, got err: %v", err)
	}
	if !validSnap.IsAuthenticated() {
		t.Fatal("expected IsAuthenticated() == true")
	}
	if validSnap.RequiresOperatorAction() {
		t.Fatal("expected RequiresOperatorAction() == false for authenticated state")
	}

	// SchemaVersion mismatch
	snapBadVer := validSnap
	snapBadVer.SchemaVersion = "reinframe.runtime_auth_snapshot.v2"
	if err := snapBadVer.Validate(); err == nil {
		t.Fatal("expected error on invalid schema version")
	}

	// Invalid CredentialOwner
	snapBadOwner := validSnap
	snapBadOwner.CredentialOwner = "invalid_owner"
	if err := snapBadOwner.Validate(); err == nil {
		t.Fatal("expected error on invalid credential owner")
	}

	// Invalid Mode
	snapBadMode := validSnap
	snapBadMode.Mode = "invalid_mode"
	if err := snapBadMode.Validate(); err == nil {
		t.Fatal("expected error on invalid auth mode")
	}

	// Invalid State
	snapBadState := validSnap
	snapBadState.State = "invalid_state"
	if err := snapBadState.Validate(); err == nil {
		t.Fatal("expected error on invalid auth state")
	}

	// Missing ScopeHash
	snapBadScope := validSnap
	snapBadScope.ScopeHash = ""
	if err := snapBadScope.Validate(); err == nil {
		t.Fatal("expected error on missing scope hash")
	}

	// Missing AuthGenerationHash
	snapBadGen := validSnap
	snapBadGen.AuthGenerationHash = ""
	if err := snapBadGen.Validate(); err == nil {
		t.Fatal("expected error on missing auth generation hash")
	}

	// Zero ObservedAt
	snapBadTime := validSnap
	snapBadTime.ObservedAt = time.Time{}
	if err := snapBadTime.Validate(); err == nil {
		t.Fatal("expected error on zero observed_at")
	}
}

func TestRuntimeAuthSnapshot_SecretRejection(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scopeHash := protocol.ComputeScopeHash(nil)
	authGenHash := protocol.ComputeAuthGenerationHash("default", "gen-1")

	// Injecting prohibited raw secret token into metadata must be rejected
	prohibitedTokens := []string{
		"sk-proj-abc12345678901234567890",
		"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
		"refresh_token_abcdef123456",
		"~/.codex/auth.json",
		"oauth_token_secret_data",
	}

	for _, secret := range prohibitedTokens {
		snap := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
			State:              protocol.RuntimeAuthStateAuthenticated,
			RuntimeProfile:     "default",
			ScopeHash:          scopeHash,
			AuthGenerationHash: authGenHash,
			ObservedAt:         now,
			Metadata: map[string]string{
				"token": secret,
			},
		}

		if err := snap.Validate(); err == nil {
			t.Fatalf("expected validation error for prohibited secret %q in metadata", secret)
		}
	}
}

func TestRuntimeAuthSnapshot_MatchesRequirement(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scopeHash := protocol.ComputeScopeHash(nil)
	authGenHash := protocol.ComputeAuthGenerationHash("default", "gen-1")

	snapSub := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		ScopeHash:          scopeHash,
		AuthGenerationHash: authGenHash,
		ObservedAt:         now,
	}

	// ChatGPT subscription matches chatgpt_subscription requirement
	if err := snapSub.MatchesRequirement("chatgpt_subscription"); err != nil {
		t.Fatalf("expected match for chatgpt_subscription: %v", err)
	}

	// Mismatch: subscription does not match api_key requirement
	if err := snapSub.MatchesRequirement("api_key"); err == nil {
		t.Fatal("expected error when requiring api_key on chatgpt_subscription snapshot")
	}

	// Mismatch: api_key mode does not match chatgpt_subscription requirement
	snapAPI := snapSub
	snapAPI.Mode = protocol.RuntimeAuthModeAPIKey
	if err := snapAPI.MatchesRequirement("chatgpt_subscription"); err == nil {
		t.Fatal("expected error when requiring chatgpt_subscription on api_key snapshot")
	}

	// Unauthenticated state fails MatchesRequirement even if mode is right
	snapUnauth := snapSub
	snapUnauth.State = protocol.RuntimeAuthStateUnauthenticated
	if err := snapUnauth.MatchesRequirement("chatgpt_subscription"); err == nil {
		t.Fatal("expected error when state is unauthenticated")
	}

	// Expired state fails MatchesRequirement
	snapExpired := snapSub
	snapExpired.State = protocol.RuntimeAuthStateExpired
	if err := snapExpired.MatchesRequirement("chatgpt_subscription"); err == nil {
		t.Fatal("expected error when state is expired")
	}
}

func TestRuntimeAuthSnapshot_CacheKeyPartition(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scopeHash1 := protocol.ComputeScopeHash([]string{"pkg/auth"})
	scopeHash2 := protocol.ComputeScopeHash([]string{"pkg/reviewer"})
	authGen1 := protocol.ComputeAuthGenerationHash("default", "gen-1")
	authGen2 := protocol.ComputeAuthGenerationHash("default", "gen-2")

	snap1 := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		ScopeHash:          scopeHash1,
		AuthGenerationHash: authGen1,
		ObservedAt:         now,
	}

	key1 := snap1.CacheKeyPartition()
	if key1 == "" {
		t.Fatal("empty CacheKeyPartition")
	}

	// Same snapshot -> same key
	if snap1.CacheKeyPartition() != key1 {
		t.Fatal("partition key not deterministic")
	}

	// Scope change changes key
	snap2 := snap1
	snap2.ScopeHash = scopeHash2
	if snap2.CacheKeyPartition() == key1 {
		t.Fatal("partition key did not change when ScopeHash changed")
	}

	// Auth generation change (e.g. re-auth, session rotate) changes key
	snap3 := snap1
	snap3.AuthGenerationHash = authGen2
	if snap3.CacheKeyPartition() == key1 {
		t.Fatal("partition key did not change when AuthGenerationHash changed")
	}

	// Mode change changes key
	snap4 := snap1
	snap4.Mode = protocol.RuntimeAuthModeAPIKey
	if snap4.CacheKeyPartition() == key1 {
		t.Fatal("partition key did not change when Mode changed")
	}
}

func TestRuntimeAuthSnapshot_DiagnosticsAndFormatting(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	scopeHash := protocol.ComputeScopeHash(nil)
	authGenHash := protocol.ComputeAuthGenerationHash("default", "gen-1")

	snap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          scopeHash,
		AuthGenerationHash: authGenHash,
		ObservedAt:         now,
	}

	str := snap.String()
	goStr := snap.GoString()
	if !strings.Contains(str, "codex_process") || !strings.Contains(str, "chatgpt_subscription") {
		t.Fatalf("String() missing core fields: %s", str)
	}
	if str != goStr {
		t.Fatalf("String() and GoString() diverged: %q != %q", str, goStr)
	}

	// JSON Roundtrip
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var restored protocol.RuntimeAuthSnapshot
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if err := restored.Validate(); err != nil {
		t.Fatalf("restored snapshot failed validation: %v", err)
	}
	if restored.CacheKeyPartition() != snap.CacheKeyPartition() {
		t.Fatalf("restored CacheKeyPartition mismatch: %s != %s", restored.CacheKeyPartition(), snap.CacheKeyPartition())
	}
}
