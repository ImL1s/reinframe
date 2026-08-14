package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/classifier"
	"github.com/ImL1s/reinframe/pkg/codexruntime"
	"github.com/ImL1s/reinframe/pkg/config"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// ============================================================================
// Helper Mock Server for Codex App Server JSON-RPC Stdio Testing
// ============================================================================

type fakeSubscriptionServer struct {
	mu           sync.Mutex
	clientWrites io.Reader
	clientReads  io.Writer
	modelsJSON   json.RawMessage
	turnModel    string
	failRate429  bool
	crashOnTurn  bool
	hangOnTurn   bool
	onTurnStart  func(threadID, turnID, model string)
}

func startFakeSubscriptionServer(
	t *testing.T,
	clientWrites io.Reader,
	clientReads io.Writer,
	initialModels json.RawMessage,
) *fakeSubscriptionServer {
	t.Helper()
	s := &fakeSubscriptionServer{
		clientWrites: clientWrites,
		clientReads:  clientReads,
		modelsJSON:   initialModels,
		turnModel:    "gpt-5.3-codex-spark",
	}

	go s.loop(t)
	return s
}

func (s *fakeSubscriptionServer) SetModels(models json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelsJSON = models
}

func (s *fakeSubscriptionServer) SetTurnModel(m string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnModel = m
}

func (s *fakeSubscriptionServer) SetRateLimitFailure(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failRate429 = enabled
}

func (s *fakeSubscriptionServer) SetCrashOnTurn(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.crashOnTurn = enabled
}

func (s *fakeSubscriptionServer) SetHangOnTurn(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hangOnTurn = enabled
}

func (s *fakeSubscriptionServer) loop(t *testing.T) {
	sc := bufio.NewScanner(s.clientWrites)
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, adapter.MaxCodexAppServerMessageBytes+1)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		method, _ := req["method"].(string)
		idFloat, hasID := req["id"].(float64)
		idInt := int64(idFloat)
		params, _ := req["params"].(map[string]any)

		if !hasID || method == "" {
			// Notification from client (e.g. "initialized") or response to server request
			continue
		}

		s.mu.Lock()
		curModels := s.modelsJSON
		curTurnModel := s.turnModel
		rateLimit := s.failRate429
		crash := s.crashOnTurn
		hang := s.hangOnTurn
		onTurn := s.onTurnStart
		s.mu.Unlock()

		switch method {
		case "initialize":
			s.writeRPC(idInt, map[string]any{
				"serverInfo": map[string]any{
					"name":    "codex-app-server-subscription",
					"version": "1.2.0",
				},
				"protocolVersion": 1,
				"capabilities": map[string]any{
					"approval":     true,
					"streaming":    true,
					"modelCatalog": true,
				},
			})

		case "model/list":
			var parsed any
			if len(curModels) > 0 {
				_ = json.Unmarshal(curModels, &parsed)
			} else {
				parsed = map[string]any{"models": []any{}}
			}
			s.writeRPC(idInt, parsed)

		case "thread/start":
			model, _ := params["model"].(string)
			if curTurnModel != "" {
				model = curTurnModel
			}
			if model == "" {
				model = "gpt-5.3-codex-spark"
			}
			s.writeRPC(idInt, map[string]any{
				"threadId":  fmt.Sprintf("th_%d", idInt),
				"model":     model,
				"status":    "active",
				"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
			})

		case "turn/start":
			if hang {
				// Simulate unhandled hang / timeout
				continue
			}
			if crash {
				// Close writer or send connection drop
				if closer, ok := s.clientReads.(io.Closer); ok {
					_ = closer.Close()
				}
				return
			}
			if rateLimit {
				s.writeRPCErr(idInt, map[string]any{
					"code":    -32000,
					"message": "Rate limit exceeded (429): ChatGPT Pro subscription quota exhausted for model",
				})
				continue
			}

			model, _ := params["model"].(string)
			if curTurnModel != "" {
				model = curTurnModel
			}
			turnID, _ := params["turnId"].(string)
			if turnID == "" {
				turnID = fmt.Sprintf("turn_%d", idInt)
			}
			threadID, _ := params["threadId"].(string)

			if onTurn != nil {
				onTurn(threadID, turnID, model)
			}

			s.writeRPC(idInt, map[string]any{
				"turnId":    turnID,
				"threadId":  threadID,
				"status":    "in_progress",
				"model":     model,
				"startedAt": time.Now().UTC().Format(time.RFC3339Nano),
			})

		case "turn/interrupt":
			s.writeRPC(idInt, map[string]any{"ok": true})

		case "approval/respond":
			s.writeRPC(idInt, map[string]any{"ok": true})

		default:
			s.writeRPCErr(idInt, map[string]any{"code": -32601, "message": "Method not found"})
		}
	}
}

func (s *fakeSubscriptionServer) writeRPC(id int64, result any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = s.clientReads.Write(append(raw, '\n'))
}

func (s *fakeSubscriptionServer) writeRPCErr(id int64, errObj map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "error": errObj})
	_, _ = s.clientReads.Write(append(raw, '\n'))
}

func (s *fakeSubscriptionServer) sendServerApprovalRequest(reqID int64, threadID, turnID, command string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      reqID,
		"method":  "approval/request",
		"params": map[string]any{
			"requestId": fmt.Sprintf("req_%d", reqID),
			"threadId":  threadID,
			"turnId":    turnID,
			"kind":      "command",
			"command":   command,
			"createdAt": time.Now().UTC().Format(time.RFC3339Nano),
		},
	})
	_, _ = s.clientReads.Write(append(raw, '\n'))
}

func setupSubscriptionHarness(t *testing.T, initialModels json.RawMessage) (*adapter.CodexAppServerClient, *fakeSubscriptionServer, func()) {
	t.Helper()
	c2sR, c2sW := io.Pipe()
	s2cR, s2cW := io.Pipe()

	server := startFakeSubscriptionServer(t, c2sR, s2cW, initialModels)

	cfg := adapter.CodexAppServerConfig{
		StartupTimeout:      10 * time.Second,
		RequestTimeout:      10 * time.Second,
		MaxMessageBytes:     adapter.MaxCodexAppServerMessageBytes,
		EventsQueueDepth:    512,
		ApprovalsQueueDepth: 128,
	}

	client := adapter.NewCodexAppServerClientForTest(c2sW, s2cR, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("failed to start app server client test harness: %v", err)
	}

	cleanup := func() {
		_ = client.Close(context.Background())
		_ = c2sR.Close()
		_ = c2sW.Close()
		_ = s2cR.Close()
		_ = s2cW.Close()
	}

	return client, server, cleanup
}

// ============================================================================
// Suite 1: Auth State Lifecycle & Cache Partition Invalidation
// (Authenticated -> Expired -> Re-authenticated)
// ============================================================================

func TestCodexSubscription_AuthStateLifecycle(t *testing.T) {
	t.Parallel()

	modelsPayload := json.RawMessage(`{
		"models": [
			{
				"id": "gpt-5.3-codex",
				"displayName": "GPT-5.3 Codex",
				"supportState": "selectable",
				"capabilities": 3,
				"contextWindow": 200000
			},
			{
				"id": "gpt-5.3-codex-spark",
				"displayName": "GPT-5.3 Codex Spark (Research Preview)",
				"supportState": "selectable",
				"capabilities": 7,
				"contextWindow": 128000,
				"defaultReasoningEffort": "high"
			}
		]
	}`)

	client, _, cleanup := setupSubscriptionHarness(t, modelsPayload)
	defer cleanup()

	pm := codexruntime.NewCachePartitionManager()
	catService := adapter.NewModelCatalogService(client, adapter.ModelCatalogConfig{
		TTL:              10 * time.Minute,
		PartitionManager: pm,
	})

	scope := []string{"workspace:/work/reinframe", "account:dev@enterprise.com"}
	scopeHash := protocol.ComputeScopeHash(scope)
	authGen1 := protocol.ComputeAuthGenerationHash("default", "epoch_001_initial")

	cfg := config.CodexRuntimeConfig{
		Enabled:         true,
		RequiredAuth:    "chatgpt_subscription",
		CredentialOwner: "codex_process",
		RuntimeProfile:  "default",
	}

	// 1. Initial State: Authenticated
	t.Run("1_Authenticated_State_InitializesPartitionAndCachesCatalog", func(t *testing.T) {
		snapAuth1 := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
			State:              protocol.RuntimeAuthStateAuthenticated,
			RuntimeProfile:     "default",
			CodexVersion:       "codex 0.4.0",
			ScopeHash:          scopeHash,
			AuthGenerationHash: authGen1,
			ObservedAt:         time.Now().UTC(),
		}

		prober := codexruntime.NewFakeStatusProber(snapAuth1)
		rtService := codexruntime.NewRuntimeService(cfg, prober)
		rtService.SetBinary(&codexruntime.ResolvedBinary{Path: "codex", Version: "0.4.0", SHA256: "testbin"})

		snap, err := rtService.EnsureReady(context.Background(), scope)
		if err != nil {
			t.Fatalf("EnsureReady failed for authenticated snapshot: %v", err)
		}
		if !snap.IsAuthenticated() {
			t.Fatalf("expected snapshot to be authenticated, got %s", snap.State)
		}

		// Perform catalog discovery
		catSnap, err := catService.Discover(context.Background(), snap)
		if err != nil {
			t.Fatalf("catalog Discover failed: %v", err)
		}
		if len(catSnap.Models) != 2 {
			t.Fatalf("expected 2 models discovered, got %d", len(catSnap.Models))
		}
		if !catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Errorf("expected gpt-5.3-codex-spark to be selectable")
		}

		initialKey := pm.CurrentPartitionKey()
		if initialKey == "" {
			t.Fatal("partition key should not be empty")
		}
		if pm.InvalidationCount() != 1 {
			t.Fatalf("expected invalidation count 1 on initial partition, got %d", pm.InvalidationCount())
		}
		if pm.LastInvalidationReason() != codexruntime.ReasonInitialPartition {
			t.Fatalf("expected ReasonInitialPartition, got %s", pm.LastInvalidationReason())
		}

		// Repeat Discover must return cached snapshot without invalidating
		catSnap2, err := catService.Discover(context.Background(), snap)
		if err != nil {
			t.Fatalf("second Discover failed: %v", err)
		}
		if catSnap2.CatalogHash != catSnap.CatalogHash {
			t.Errorf("cached catalog hash mismatch: %s vs %s", catSnap2.CatalogHash, catSnap.CatalogHash)
		}
		if pm.InvalidationCount() != 1 {
			t.Fatalf("expected invalidation count to remain 1, got %d", pm.InvalidationCount())
		}
	})

	// 2. Transition: Expired
	t.Run("2_Expired_State_FailsClosedAndInvalidatesCache", func(t *testing.T) {
		snapExpired := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
			State:              protocol.RuntimeAuthStateExpired,
			RuntimeProfile:     "default",
			CodexVersion:       "codex 0.4.0",
			ScopeHash:          scopeHash,
			AuthGenerationHash: authGen1,
			ObservedAt:         time.Now().UTC(),
		}

		prober := codexruntime.NewFakeStatusProber(snapExpired)
		rtService := codexruntime.NewRuntimeService(cfg, prober)
		rtService.SetBinary(&codexruntime.ResolvedBinary{Path: "codex", Version: "0.4.0", SHA256: "testbin"})

		// EnsureReady must fail closed with ErrSessionExpired
		_, err := rtService.EnsureReady(context.Background(), scope)
		if !errors.Is(err, codexruntime.ErrSessionExpired) {
			t.Fatalf("expected ErrSessionExpired on expired state, got: %v", err)
		}

		// Update partition manager with expired snapshot -> partition cleared
		invalidated, reason, err := pm.UpdateSnapshot(snapExpired)
		if err != nil {
			t.Fatalf("UpdateSnapshot failed on expired snapshot: %v", err)
		}
		if !invalidated {
			t.Fatal("expected cache partition to be invalidated on expired state")
		}
		if reason != codexruntime.ReasonUnauthenticatedState {
			t.Fatalf("expected ReasonUnauthenticatedState, got %s", reason)
		}
		if pm.CurrentPartitionKey() != "" {
			t.Fatalf("expected empty partition key after expiration, got %q", pm.CurrentPartitionKey())
		}

		// Catalog service must reject discovery with unauthenticated partition error
		_, err = catService.Discover(context.Background(), snapExpired)
		if !errors.Is(err, codexruntime.ErrUnauthenticatedPartition) {
			t.Fatalf("expected ErrUnauthenticatedPartition on expired snapshot, got %v", err)
		}

		// Cached snapshot must now be invalid
		if _, ok := catService.CurrentSnapshot(); ok {
			t.Fatal("expected CurrentSnapshot to report invalid/false after cache partition invalidation")
		}
	})

	// 3. Transition: Re-authenticated (New Generation Hash)
	t.Run("3_Reauthenticated_CreatesNewPartitionAndRefetchesFreshCatalog", func(t *testing.T) {
		authGen2 := protocol.ComputeAuthGenerationHash("default", "epoch_002_reauthenticated")
		snapReauth := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
			State:              protocol.RuntimeAuthStateAuthenticated,
			RuntimeProfile:     "default",
			CodexVersion:       "codex 0.4.0",
			ScopeHash:          scopeHash,
			AuthGenerationHash: authGen2,
			ObservedAt:         time.Now().UTC(),
		}

		prober := codexruntime.NewFakeStatusProber(snapReauth)
		rtService := codexruntime.NewRuntimeService(cfg, prober)
		rtService.SetBinary(&codexruntime.ResolvedBinary{Path: "codex", Version: "0.4.0", SHA256: "testbin"})

		snap, err := rtService.EnsureReady(context.Background(), scope)
		if err != nil {
			t.Fatalf("EnsureReady failed on re-authenticated snapshot: %v", err)
		}
		if !snap.IsAuthenticated() {
			t.Fatalf("expected re-authenticated state, got %s", snap.State)
		}

		// Discover now establishes fresh partition key
		catSnap, err := catService.Discover(context.Background(), snap)
		if err != nil {
			t.Fatalf("Discover failed after re-auth: %v", err)
		}
		if catSnap.AuthGenerationHash != authGen2 {
			t.Fatalf("expected snapshot auth generation %q, got %q", authGen2, catSnap.AuthGenerationHash)
		}
		if pm.CurrentPartitionKey() == "" {
			t.Fatal("partition key should be re-established")
		}
		if !catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Fatal("expected gpt-5.3-codex-spark to be selectable after re-auth")
		}
	})

	// 4. Security & Sentinel Invariants: Zero Token Extraction & Secret Leakage Prevention
	t.Run("4_SecuritySentinel_ZeroTokenExtractionAndSecretLeakage", func(t *testing.T) {
		// Test path access guards
		for _, prohibited := range codexruntime.ProhibitedCredentialPaths {
			err := codexruntime.AssertNoProhibitedPathAccess(prohibited)
			if err == nil {
				t.Errorf("expected security violation when accessing prohibited path %q", prohibited)
			}
		}

		// Test secret leakage guards in logs/text
		for _, secretPattern := range codexruntime.ProhibitedSecretPatterns {
			sampleText := fmt.Sprintf("debug output with %s123456789 secret value", secretPattern)
			err := codexruntime.AssertNoSecretLeakage(sampleText)
			if err == nil {
				t.Errorf("expected secret leak detection for pattern %q", secretPattern)
			}
		}

		// Ensure clean snapshot validates without error
		cleanSnap := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
			State:              protocol.RuntimeAuthStateAuthenticated,
			RuntimeProfile:     "default",
			ScopeHash:          scopeHash,
			AuthGenerationHash: authGen1,
			ObservedAt:         time.Now().UTC(),
			Metadata: map[string]string{
				"tier": "chatgpt_pro",
			},
		}
		if err := cleanSnap.Validate(); err != nil {
			t.Fatalf("expected clean snapshot to validate: %v", err)
		}

		// Ensure contaminated snapshot with secret token is rejected
		dirtySnap := cleanSnap
		dirtySnap.Metadata = map[string]string{
			"token": "sk-proj-secret123456789",
		}
		if err := dirtySnap.Validate(); !errors.Is(err, protocol.ErrProhibitedSecretDetected) {
			t.Fatalf("expected ErrProhibitedSecretDetected on contaminated metadata, got: %v", err)
		}
	})
}

// ============================================================================
// Suite 2: Workspace / Scope Churn
// (Switching scope partitions cache generation and blocks stale catalog reuse)
// ============================================================================

func TestCodexSubscription_WorkspaceScopeChurn(t *testing.T) {
	t.Parallel()

	// Account 1 (Alice Pro): has Spark preview + standard models
	modelsAlicePro := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
			{"id": "gpt-5.3-codex-spark", "displayName": "GPT-5.3 Codex Spark (Pro Preview)", "supportState": "selectable", "capabilities": 7, "contextWindow": 128000},
			{"id": "o3-mini", "displayName": "o3-mini", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000}
		]
	}`)

	// Account 2 (Bob Standard): standard tier only (no Spark)
	modelsBobStandard := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
			{"id": "gpt-5.1", "displayName": "GPT-5.1", "supportState": "selectable", "capabilities": 1, "contextWindow": 128000}
		]
	}`)

	client, server, cleanup := setupSubscriptionHarness(t, modelsAlicePro)
	defer cleanup()

	pm := codexruntime.NewCachePartitionManager()
	catService := adapter.NewModelCatalogService(client, adapter.ModelCatalogConfig{
		TTL:              10 * time.Minute,
		PartitionManager: pm,
	})

	scopeAlice := []string{"workspace:/org/repo-alpha", "account:alice@pro.company.com"}
	scopeHashAlice := protocol.ComputeScopeHash(scopeAlice)

	scopeBob := []string{"workspace:/org/repo-beta", "account:bob@standard.company.com"}
	scopeHashBob := protocol.ComputeScopeHash(scopeBob)

	if scopeHashAlice == scopeHashBob {
		t.Fatal("scope hashes for Alice and Bob must differ")
	}

	authGen := protocol.ComputeAuthGenerationHash("default", "epoch_stable")

	snapAlice := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		ScopeHash:          scopeHashAlice,
		AuthGenerationHash: authGen,
		ObservedAt:         time.Now().UTC(),
	}

	snapBob := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		ScopeHash:          scopeHashBob,
		AuthGenerationHash: authGen,
		ObservedAt:         time.Now().UTC(),
	}

	// 1. Discover in Scope Alice
	t.Run("ScopeAlice_DiscoversProCatalog", func(t *testing.T) {
		catSnap, err := catService.Discover(context.Background(), snapAlice)
		if err != nil {
			t.Fatalf("Discover failed for Alice: %v", err)
		}
		if len(catSnap.Models) != 3 {
			t.Fatalf("expected 3 models for Alice, got %d", len(catSnap.Models))
		}
		if !catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Fatal("expected gpt-5.3-codex-spark selectable for Alice")
		}
		if !catService.IsSelectable("o3-mini") {
			t.Fatal("expected o3-mini selectable for Alice")
		}
	})

	// 2. Switch to Scope Bob: Stale Alice catalog MUST NOT be served
	t.Run("ScopeBob_BlocksStaleAliceCatalogAndPartitionsCache", func(t *testing.T) {
		// Server switches to Bob's available models
		server.SetModels(modelsBobStandard)

		// Discover in Bob's scope
		catSnap, err := catService.Discover(context.Background(), snapBob)
		if err != nil {
			t.Fatalf("Discover failed for Bob: %v", err)
		}
		if len(catSnap.Models) != 2 {
			t.Fatalf("expected 2 models for Bob, got %d", len(catSnap.Models))
		}

		// Partition key must now belong to Bob
		expectedBobKey := snapBob.CacheKeyPartition()
		if pm.CurrentPartitionKey() != expectedBobKey {
			t.Fatalf("partition key mismatch for Bob: got %s, want %s", pm.CurrentPartitionKey(), expectedBobKey)
		}
		if pm.LastInvalidationReason() != codexruntime.ReasonScopeChanged {
			t.Fatalf("expected ReasonScopeChanged, got %s", pm.LastInvalidationReason())
		}

		// Spark and o3-mini MUST NOT be selectable in Bob's scope
		if catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Fatal("CRITICAL INVARIANT VIOLATION: stale Spark model selectable in non-entitled Bob scope")
		}
		if catService.IsSelectable("o3-mini") {
			t.Fatal("CRITICAL INVARIANT VIOLATION: stale o3-mini model selectable in Bob scope")
		}
		if _, ok := catService.GetModel("gpt-5.3-codex-spark"); ok {
			t.Fatal("GetModel for gpt-5.3-codex-spark should return false in Bob scope")
		}

		// Bob's valid models are selectable
		if !catService.IsSelectable("gpt-5.3-codex") {
			t.Fatal("expected gpt-5.3-codex selectable for Bob")
		}
		if !catService.IsSelectable("gpt-5.1") {
			t.Fatal("expected gpt-5.1 selectable for Bob")
		}
	})

	// 3. Switch back to Scope Alice: triggers partition update and refetch
	t.Run("ScopeSwitchBack_RevalidatesPartition", func(t *testing.T) {
		server.SetModels(modelsAlicePro)

		catSnap, err := catService.Discover(context.Background(), snapAlice)
		if err != nil {
			t.Fatalf("Discover failed for Alice switch-back: %v", err)
		}
		if len(catSnap.Models) != 3 {
			t.Fatalf("expected 3 models for Alice, got %d", len(catSnap.Models))
		}
		if !catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Fatal("expected gpt-5.3-codex-spark selectable for Alice on switch-back")
		}
	})
}

// ============================================================================
// Suite 3: Dynamic Model Catalog Churn
// (Addition, removal, and renaming of models without hardcoded assumptions)
// ============================================================================

func TestCodexSubscription_DynamicCatalogChurn(t *testing.T) {
	t.Parallel()

	// Initial catalog T0
	catalogT0 := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
			{"id": "gpt-5.1", "displayName": "GPT-5.1", "supportState": "selectable", "capabilities": 1, "contextWindow": 128000}
		]
	}`)

	client, server, cleanup := setupSubscriptionHarness(t, catalogT0)
	defer cleanup()

	pm := codexruntime.NewCachePartitionManager()
	catService := adapter.NewModelCatalogService(client, adapter.ModelCatalogConfig{
		TTL:              10 * time.Minute,
		PartitionManager: pm,
	})

	authSnap := protocol.RuntimeAuthSnapshot{
		SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
		CredentialOwner:    protocol.CredentialOwnerCodexProcess,
		Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
		State:              protocol.RuntimeAuthStateAuthenticated,
		RuntimeProfile:     "default",
		ScopeHash:          protocol.ComputeScopeHash([]string{"workdir"}),
		AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen_churn_t0"),
		ObservedAt:         time.Now().UTC(),
	}

	// 1. Initial Discovery (T0)
	snapT0, err := catService.Discover(context.Background(), authSnap)
	if err != nil {
		t.Fatalf("Discover T0 failed: %v", err)
	}
	if len(snapT0.Models) != 2 {
		t.Fatalf("expected 2 models at T0, got %d", len(snapT0.Models))
	}
	if _, ok := catService.GetModel("gpt-5.3-codex-spark"); ok {
		t.Fatal("gpt-5.3-codex-spark should not exist at T0")
	}
	if _, ok := catService.GetModel("o3-mini"); ok {
		t.Fatal("o3-mini should not exist at T0")
	}

	// 2. Dynamic Addition (T1): OpenAI adds gpt-5.3-codex-spark and o3-mini
	t.Run("DynamicAddition_NewModelsDiscoveredWithoutHardcodedAssumptions", func(t *testing.T) {
		catalogT1 := json.RawMessage(`{
			"models": [
				{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
				{"id": "gpt-5.1", "displayName": "GPT-5.1", "supportState": "selectable", "capabilities": 1, "contextWindow": 128000},
				{
					"id": "gpt-5.3-codex-spark",
					"displayName": "GPT-5.3 Codex Spark (Research Preview)",
					"supportState": "selectable",
					"capabilities": 7,
					"contextWindow": 128000,
					"defaultReasoningEffort": "high"
				},
				{
					"id": "o3-mini",
					"displayName": "o3-mini (High Speed Reasoning)",
					"supportState": "selectable",
					"capabilities": 3,
					"contextWindow": 200000,
					"defaultReasoningEffort": "low"
				}
			]
		}`)
		server.SetModels(catalogT1)

		snapT1, err := catService.Refresh(context.Background(), authSnap)
		if err != nil {
			t.Fatalf("Refresh T1 failed: %v", err)
		}
		if len(snapT1.Models) != 4 {
			t.Fatalf("expected 4 models at T1, got %d", len(snapT1.Models))
		}
		if snapT1.CatalogHash == snapT0.CatalogHash {
			t.Fatal("catalog hash must update after model additions")
		}

		// Newly added models are discoverable & selectable
		sparkDesc, ok := catService.GetModel("gpt-5.3-codex-spark")
		if !ok {
			t.Fatal("gpt-5.3-codex-spark must be discoverable at T1")
		}
		if sparkDesc.DefaultReasoningEffort != "high" || sparkDesc.ContextWindow != 128000 {
			t.Errorf("unexpected spark descriptor fields: %+v", sparkDesc)
		}
		if !catService.IsSelectable("gpt-5.3-codex-spark") {
			t.Fatal("gpt-5.3-codex-spark must be selectable at T1")
		}

		o3Desc, ok := catService.GetModel("o3-mini")
		if !ok {
			t.Fatal("o3-mini must be discoverable at T1")
		}
		if o3Desc.ContextWindow != 200000 || o3Desc.DefaultReasoningEffort != "low" {
			t.Errorf("unexpected o3-mini descriptor fields: %+v", o3Desc)
		}
	})

	// 3. Dynamic Deprecation/Removal (T2): OpenAI retires gpt-5.1
	t.Run("DynamicDeprecation_RemovedModelsBecomeUnselectable", func(t *testing.T) {
		catalogT2 := json.RawMessage(`{
			"models": [
				{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
				{"id": "gpt-5.3-codex-spark", "displayName": "GPT-5.3 Codex Spark", "supportState": "selectable", "capabilities": 7, "contextWindow": 128000},
				{"id": "o3-mini", "displayName": "o3-mini", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000}
			]
		}`)
		server.SetModels(catalogT2)

		snapT2, err := catService.Refresh(context.Background(), authSnap)
		if err != nil {
			t.Fatalf("Refresh T2 failed: %v", err)
		}
		if len(snapT2.Models) != 3 {
			t.Fatalf("expected 3 models at T2, got %d", len(snapT2.Models))
		}

		// Retired model gpt-5.1 is no longer found or selectable
		if _, ok := catService.GetModel("gpt-5.1"); ok {
			t.Fatal("gpt-5.1 should not be found after deprecation")
		}
		if catService.IsSelectable("gpt-5.1") {
			t.Fatal("gpt-5.1 must not be selectable after deprecation")
		}
	})

	// 4. Dynamic Renaming & Qualification Upgrade (T3)
	t.Run("DynamicRenamingAndQualificationStateEvolution", func(t *testing.T) {
		catalogT3 := json.RawMessage(`{
			"models": [
				{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
				{
					"id": "gpt-5.3-codex-spark",
					"displayName": "GPT-5.3 Codex Spark (Production GA)",
					"supportState": "live_qualified",
					"capabilities": 15,
					"contextWindow": 256000,
					"defaultReasoningEffort": "high"
				},
				{"id": "o3-mini", "displayName": "o3-mini", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000}
			]
		}`)
		server.SetModels(catalogT3)

		snapT3, err := catService.Refresh(context.Background(), authSnap)
		if err != nil {
			t.Fatalf("Refresh T3 failed: %v", err)
		}

		sparkDesc, ok := catService.GetModel("gpt-5.3-codex-spark")
		if !ok {
			t.Fatal("gpt-5.3-codex-spark must be found at T3")
		}
		if sparkDesc.DisplayName != "GPT-5.3 Codex Spark (Production GA)" {
			t.Errorf("display name did not update: %s", sparkDesc.DisplayName)
		}
		if sparkDesc.SupportState != adapter.ModelSupportStateLiveQualified {
			t.Errorf("expected live_qualified support state, got %s", sparkDesc.SupportState)
		}
		if !sparkDesc.IsLiveQualified() {
			t.Fatal("expected IsLiveQualified to return true")
		}
		if sparkDesc.ContextWindow != 256000 {
			t.Errorf("context window not updated: %d", sparkDesc.ContextWindow)
		}
		_ = snapT3
	})

	// 5. Future Model Extensibility (T4): Unannounced Model
	t.Run("FutureModelExtensibility_UnannouncedFutureModelsHandledCleanly", func(t *testing.T) {
		catalogFuture := json.RawMessage(`{
			"models": [
				{
					"id": "gpt-6-codex-autonomous-2027",
					"displayName": "GPT-6 Codex Autonomous",
					"supportState": "selectable",
					"capabilities": 31,
					"contextWindow": 1000000,
					"inputModalities": ["text", "image", "audio", "video"]
				}
			]
		}`)
		server.SetModels(catalogFuture)

		snapFuture, err := catService.Refresh(context.Background(), authSnap)
		if err != nil {
			t.Fatalf("Refresh future models failed: %v", err)
		}
		if len(snapFuture.Models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(snapFuture.Models))
		}

		desc, ok := catService.GetModel("gpt-6-codex-autonomous-2027")
		if !ok {
			t.Fatal("future model not found")
		}
		if desc.ContextWindow != 1000000 || len(desc.InputModalities) != 4 {
			t.Errorf("future model parsed incorrectly: %+v", desc)
		}
	})
}

// ============================================================================
// Suite 4: Fallback Invariants & Dual-Lane Boundary
// (allow_provider_model_fallback rejection vs explicit substitution)
// ============================================================================

func TestCodexSubscription_FallbackInvariants(t *testing.T) {
	t.Parallel()

	modelsPayload := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable"},
			{"id": "gpt-5.3-codex-spark", "displayName": "GPT-5.3 Codex Spark", "supportState": "selectable"}
		]
	}`)

	client, server, cleanup := setupSubscriptionHarness(t, modelsPayload)
	defer cleanup()

	// 1. Fallback Disabled (Default Fail-Closed Policy)
	t.Run("FallbackDisabled_RejectsSilentSubstitutionAndUnprovenIdentity", func(t *testing.T) {
		// 1a: Requested Spark, server silently downgraded to gpt-5.3-codex
		server.SetTurnModel("gpt-5.3-codex")

		st, err := adapter.VerifyModelIdentity("gpt-5.3-codex-spark", "gpt-5.3-codex", false)
		if err == nil {
			t.Fatal("expected error when model substitution occurs with allow_fallback: false")
		}
		if !errors.Is(err, adapter.ErrModelUnavailable) {
			var appErr *adapter.AppServerError
			if !errors.As(err, &appErr) || appErr.Code != adapter.ErrCodeModelUnavailable {
				t.Fatalf("expected ErrCodeModelUnavailable, got: %v", err)
			}
		}
		if st.SubstitutionState != adapter.ModelSubstitutionViolated {
			t.Fatalf("expected ModelSubstitutionViolated, got %s", st.SubstitutionState)
		}

		// AppServer client Turn invocation fails closed on substitution violation
		_, err = client.StartTurn(context.Background(), adapter.TurnStartRequest{
			ThreadID:                   "th_fallback_test_1",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: false,
			Prompt:                     "Implement feature",
		})
		if err == nil {
			t.Fatal("StartTurn must fail closed when server attempts silent substitution without fallback allowed")
		}

		// 1b: Reported model is empty string (Unproven model identity)
		stEmpty, errEmpty := adapter.VerifyModelIdentity("gpt-5.3-codex-spark", "", false)
		if errEmpty == nil {
			t.Fatal("expected error on empty reported model")
		}
		if stEmpty.SubstitutionState != adapter.ModelSubstitutionIdentityUnproven {
			t.Fatalf("expected ModelSubstitutionIdentityUnproven, got %s", stEmpty.SubstitutionState)
		}
	})

	// 2. Fallback Enabled (Explicit Same-Lane Substitution Opt-In)
	t.Run("FallbackEnabled_AllowsExplicitSameLaneSubstitution", func(t *testing.T) {
		server.SetTurnModel("gpt-5.3-codex")

		st, err := adapter.VerifyModelIdentity("gpt-5.3-codex-spark", "gpt-5.3-codex", true)
		if err != nil {
			t.Fatalf("VerifyModelIdentity should succeed when fallback is allowed: %v", err)
		}
		if st.SubstitutionState != adapter.ModelSubstitutionFallbackAllowed {
			t.Fatalf("expected ModelSubstitutionFallbackAllowed, got %s", st.SubstitutionState)
		}
		if st.ReportedModelID != "gpt-5.3-codex" || st.RequestedModelID != "gpt-5.3-codex-spark" {
			t.Errorf("identity tracking fields mismatch: %+v", st)
		}

		// AppServer client StartTurn succeeds with recorded fallback state
		turn, err := client.StartTurn(context.Background(), adapter.TurnStartRequest{
			ThreadID:                   "th_fallback_test_2",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: true,
			Prompt:                     "Implement feature with fallback",
		})
		if err != nil {
			t.Fatalf("StartTurn with fallback enabled failed: %v", err)
		}
		if turn.ModelIdentity.SubstitutionState != adapter.ModelSubstitutionFallbackAllowed {
			t.Fatalf("expected turn SubstitutionState %s, got %s", adapter.ModelSubstitutionFallbackAllowed, turn.ModelIdentity.SubstitutionState)
		}
	})

	// 3. Exact Match (Zero Substitution)
	t.Run("ExactMatch_PassesWithoutFallback", func(t *testing.T) {
		server.SetTurnModel("gpt-5.3-codex-spark")

		st, err := adapter.VerifyModelIdentity("gpt-5.3-codex-spark", "gpt-5.3-codex-spark", false)
		if err != nil {
			t.Fatalf("exact match should succeed without error: %v", err)
		}
		if st.SubstitutionState != adapter.ModelSubstitutionExact {
			t.Fatalf("expected ModelSubstitutionExact, got %s", st.SubstitutionState)
		}

		turn, err := client.StartTurn(context.Background(), adapter.TurnStartRequest{
			ThreadID:                   "th_exact_test",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: false,
			Prompt:                     "Exact match turn",
		})
		if err != nil {
			t.Fatalf("StartTurn failed on exact match: %v", err)
		}
		if turn.ModelIdentity.SubstitutionState != adapter.ModelSubstitutionExact {
			t.Fatalf("expected ModelSubstitutionExact on turn, got %s", turn.ModelIdentity.SubstitutionState)
		}
	})

	// 4. Dual-Lane Boundary & Isolation (ADR 006 / Spec Section 3)
	t.Run("DualLaneRouting_StrictlyIsolatesSubscriptionFromAPIKeyMode", func(t *testing.T) {
		cfgSubscription := config.CodexRuntimeConfig{
			Enabled:         true,
			RequiredAuth:    "chatgpt_subscription",
			CredentialOwner: "codex_process",
			RuntimeProfile:  "default",
		}

		// If runtime reports API key mode while config mandates ChatGPT subscription, fail closed
		snapAPIKey := protocol.RuntimeAuthSnapshot{
			SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
			CredentialOwner:    protocol.CredentialOwnerCodexProcess,
			Mode:               protocol.RuntimeAuthModeAPIKey,
			State:              protocol.RuntimeAuthStateAuthenticated,
			RuntimeProfile:     "default",
			ScopeHash:          protocol.ComputeScopeHash([]string{"workdir"}),
			AuthGenerationHash: protocol.ComputeAuthGenerationHash("default", "gen_api"),
			ObservedAt:         time.Now().UTC(),
		}

		prober := codexruntime.NewFakeStatusProber(snapAPIKey)
		rtService := codexruntime.NewRuntimeService(cfgSubscription, prober)
		rtService.SetBinary(&codexruntime.ResolvedBinary{Path: "codex", Version: "0.4.0", SHA256: "testbin"})

		_, err := rtService.EnsureReady(context.Background(), []string{"workdir"})
		if !errors.Is(err, codexruntime.ErrAuthModeMismatchCfg) {
			t.Fatalf("expected ErrAuthModeMismatchCfg when API key mode attempts to masquerade as subscription mode, got: %v", err)
		}

		// ClassifierProvider Spark API profile (#188) requires explicit Entitled: true
		_, err = classifier.NewOpenAISpark(classifier.OpenAISparkConfig{
			Entitled:            false, // Not entitled
			BaseURL:             "http://127.0.0.1:1",
			AllowRemote:         true,
			APIKeyRef:           "${TEST_OPENAI_KEY}",
			LookupEnv:           func(string) (string, bool) { return "sk-test-key", true },
			CapabilitiesProfile: classifier.CapabilitiesProfileOpenAISparkV1,
		})
		if err == nil {
			t.Fatal("expected error when initializing Spark API profile without explicit entitlement")
		}
		var pe *classifier.ProviderError
		if !errors.As(err, &pe) || pe.Class != "capability" {
			t.Fatalf("expected capability entitlement error, got %v (%T)", err, err)
		}
		if !strings.Contains(pe.Message, "requires explicit project capability entitlement") {
			t.Fatalf("unexpected error message for unentitled Spark API profile: %v", err)
		}
	})
}

// ============================================================================
// Suite 5: Rate Limit and Outage Handling
// (429 / capacity exhaustion fails gracefully without cross-billing API fallback)
// ============================================================================

func TestCodexSubscription_RateLimitAndOutageHandling(t *testing.T) {
	t.Parallel()

	modelsPayload := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex-spark", "displayName": "GPT-5.3 Codex Spark", "supportState": "selectable"}
		]
	}`)

	// 1. Rate Limit 429 / Quota Exhaustion
	t.Run("RateLimit429_FailsGracefullyWithoutCrossBillingFallback", func(t *testing.T) {
		client, server, cleanup := setupSubscriptionHarness(t, modelsPayload)
		defer cleanup()

		server.SetRateLimitFailure(true)

		_, err := client.StartTurn(context.Background(), adapter.TurnStartRequest{
			ThreadID:                   "th_ratelimit_test",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: false,
			Prompt:                     "Execute task under quota limit",
		})
		if err == nil {
			t.Fatal("expected StartTurn to fail when rate limit is returned")
		}

		var appErr *adapter.AppServerError
		if !errors.As(err, &appErr) {
			t.Fatalf("expected AppServerError, got: %T (%v)", err, err)
		}
		if appErr.Code != adapter.ErrCodeRateLimited {
			t.Fatalf("expected ErrCodeRateLimited, got code: %s (msg: %s)", appErr.Code, appErr.Message)
		}

		// Verify zero silent cross-billing fallback: error is returned directly to caller
		if strings.Contains(err.Error(), "sk-") {
			t.Fatal("error message contains raw secret token")
		}
	})

	// 2. Sudden Process Crash / Pipe Severance
	t.Run("ProcessCrash_FailsClosedWithRuntimeCrashed", func(t *testing.T) {
		client, server, cleanup := setupSubscriptionHarness(t, modelsPayload)
		defer cleanup()

		server.SetCrashOnTurn(true)

		_, err := client.StartTurn(context.Background(), adapter.TurnStartRequest{
			ThreadID:                   "th_crash_test",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: false,
			Prompt:                     "Turn that triggers process crash",
		})
		if err == nil {
			t.Fatal("expected StartTurn to fail when server crashes")
		}

		var appErr *adapter.AppServerError
		if !errors.As(err, &appErr) || appErr.Code != adapter.ErrCodeRuntimeCrashed {
			t.Fatalf("expected ErrCodeRuntimeCrashed, got: %v", err)
		}
	})

	// 3. Request Timeout
	t.Run("RequestTimeout_FailsClosedWithRequestTimeout", func(t *testing.T) {
		client, server, cleanup := setupSubscriptionHarness(t, modelsPayload)
		defer cleanup()

		server.SetHangOnTurn(true)

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()

		_, err := client.StartTurn(ctx, adapter.TurnStartRequest{
			ThreadID:                   "th_hang_test",
			ModelID:                    "gpt-5.3-codex-spark",
			AllowProviderModelFallback: false,
			Prompt:                     "Turn that hangs",
		})
		if err == nil {
			t.Fatal("expected StartTurn to fail on context timeout")
		}

		var appErr *adapter.AppServerError
		if !errors.As(err, &appErr) || (appErr.Code != adapter.ErrCodeRequestTimeout && !errors.Is(err, context.DeadlineExceeded)) {
			t.Fatalf("expected request timeout error, got: %v", err)
		}
	})
}

// ============================================================================
// Suite 6: Concurrency and Stress Testing
// (50 concurrent sessions querying catalog, starting turns, and handling approvals)
// ============================================================================

func TestCodexSubscription_ConcurrencyAndStress(t *testing.T) {
	t.Parallel()

	modelsPayload := json.RawMessage(`{
		"models": [
			{"id": "gpt-5.3-codex", "displayName": "GPT-5.3 Codex", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000},
			{"id": "gpt-5.3-codex-spark", "displayName": "GPT-5.3 Codex Spark", "supportState": "selectable", "capabilities": 7, "contextWindow": 128000, "defaultReasoningEffort": "high"},
			{"id": "o3-mini", "displayName": "o3-mini", "supportState": "selectable", "capabilities": 3, "contextWindow": 200000}
		]
	}`)

	client, server, cleanup := setupSubscriptionHarness(t, modelsPayload)
	defer cleanup()

	pm := codexruntime.NewCachePartitionManager()
	catService := adapter.NewModelCatalogService(client, adapter.ModelCatalogConfig{
		TTL:              10 * time.Minute,
		PartitionManager: pm,
	})

	const concurrentSessions = 50
	var (
		wg              sync.WaitGroup
		turnCount       atomic.Int64
		catalogHitCount atomic.Int64
		approvalCount   atomic.Int64
		errCount        atomic.Int64
	)

	// Approval bridge listener: intercept server approval requests and respond
	approvalDone := make(chan struct{})
	go func() {
		defer close(approvalDone)
		for req := range client.ApprovalRequests() {
			approvalCount.Add(1)
			// Route through policy
			resp := adapter.RouteApprovalRequest(context.Background(), req, adapter.HookPolicy{})
			_ = client.RespondApproval(context.Background(), resp)
		}
	}()

	authGen := protocol.ComputeAuthGenerationHash("default", "gen_stress_test")

	// Launch 50 concurrent sessions
	wg.Add(concurrentSessions)
	for i := 1; i <= concurrentSessions; i++ {
		sessionIdx := i
		go func() {
			defer wg.Done()

			scope := []string{"workspace:/repo/stress_shared_workspace"}
			snap := protocol.RuntimeAuthSnapshot{
				SchemaVersion:      protocol.RuntimeAuthSnapshotSchemaVersion,
				CredentialOwner:    protocol.CredentialOwnerCodexProcess,
				Mode:               protocol.RuntimeAuthModeChatGPTSubscription,
				State:              protocol.RuntimeAuthStateAuthenticated,
				RuntimeProfile:     "default",
				ScopeHash:          protocol.ComputeScopeHash(scope),
				AuthGenerationHash: authGen,
				ObservedAt:         time.Now().UTC(),
			}

			// 1. Concurrent Catalog Discovery
			catSnap, err := catService.Discover(context.Background(), snap)
			if err != nil {
				errCount.Add(1)
				t.Errorf("session %d: catalog discover failed: %v", sessionIdx, err)
				return
			}
			if len(catSnap.Models) != 3 {
				errCount.Add(1)
				t.Errorf("session %d: expected 3 models, got %d", sessionIdx, len(catSnap.Models))
				return
			}
			catalogHitCount.Add(1)

			// 2. Concurrent Thread and Turn Start
			thID := fmt.Sprintf("th_stress_%d", sessionIdx)
			turnID := fmt.Sprintf("turn_stress_%d", sessionIdx)

			_, err = client.StartThread(context.Background(), adapter.ThreadStartRequest{
				ThreadID: thID,
				ModelID:  "gpt-5.3-codex-spark",
			})
			if err != nil {
				errCount.Add(1)
				t.Errorf("session %d: StartThread failed: %v", sessionIdx, err)
				return
			}

			turn, err := client.StartTurn(context.Background(), adapter.TurnStartRequest{
				ThreadID: thID,
				TurnID:   turnID,
				ModelID:  "gpt-5.3-codex-spark",
				Prompt:   fmt.Sprintf("Prompt from session %d", sessionIdx),
			})
			if err != nil {
				errCount.Add(1)
				t.Errorf("session %d: StartTurn failed: %v", sessionIdx, err)
				return
			}
			if turn.ModelIdentity.SubstitutionState != adapter.ModelSubstitutionExact {
				errCount.Add(1)
				t.Errorf("session %d: expected ModelSubstitutionExact, got %s", sessionIdx, turn.ModelIdentity.SubstitutionState)
				return
			}
			turnCount.Add(1)

			// 3. Trigger approval request from server for every even session
			if sessionIdx%2 == 0 {
				reqID := int64(sessionIdx * 1000)
				server.sendServerApprovalRequest(reqID, thID, turnID, "git commit -m 'stress test'")
			}
		}()
	}

	wg.Wait()

	if errCount.Load() > 0 {
		t.Fatalf("stress test encountered %d errors across %d concurrent sessions", errCount.Load(), concurrentSessions)
	}

	if catalogHitCount.Load() != concurrentSessions {
		t.Errorf("expected %d catalog hits, got %d", concurrentSessions, catalogHitCount.Load())
	}
	if turnCount.Load() != concurrentSessions {
		t.Errorf("expected %d turns completed, got %d", concurrentSessions, turnCount.Load())
	}

	// Give a short window for approvals to drain
	time.Sleep(100 * time.Millisecond)

	expectedApprovals := int64(concurrentSessions / 2)
	if approvalCount.Load() < expectedApprovals {
		t.Errorf("expected at least %d approvals processed, got %d", expectedApprovals, approvalCount.Load())
	}
}
