package codexruntime_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/codexruntime"
	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

func TestCachePartitionManager_Lifecycle(t *testing.T) {
	t.Parallel()

	pm := codexruntime.NewCachePartitionManager()
	if pm.CurrentPartitionKey() != "" {
		t.Fatal("expected initially empty partition key")
	}

	now := time.Now().UTC()
	scope1 := protocol.ComputeScopeHash([]string{"src", "pkg"})
	scope2 := protocol.ComputeScopeHash([]string{"src", "pkg", "docs"})
	gen1 := protocol.ComputeAuthGenerationHash("default", "epoch1")
	gen2 := protocol.ComputeAuthGenerationHash("default", "epoch2")

	snap1 := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          scope1,
		AuthGenerationHash: gen1,
		ObservedAt:         now,
	}

	// 1. Initial partition
	invalidated, reason, err := pm.UpdateSnapshot(snap1)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if !invalidated {
		t.Fatal("expected initial update to set partition")
	}
	if reason != codexruntime.ReasonInitialPartition {
		t.Fatalf("expected reason %s, got %s", codexruntime.ReasonInitialPartition, reason)
	}
	key1 := pm.CurrentPartitionKey()
	if key1 == "" {
		t.Fatal("partition key should not be empty")
	}

	// 2. Same snapshot -> no invalidation
	invalidated, _, err = pm.UpdateSnapshot(snap1)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if invalidated {
		t.Fatal("identical snapshot should not invalidate cache")
	}
	if pm.CurrentPartitionKey() != key1 {
		t.Fatal("partition key changed unexpectedly")
	}

	// 3. Scope change -> invalidation with ReasonScopeChanged
	snapScopeChanged := snap1
	snapScopeChanged.ScopeHash = scope2
	invalidated, reason, err = pm.UpdateSnapshot(snapScopeChanged)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if !invalidated {
		t.Fatal("scope change should invalidate cache")
	}
	if reason != codexruntime.ReasonScopeChanged {
		t.Fatalf("expected reason %s, got %s", codexruntime.ReasonScopeChanged, reason)
	}
	key2 := pm.CurrentPartitionKey()
	if key2 == key1 {
		t.Fatal("partition key should change on scope hash change")
	}

	// 4. Auth generation change (logout/relogin/token renewal) -> ReasonAuthGenChanged
	snapGenChanged := snapScopeChanged
	snapGenChanged.AuthGenerationHash = gen2
	invalidated, reason, err = pm.UpdateSnapshot(snapGenChanged)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if !invalidated {
		t.Fatal("auth generation change should invalidate cache")
	}
	if reason != codexruntime.ReasonAuthGenChanged {
		t.Fatalf("expected reason %s, got %s", codexruntime.ReasonAuthGenChanged, reason)
	}
	key3 := pm.CurrentPartitionKey()
	if key3 == key2 {
		t.Fatal("partition key should change on auth generation change")
	}

	// 5. Auth mode change -> ReasonModeChanged
	snapModeChanged := snapGenChanged
	snapModeChanged.Mode = protocol.RuntimeAuthModeAPIKey
	invalidated, reason, err = pm.UpdateSnapshot(snapModeChanged)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if !invalidated {
		t.Fatal("mode change should invalidate cache")
	}
	if reason != codexruntime.ReasonModeChanged {
		t.Fatalf("expected reason %s, got %s", codexruntime.ReasonModeChanged, reason)
	}

	// 6. Transition to unauthenticated -> Clears partition key
	snapUnauth := snapModeChanged
	snapUnauth.State = protocol.RuntimeAuthStateUnauthenticated
	invalidated, reason, err = pm.UpdateSnapshot(snapUnauth)
	if err != nil {
		t.Fatalf("UpdateSnapshot failed: %v", err)
	}
	if !invalidated {
		t.Fatal("unauthenticated state should invalidate cache")
	}
	if reason != codexruntime.ReasonUnauthenticatedState {
		t.Fatalf("expected reason %s, got %s", codexruntime.ReasonUnauthenticatedState, reason)
	}
	if pm.CurrentPartitionKey() != "" {
		t.Fatal("expected empty partition key after unauthenticated update")
	}

	// 7. Manual invalidation
	pm.Invalidate(codexruntime.ReasonManualInvalidation)
	if pm.LastInvalidationReason() != codexruntime.ReasonManualInvalidation {
		t.Fatalf("expected last reason %s, got %s", codexruntime.ReasonManualInvalidation, pm.LastInvalidationReason())
	}
}

func TestRuntimeService_EnsureReady_Success(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.Executable = "codex"
	cfg.CodexRuntime.CredentialOwner = "codex_process"
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	snap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash([]string{"src"}),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen-1"),
		ObservedAt:         time.Now().UTC(),
	}

	fakeProber := codexruntime.NewFakeStatusProber(snap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/usr/local/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	ctx := context.Background()
	resSnap, err := svc.EnsureReady(ctx, []string{"src"})
	if err != nil {
		t.Fatalf("EnsureReady failed: %v", err)
	}

	if !resSnap.IsAuthenticated() {
		t.Fatal("expected authenticated snapshot")
	}
	if svc.CurrentPartitionKey() == "" {
		t.Fatal("expected active partition key")
	}
}

func TestRuntimeService_AuthModeMismatch_FailsClosed(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	// Mock returning api_key when subscription is required
	snap := protocol.RuntimeAuthSnapshot{
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

	fakeProber := codexruntime.NewFakeStatusProber(snap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/usr/local/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	ctx := context.Background()
	_, err := svc.EnsureReady(ctx, nil)
	if err == nil {
		t.Fatal("expected EnsureReady to fail when auth mode mismatches required_auth")
	}
}

func TestRuntimeService_FailureSemantics(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	baseSnap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateUnauthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash(nil),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen-1"),
		ObservedAt:         time.Now().UTC(),
	}

	// 1. Unauthenticated -> ErrOperatorRequired (no silent API fallback)
	fakeProber := codexruntime.NewFakeStatusProber(baseSnap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/usr/local/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	ctx := context.Background()
	_, err := svc.EnsureReady(ctx, nil)
	if err != codexruntime.ErrOperatorRequired {
		t.Fatalf("expected ErrOperatorRequired, got: %v", err)
	}

	// 2. Expired -> ErrSessionExpired (turn progression stops)
	expiredSnap := baseSnap
	expiredSnap.State = protocol.RuntimeAuthStateExpired
	fakeProber.SetSnapshot(expiredSnap)
	_, err = svc.EnsureReady(ctx, nil)
	if err != codexruntime.ErrSessionExpired {
		t.Fatalf("expected ErrSessionExpired, got: %v", err)
	}

	// 3. Unavailable -> ErrRuntimeUnavailable
	unavailableSnap := baseSnap
	unavailableSnap.State = protocol.RuntimeAuthStateUnavailable
	fakeProber.SetSnapshot(unavailableSnap)
	_, err = svc.EnsureReady(ctx, nil)
	if err != codexruntime.ErrRuntimeUnavailable {
		t.Fatalf("expected ErrRuntimeUnavailable, got: %v", err)
	}

	// 4. Disabled config -> ErrRuntimeDisabled
	disabledCfg := cfg.CodexRuntime
	disabledCfg.Enabled = false
	svcDisabled := codexruntime.NewRuntimeService(disabledCfg, fakeProber)
	_, err = svcDisabled.EnsureReady(ctx, nil)
	if err != codexruntime.ErrRuntimeDisabled {
		t.Fatalf("expected ErrRuntimeDisabled, got: %v", err)
	}
}

func TestRuntimeService_ConcurrentAccess_RaceTest(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.CodexRuntime.Enabled = true
	cfg.CodexRuntime.RequiredAuth = "chatgpt_subscription"

	snap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		CodexVersion:       "codex 0.4.0",
		ScopeHash:          protocol.ComputeScopeHash([]string{"src"}),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen-1"),
		ObservedAt:         time.Now().UTC(),
	}

	fakeProber := codexruntime.NewFakeStatusProber(snap)
	svc := codexruntime.NewRuntimeService(cfg.CodexRuntime, fakeProber)
	svc.SetBinary(&codexruntime.ResolvedBinary{
		Path:    "/usr/local/bin/codex",
		SHA256:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Version: "codex 0.4.0",
	})

	var wg sync.WaitGroup
	workers := 20
	iterations := 50

	ctx := context.Background()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				if j%5 == 0 {
					svc.InvalidateSession(codexruntime.ReasonManualInvalidation)
				}
				_, _ = svc.EnsureReady(ctx, []string{"src"})
				_ = svc.CurrentPartitionKey()
			}
		}(i)
	}

	wg.Wait()
}

func TestResolveBinary_Validation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Shell metacharacters
	for _, bad := range []string{"codex; ls", "codex | cat", "codex && bash", "codex`whoami`", "codex > out", "codex\nls"} {
		badCfg := config.CodexRuntimeConfig{Executable: bad}
		_, err := codexruntime.ResolveBinary(ctx, badCfg)
		if err == nil {
			t.Fatalf("expected error for shell metacharacters in %q", bad)
		}
	}

	// Non-existent binary
	nonExistentCfg := config.CodexRuntimeConfig{Executable: "non_existent_codex_binary_xyz_123_random"}
	_, err := codexruntime.ResolveBinary(ctx, nonExistentCfg)
	if err == nil {
		t.Fatal("expected error for non-existent executable")
	}
}

func TestResolveBinary_MockExecutable(t *testing.T) {
	t.Parallel()

	// Create a mock executable script in a temporary directory
	tempDir := t.TempDir()
	var mockScript string
	if os.PathSeparator == '\\' {
		// Windows batch
		mockScript = filepath.Join(tempDir, "mock_codex.cmd")
		err := os.WriteFile(mockScript, []byte("@echo off\r\necho codex 0.4.2\r\n"), 0755)
		if err != nil {
			t.Fatalf("failed to write mock script: %v", err)
		}
	} else {
		// Unix shell script
		mockScript = filepath.Join(tempDir, "mock_codex.sh")
		err := os.WriteFile(mockScript, []byte("#!/bin/sh\necho \"codex 0.4.2\"\n"), 0755)
		if err != nil {
			t.Fatalf("failed to write mock script: %v", err)
		}
	}

	ctx := context.Background()
	cfg := config.CodexRuntimeConfig{
		Executable: mockScript,
	}

	res, err := codexruntime.ResolveBinary(ctx, cfg)
	if err != nil {
		t.Fatalf("ResolveBinary on mock script failed: %v", err)
	}

	if res.Version != "codex 0.4.2" {
		t.Fatalf("expected version %q, got %q", "codex 0.4.2", res.Version)
	}
	if len(res.SHA256) != 64 {
		t.Fatalf("expected 64-char sha256, got %q", res.SHA256)
	}

	// SHA256 integrity check matching
	cfgMatching := cfg
	cfgMatching.BinarySHA256 = res.SHA256
	resMatching, err := codexruntime.ResolveBinary(ctx, cfgMatching)
	if err != nil {
		t.Fatalf("ResolveBinary with matching sha256 failed: %v", err)
	}
	if resMatching.SHA256 != res.SHA256 {
		t.Fatal("SHA256 mismatch")
	}

	// SHA256 integrity check mismatch
	cfgMismatch := cfg
	cfgMismatch.BinarySHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	_, err = codexruntime.ResolveBinary(ctx, cfgMismatch)
	if err == nil {
		t.Fatal("expected error on sha256 mismatch")
	}
}
