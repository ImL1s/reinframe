package codexruntime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/codexruntime"
	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// TestSecurity_NeverOpensCredentialStore verifies that Reinframe never reads ~/.codex/auth.json.
func TestSecurity_NeverOpensCredentialStore(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	fakeCodexDir := filepath.Join(tempDir, ".codex")
	if err := os.MkdirAll(fakeCodexDir, 0700); err != nil {
		t.Fatalf("failed to create fake .codex dir: %v", err)
	}

	fakeAuthJSON := filepath.Join(fakeCodexDir, "auth.json")
	secretToken := "sk-live-super-secret-token-1234567890abcdef"
	secretPayload := fmt.Sprintf(`{"oauth_token":"%s","refresh_token":"rt-9999"}`, secretToken)
	if err := os.WriteFile(fakeAuthJSON, []byte(secretPayload), 0600); err != nil {
		t.Fatalf("failed to write fake auth.json: %v", err)
	}

	// 1. Check Sentinel assertion rejects accessing this path
	if err := codexruntime.AssertNoProhibitedPathAccess(fakeAuthJSON); err == nil {
		t.Fatal("AssertNoProhibitedPathAccess must reject access to .codex/auth.json")
	}

	// 2. Resolve binary and probe auth using CLI prober pointing to a safe mock
	var mockExec string
	if os.PathSeparator == '\\' {
		mockExec = filepath.Join(tempDir, "mock_codex.cmd")
		script := "@echo off\r\necho Logged in using ChatGPT subscription\r\n"
		if err := os.WriteFile(mockExec, []byte(script), 0755); err != nil {
			t.Fatalf("failed to write mock: %v", err)
		}
	} else {
		mockExec = filepath.Join(tempDir, "mock_codex.sh")
		script := "#!/bin/sh\necho \"Logged in using ChatGPT subscription\"\n"
		if err := os.WriteFile(mockExec, []byte(script), 0755); err != nil {
			t.Fatalf("failed to write mock: %v", err)
		}
	}

	cfg := config.Default().CodexRuntime
	cfg.Enabled = true
	cfg.Executable = mockExec
	cfg.CredentialOwner = "codex_process"
	cfg.RequiredAuth = "chatgpt_subscription"

	bin := &codexruntime.ResolvedBinary{
		Path:    mockExec,
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	}

	prober := codexruntime.NewCLIStatusProber()
	snap, err := prober.ProbeAuthStatus(context.Background(), cfg, bin, []string{"repo"})
	if err != nil {
		t.Fatalf("ProbeAuthStatus failed: %v", err)
	}

	// 3. Verify snapshot contains NO trace of fakeAuthJSON or secretToken
	snapJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	rawSnapStr := string(snapJSON)

	if strings.Contains(rawSnapStr, secretToken) {
		t.Fatalf("SECURITY VIOLATION: secret token appeared in snapshot: %s", rawSnapStr)
	}
	if strings.Contains(rawSnapStr, "auth.json") {
		t.Fatalf("SECURITY VIOLATION: auth.json appeared in snapshot: %s", rawSnapStr)
	}

	if err := codexruntime.AssertNoSecretLeakage(rawSnapStr); err != nil {
		t.Fatalf("AssertNoSecretLeakage failed on snapshot JSON: %v", err)
	}
	if err := codexruntime.AssertNoSecretLeakage(snap.String()); err != nil {
		t.Fatalf("AssertNoSecretLeakage failed on snapshot String(): %v", err)
	}
}

// TestSecurity_SubscriptionCannotPopulateAPIKeyField verifies separation between subscription & API key.
func TestSecurity_SubscriptionCannotPopulateAPIKeyField(t *testing.T) {
	t.Parallel()

	snapSub := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash(nil),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen-1"),
		ObservedAt:         time.Now().UTC(),
	}

	// 1. Subscription snapshot cannot satisfy API key requirement
	if err := snapSub.MatchesRequirement(string(protocol.RuntimeAuthModeAPIKey)); err == nil {
		t.Fatal("subscription snapshot must not satisfy api_key requirement")
	}

	// 2. API key snapshot cannot satisfy subscription requirement
	snapAPI := snapSub
	snapAPI.Mode = protocol.RuntimeAuthModeAPIKey
	if err := snapAPI.MatchesRequirement(string(protocol.RuntimeAuthModeChatGPTSubscription)); err == nil {
		t.Fatal("api_key snapshot must not satisfy chatgpt_subscription requirement")
	}
}

// TestSecurity_AuthModeMismatchBlocksRuntimeStartup verifies startup blocked before model selection.
func TestSecurity_AuthModeMismatchBlocksRuntimeStartup(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	// Mock reporting api_key instead of required chatgpt_subscription
	mismatchSnap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeAPIKey,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash(nil),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen-1"),
		ObservedAt:         time.Now().UTC(),
	}

	fakeProber := codexruntime.NewFakeStatusProber(mismatchSnap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	ctx := context.Background()
	_, err := svc.EnsureReady(ctx, nil)
	if err == nil {
		t.Fatal("EnsureReady must block execution when required_auth mismatches actual mode")
	}
	if !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("expected mismatch error, got: %v", err)
	}
}

// TestSecurity_LogoutExpiryInvalidatesCacheGeneration verifies cache rotation on auth change.
func TestSecurity_LogoutExpiryInvalidatesCacheGeneration(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	initialSnap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash([]string{"src"}),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "login-epoch-1"),
		ObservedAt:         time.Now().UTC(),
	}

	fakeProber := codexruntime.NewFakeStatusProber(initialSnap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	ctx := context.Background()
	_, err := svc.EnsureReady(ctx, []string{"src"})
	if err != nil {
		t.Fatalf("initial EnsureReady failed: %v", err)
	}

	key1 := svc.CurrentPartitionKey()
	if key1 == "" {
		t.Fatal("initial partition key must not be empty")
	}

	// Simulate user logging out / token expiring
	expiredSnap := initialSnap
	expiredSnap.State = protocol.RuntimeAuthStateExpired
	expiredSnap.AuthGenerationHash = protocol.ComputeAuthGenerationHash("default", "expired-epoch-2")
	fakeProber.SetSnapshot(expiredSnap)

	_, err = svc.EnsureReady(ctx, []string{"src"})
	if err != codexruntime.ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}

	// Cache partition must be destroyed / cleared
	if svc.CurrentPartitionKey() != "" {
		t.Fatalf("expected empty partition key after expiry, got %q", svc.CurrentPartitionKey())
	}
}

// TestSecurity_ProjectConfigCannotOverrideExecutableOrOwner verifies untrusted override rejection.
func TestSecurity_ProjectConfigCannotOverrideExecutableOrOwner(t *testing.T) {
	t.Parallel()

	base := config.Default()
	base.CodexRuntime.Executable = "/usr/bin/codex"
	base.CodexRuntime.CredentialOwner = "codex_process"

	// 1. Untrusted project attempts to redirect executable to malicious binary
	untrustedExec := config.Default()
	untrustedExec.CodexRuntime.Executable = "/tmp/fake_codex_backdoor"
	if err := config.ValidateUntrustedProjectOverride(base, untrustedExec); err == nil {
		t.Fatal("expected error when project config tries to override codex_runtime.executable")
	}

	// 2. Untrusted project attempts to hijack credential ownership
	untrustedOwner := config.Default()
	untrustedOwner.CodexRuntime.CredentialOwner = "reinframe_env"
	if err := config.ValidateUntrustedProjectOverride(base, untrustedOwner); err == nil {
		t.Fatal("expected error when project config tries to override codex_runtime.credential_owner")
	}
}
