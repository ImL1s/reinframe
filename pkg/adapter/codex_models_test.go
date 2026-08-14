package adapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ImL1s/reinframe/pkg/adapter"
	"github.com/ImL1s/reinframe/pkg/codexruntime"
	"github.com/ImL1s/reinframe/pkg/protocol"
)

// mockAppServerClient implements adapter.AppServerClient for testing ModelCatalogService.
type mockAppServerClient struct {
	mu           sync.Mutex
	listModelsFn func(ctx context.Context) (json.RawMessage, error)
	callCount    int
}

func (m *mockAppServerClient) Start(ctx context.Context) error { return nil }
func (m *mockAppServerClient) StartThread(ctx context.Context, req adapter.ThreadStartRequest) (adapter.Thread, error) {
	return adapter.Thread{}, nil
}
func (m *mockAppServerClient) ResumeThread(ctx context.Context, req adapter.ThreadResumeRequest) (adapter.Thread, error) {
	return adapter.Thread{}, nil
}
func (m *mockAppServerClient) StartTurn(ctx context.Context, req adapter.TurnStartRequest) (adapter.Turn, error) {
	return adapter.Turn{}, nil
}
func (m *mockAppServerClient) InterruptTurn(ctx context.Context, threadID, turnID string) error {
	return nil
}
func (m *mockAppServerClient) Events() <-chan adapter.RuntimeEvent { return nil }
func (m *mockAppServerClient) ApprovalRequests() <-chan adapter.ApprovalRequest {
	return nil
}
func (m *mockAppServerClient) RespondApproval(ctx context.Context, response adapter.ApprovalResponse) error {
	return nil
}
func (m *mockAppServerClient) Close(ctx context.Context) error { return nil }
func (m *mockAppServerClient) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method == "model/list" {
		return m.ListModels(ctx)
	}
	return nil, nil
}
func (m *mockAppServerClient) ListModels(ctx context.Context) (json.RawMessage, error) {
	m.mu.Lock()
	m.callCount++
	fn := m.listModelsFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return json.RawMessage(`{"models":[]}`), nil
}

func (m *mockAppServerClient) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount
}

func TestParseModelListResponse_FormatsAndEnvelopes(t *testing.T) {
	t.Parallel()

	t.Run("standard_models_wrapper_camelCase", func(t *testing.T) {
		raw := json.RawMessage(`{
			"models": [
				{
					"id": "gpt-5.3-codex",
					"displayName": "GPT-5.3 Codex",
					"supportState": "selectable",
					"capabilities": 3,
					"contextWindow": 200000,
					"inputModalities": ["text", "image"],
					"defaultReasoningEffort": "medium",
					"isDefault": true
				},
				{
					"id": "gpt-5.3-codex-spark",
					"displayName": "GPT-5.3 Codex Spark (Research Preview)",
					"supportState": "discovered",
					"capabilities": 1,
					"contextWindow": 128000,
					"inputModalities": ["text"],
					"defaultReasoningEffort": "high",
					"isDefault": false
				}
			]
		}`)

		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}

		m0 := models[0]
		if m0.ModelID != "gpt-5.3-codex" || m0.DisplayName != "GPT-5.3 Codex" || m0.SupportState != adapter.ModelSupportStateSelectable {
			t.Errorf("unexpected m0: %+v", m0)
		}
		if m0.ContextWindow != 200000 || !m0.IsDefault || m0.DefaultReasoningEffort != "medium" {
			t.Errorf("unexpected m0 fields: %+v", m0)
		}
		if len(m0.InputModalities) != 2 || m0.InputModalities[0] != "text" || m0.InputModalities[1] != "image" {
			t.Errorf("unexpected modalities: %v", m0.InputModalities)
		}

		m1 := models[1]
		if m1.ModelID != "gpt-5.3-codex-spark" || m1.SupportState != adapter.ModelSupportStateDiscovered {
			t.Errorf("unexpected m1: %+v", m1)
		}
	})

	t.Run("data_wrapper_snake_case", func(t *testing.T) {
		raw := json.RawMessage(`{
			"data": [
				{
					"model_id": "custom-codex-model",
					"display_name": "Custom Codex Model",
					"support_state": "capability_pinned",
					"context_window": 64000,
					"modalities": ["text"],
					"reasoning_effort": "low",
					"default": false
				}
			]
		}`)

		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if len(models) != 1 {
			t.Fatalf("expected 1 model, got %d", len(models))
		}
		if models[0].ModelID != "custom-codex-model" || models[0].SupportState != adapter.ModelSupportStateCapabilityPinned {
			t.Errorf("unexpected model: %+v", models[0])
		}
		if models[0].ContextWindow != 64000 || models[0].DefaultReasoningEffort != "low" {
			t.Errorf("unexpected fields: %+v", models[0])
		}
	})

	t.Run("direct_array_of_objects", func(t *testing.T) {
		raw := json.RawMessage(`[
			{
				"id": "model-1",
				"name": "Model 1",
				"state": "live_qualified"
			}
		]`)

		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("failed to parse: %v", err)
		}
		if len(models) != 1 || models[0].ModelID != "model-1" || models[0].SupportState != adapter.ModelSupportStateLiveQualified {
			t.Errorf("unexpected model: %+v", models[0])
		}
	})

	t.Run("string_array_of_model_ids", func(t *testing.T) {
		raw := json.RawMessage(`["gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5-codex"]`)

		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("failed to parse string array: %v", err)
		}
		if len(models) != 3 {
			t.Fatalf("expected 3 models, got %d", len(models))
		}
		for _, m := range models {
			if !m.IsSelectable() {
				t.Errorf("bare model %s should be selectable by default", m.ModelID)
			}
		}
	})

	t.Run("capabilities_parsing_object_and_array", func(t *testing.T) {
		raw := json.RawMessage(`{
			"models": [
				{
					"id": "model-caps-obj",
					"capabilities": {
						"streaming": true,
						"toolInspection": true,
						"pause": false,
						"adviceDelivery": true
					}
				},
				{
					"id": "model-caps-arr",
					"capabilities": ["event_stream", "tool_gate", "checkpoint"]
				}
			]
		}`)

		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("failed to parse capabilities: %v", err)
		}
		if len(models) != 2 {
			t.Fatalf("expected 2 models, got %d", len(models))
		}

		m0 := models[0]
		if !m0.HasCapability(protocol.CapEventStream) || !m0.HasCapability(protocol.CapToolInspection) || !m0.HasCapability(protocol.CapAdviceDelivery) {
			t.Errorf("expected capabilities missing in m0: %b", m0.Capabilities)
		}
		if m0.HasCapability(protocol.CapPause) {
			t.Errorf("pause capability should not be set in m0")
		}

		m1 := models[1]
		if !m1.HasCapability(protocol.CapEventStream) || !m1.HasCapability(protocol.CapToolGate) || !m1.HasCapability(protocol.CapCheckpoint) {
			t.Errorf("expected capabilities missing in m1: %b", m1.Capabilities)
		}
	})
}

func TestParseModelListResponse_MalformedDefenses(t *testing.T) {
	t.Parallel()

	t.Run("empty_payload", func(t *testing.T) {
		_, err := adapter.ParseModelListResponse(json.RawMessage(``))
		if err == nil {
			t.Error("expected error for empty payload")
		}
	})

	t.Run("malformed_json", func(t *testing.T) {
		_, err := adapter.ParseModelListResponse(json.RawMessage(`{invalid json`))
		if err == nil {
			t.Error("expected error for malformed json")
		}
	})

	t.Run("unsupported_root_type", func(t *testing.T) {
		_, err := adapter.ParseModelListResponse(json.RawMessage(`12345`))
		if err == nil {
			t.Error("expected error for number root")
		}
	})

	t.Run("invalid_model_element_type", func(t *testing.T) {
		_, err := adapter.ParseModelListResponse(json.RawMessage(`[123]`))
		if err == nil {
			t.Error("expected error for invalid element type")
		}
	})

	t.Run("missing_model_id_item", func(t *testing.T) {
		raw := json.RawMessage(`{"models": [{"displayName": "No ID"}]}`)
		_, err := adapter.ParseModelListResponse(raw)
		if err == nil {
			t.Error("expected error for missing model ID")
		}
	})

	t.Run("empty_models_array_succeeds", func(t *testing.T) {
		raw := json.RawMessage(`{"models": []}`)
		models, err := adapter.ParseModelListResponse(raw)
		if err != nil {
			t.Fatalf("unexpected error for empty models array: %v", err)
		}
		if len(models) != 0 {
			t.Errorf("expected 0 models, got %d", len(models))
		}
	})
}

func TestDynamicModelDiscovery_NoStaticAllowlist(t *testing.T) {
	t.Parallel()

	// Verify that any new or preview model returned by App Server is discovered and available
	// without any static hardcoded allowlist rejection.
	mockClient := &mockAppServerClient{
		listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{
				"models": [
					{
						"id": "gpt-5.3-codex-spark",
						"displayName": "GPT-5.3 Codex Spark (ChatGPT Pro Preview)",
						"supportState": "discovered",
						"capabilities": 1,
						"contextWindow": 128000
					},
					{
						"id": "gpt-6-future-preview",
						"displayName": "GPT-6 Future Preview",
						"supportState": "selectable",
						"capabilities": 31,
						"contextWindow": 500000
					},
					{
						"id": "custom-enterprise-finetuned",
						"displayName": "Custom Enterprise Fine-tuned",
						"supportState": "capability_pinned",
						"capabilities": 63,
						"contextWindow": 256000
					}
				]
			}`), nil
		},
	}

	service := adapter.NewModelCatalogService(mockClient)
	authSnap, err := protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwnerCodexProcess,
		protocol.RuntimeAuthModeChatGPTSubscription,
		protocol.RuntimeAuthStateAuthenticated,
		"default",
		"1.0.0",
		protocol.ComputeScopeHash([]string{"chatgpt_pro"}),
		protocol.ComputeAuthGenerationHash("default", "gen-1"),
		time.Now().UTC(),
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create auth snapshot: %v", err)
	}

	snap, err := service.Discover(context.Background(), authSnap)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	if len(snap.Models) != 3 {
		t.Fatalf("expected 3 models discovered, got %d", len(snap.Models))
	}

	// Verify lookups for all models
	spark, found := service.GetModel("gpt-5.3-codex-spark")
	if !found || spark.SupportState != adapter.ModelSupportStateDiscovered {
		t.Errorf("gpt-5.3-codex-spark discovery lookup failed: %+v", spark)
	}
	if service.IsSelectable("gpt-5.3-codex-spark") {
		t.Error("gpt-5.3-codex-spark in discovered state should not be selectable")
	}

	gpt6, found := service.GetModel("gpt-6-future-preview")
	if !found || !service.IsSelectable("gpt-6-future-preview") {
		t.Errorf("gpt-6-future-preview should be discovered and selectable: %+v", gpt6)
	}

	custom, found := service.GetModel("custom-enterprise-finetuned")
	if !found || !service.IsSelectable("custom-enterprise-finetuned") {
		t.Errorf("custom-enterprise-finetuned should be discovered and selectable: %+v", custom)
	}
}

func TestModelCatalogService_CachePartitioningAndInvalidation(t *testing.T) {
	t.Parallel()

	scopePro := protocol.ComputeScopeHash([]string{"chatgpt_pro"})
	scopePlus := protocol.ComputeScopeHash([]string{"chatgpt_plus"})
	authGen1 := protocol.ComputeAuthGenerationHash("default", "gen-1")
	authGen2 := protocol.ComputeAuthGenerationHash("default", "gen-2")

	mockClient := &mockAppServerClient{
		listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{
				"models": [
					{
						"id": "gpt-5.3-codex",
						"displayName": "GPT-5.3 Codex",
						"supportState": "selectable",
						"capabilities": 15
					}
				]
			}`), nil
		},
	}

	pm := codexruntime.NewCachePartitionManager()
	service := adapter.NewModelCatalogService(mockClient, adapter.ModelCatalogConfig{
		TTL:              10 * time.Minute,
		PartitionManager: pm,
	})

	snap1, err := protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwnerCodexProcess,
		protocol.RuntimeAuthModeChatGPTSubscription,
		protocol.RuntimeAuthStateAuthenticated,
		"default",
		"1.0.0",
		scopePro,
		authGen1,
		time.Now().UTC(),
		nil,
	)
	if err != nil {
		t.Fatalf("failed to create snap1: %v", err)
	}

	// 1. Initial discovery -> should call client
	cat1, err := service.Discover(context.Background(), snap1)
	if err != nil {
		t.Fatalf("cat1 discovery failed: %v", err)
	}
	if mockClient.CallCount() != 1 {
		t.Errorf("expected 1 client call, got %d", mockClient.CallCount())
	}

	// 2. Second discovery with identical partition -> should hit cache without calling client
	cat2, err := service.Discover(context.Background(), snap1)
	if err != nil {
		t.Fatalf("cat2 discovery failed: %v", err)
	}
	if mockClient.CallCount() != 1 {
		t.Errorf("expected cache hit (1 client call), got %d", mockClient.CallCount())
	}
	if cat1.CatalogHash != cat2.CatalogHash {
		t.Errorf("catalog hash mismatch: %s vs %s", cat1.CatalogHash, cat2.CatalogHash)
	}

	// 3. Auth generation hash changes -> invalidates cache and queries client
	snapAuthChanged := snap1
	snapAuthChanged.AuthGenerationHash = authGen2
	catAuthChanged, err := service.Discover(context.Background(), snapAuthChanged)
	if err != nil {
		t.Fatalf("discovery on auth change failed: %v", err)
	}
	if mockClient.CallCount() != 2 {
		t.Errorf("expected 2 client calls after auth change, got %d", mockClient.CallCount())
	}
	if catAuthChanged.AuthGenerationHash != authGen2 {
		t.Errorf("expected snapshot to reflect new auth gen hash")
	}

	// 4. Scope hash changes -> invalidates cache and queries client
	snapScopeChanged := snapAuthChanged
	snapScopeChanged.ScopeHash = scopePlus
	catScopeChanged, err := service.Discover(context.Background(), snapScopeChanged)
	if err != nil {
		t.Fatalf("discovery on scope change failed: %v", err)
	}
	if mockClient.CallCount() != 3 {
		t.Errorf("expected 3 client calls after scope change, got %d", mockClient.CallCount())
	}
	if catScopeChanged.ScopeHash != scopePlus {
		t.Errorf("expected snapshot to reflect new scope hash")
	}

	// 5. Unauthenticated state -> discovery fails and clears cache
	snapUnauth := snapScopeChanged
	snapUnauth.State = protocol.RuntimeAuthStateUnauthenticated
	_, err = service.Discover(context.Background(), snapUnauth)
	if !errors.Is(err, codexruntime.ErrUnauthenticatedPartition) {
		t.Errorf("expected ErrUnauthenticatedPartition, got %v", err)
	}

	// Cache should be cleared
	if _, ok := service.CurrentSnapshot(); ok {
		t.Error("expected cache to be invalidated after unauthenticated snapshot")
	}
}

func TestModelCatalogService_StateTransitionsAndSelection(t *testing.T) {
	t.Parallel()

	modelsJSON := `{
		"models": [
			{
				"id": "model-discovered",
				"supportState": "discovered",
				"capabilities": 7
			},
			{
				"id": "model-selectable",
				"supportState": "selectable",
				"capabilities": 7
			},
			{
				"id": "model-pinned",
				"supportState": "capability_pinned",
				"capabilities": 7
			},
			{
				"id": "model-qualified",
				"supportState": "live_qualified",
				"capabilities": 7
			}
		]
	}`

	mockClient := &mockAppServerClient{
		listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
			return json.RawMessage(modelsJSON), nil
		},
	}

	service := adapter.NewModelCatalogService(mockClient)
	authSnap, _ := protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwnerCodexProcess,
		protocol.RuntimeAuthModeChatGPTSubscription,
		protocol.RuntimeAuthStateAuthenticated,
		"default",
		"1.0.0",
		protocol.ComputeScopeHash(nil),
		protocol.ComputeAuthGenerationHash("default", "gen-1"),
		time.Now().UTC(),
		nil,
	)

	_, err := service.Discover(context.Background(), authSnap)
	if err != nil {
		t.Fatalf("discovery failed: %v", err)
	}

	// State check
	if service.IsSelectable("model-discovered") {
		t.Error("discovered model must not be selectable")
	}
	if !service.IsSelectable("model-selectable") {
		t.Error("selectable model must be selectable")
	}
	if !service.IsSelectable("model-pinned") {
		t.Error("capability_pinned model must be selectable")
	}
	if !service.IsSelectable("model-qualified") {
		t.Error("live_qualified model must be selectable")
	}

	// FindQualified should only return selectable models matching capability
	qualified := service.FindQualified(1)
	if len(qualified) != 3 {
		t.Fatalf("expected 3 qualified selectable models, got %d", len(qualified))
	}
	for _, m := range qualified {
		if m.ModelID == "model-discovered" {
			t.Errorf("discovered model must not appear in FindQualified results")
		}
	}
}

func TestModelCatalogService_ClientErrorAndTimeoutDefense(t *testing.T) {
	t.Parallel()

	t.Run("server_rpc_error", func(t *testing.T) {
		mockClient := &mockAppServerClient{
			listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
				return nil, adapter.NewAppServerError(adapter.ErrCodeAuthRequired, "user not logged in")
			},
		}

		service := adapter.NewModelCatalogService(mockClient)
		authSnap, _ := protocol.NewRuntimeAuthSnapshot(
			protocol.CredentialOwnerCodexProcess,
			protocol.RuntimeAuthModeChatGPTSubscription,
			protocol.RuntimeAuthStateAuthenticated,
			"default",
			"1.0.0",
			protocol.ComputeScopeHash(nil),
			protocol.ComputeAuthGenerationHash("default", "gen-1"),
			time.Now().UTC(),
			nil,
		)

		_, err := service.Discover(context.Background(), authSnap)
		if err == nil {
			t.Fatal("expected discovery error when client fails")
		}
	})

	t.Run("context_cancelled", func(t *testing.T) {
		mockClient := &mockAppServerClient{
			listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
				return nil, ctx.Err()
			},
		}

		service := adapter.NewModelCatalogService(mockClient)
		authSnap, _ := protocol.NewRuntimeAuthSnapshot(
			protocol.CredentialOwnerCodexProcess,
			protocol.RuntimeAuthModeChatGPTSubscription,
			protocol.RuntimeAuthStateAuthenticated,
			"default",
			"1.0.0",
			protocol.ComputeScopeHash(nil),
			protocol.ComputeAuthGenerationHash("default", "gen-1"),
			time.Now().UTC(),
			nil,
		)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := service.Discover(ctx, authSnap)
		if err == nil {
			t.Fatal("expected error on cancelled context")
		}
	})
}

func TestModelCatalogService_ConcurrencyAndRaceSafety(t *testing.T) {
	t.Parallel()

	modelsRaw := json.RawMessage(`{
		"models": [
			{
				"id": "model-fast",
				"displayName": "Fast Model",
				"supportState": "selectable",
				"capabilities": 31,
				"contextWindow": 100000
			},
			{
				"id": "model-smart",
				"displayName": "Smart Model",
				"supportState": "live_qualified",
				"capabilities": 63,
				"contextWindow": 200000
			}
		]
	}`)

	mockClient := &mockAppServerClient{
		listModelsFn: func(ctx context.Context) (json.RawMessage, error) {
			return modelsRaw, nil
		},
	}

	service := adapter.NewModelCatalogService(mockClient, adapter.ModelCatalogConfig{
		TTL: 1 * time.Second,
	})

	authSnap, _ := protocol.NewRuntimeAuthSnapshot(
		protocol.CredentialOwnerCodexProcess,
		protocol.RuntimeAuthModeChatGPTSubscription,
		protocol.RuntimeAuthStateAuthenticated,
		"default",
		"1.0.0",
		protocol.ComputeScopeHash([]string{"chatgpt_pro"}),
		protocol.ComputeAuthGenerationHash("default", "gen-race"),
		time.Now().UTC(),
		nil,
	)

	// Perform initial discovery
	_, err := service.Discover(context.Background(), authSnap)
	if err != nil {
		t.Fatalf("initial discovery failed: %v", err)
	}

	const goroutines = 30
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch (workerID + j) % 6 {
				case 0:
					_, _ = service.Discover(context.Background(), authSnap)
				case 1:
					_, _ = service.GetModel("model-fast")
					_, _ = service.GetModel("model-smart")
				case 2:
					_ = service.IsSelectable("model-fast")
					_ = service.IsSelectable("nonexistent")
				case 3:
					_ = service.FindQualified(15)
				case 4:
					_, _ = service.CurrentSnapshot()
					_ = service.AllModels()
				case 5:
					if j%20 == 0 {
						service.Invalidate("test_cycle")
					}
				}
			}
		}(i)
	}

	wg.Wait()
}
