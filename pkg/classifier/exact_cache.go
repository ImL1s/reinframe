package classifier

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Exact assessment cache schema (#138).
const (
	SchemaExactCacheKey    = "reinframe.exact_assessment_cache_key.v1"
	SchemaCachedAssessment = "reinframe.cached_assessment.v1"

	// ExactCacheLayer values for observability (distinct from provider prefix caches).
	ExactCacheLayerNone           = "none"
	ExactCacheLayerReinframeExact = "reinframe_exact"
)

// ExactCacheConfig is the process-local exact RawAssessment cache policy (#138).
// Safe default: disabled until operators enable with benchmarks.
type ExactCacheConfig struct {
	Enabled      bool
	MaxEntries   int           // default 1024 when enabled
	MaxBytes     int           // default 16 MiB when enabled
	TTL          time.Duration // default 10m when enabled
	Singleflight bool          // default true when enabled
}

// Normalize applies closed defaults when Enabled.
func (c ExactCacheConfig) Normalize() ExactCacheConfig {
	out := c
	if !out.Enabled {
		return ExactCacheConfig{}
	}
	if out.MaxEntries <= 0 {
		out.MaxEntries = 1024
	}
	if out.MaxBytes <= 0 {
		out.MaxBytes = 16 << 20
	}
	if out.TTL <= 0 {
		out.TTL = 10 * time.Minute
	}
	// Singleflight defaults on when enabled (zero value false only if explicitly disabled via separate flag;
	// callers should set Singleflight true when enabling).
	return out
}

// ValidateExactCacheConfig fails closed on absurd bounds.
func ValidateExactCacheConfig(c ExactCacheConfig) error {
	if !c.Enabled {
		if c.MaxEntries != 0 || c.MaxBytes != 0 || c.TTL != 0 || c.Singleflight {
			// Allow explicit false singleflight with zeros only when disabled.
			if c.MaxEntries != 0 || c.MaxBytes != 0 || c.TTL != 0 {
				return fmt.Errorf("classifier: exact cache disabled requires empty bounds")
			}
		}
		return nil
	}
	if c.MaxEntries < 1 || c.MaxEntries > 1_000_000 {
		return fmt.Errorf("classifier: exact cache max_entries out of range")
	}
	if c.MaxBytes < 1024 || c.MaxBytes > 256<<20 {
		return fmt.Errorf("classifier: exact cache max_bytes out of range")
	}
	if c.TTL < time.Second || c.TTL > 24*time.Hour {
		return fmt.Errorf("classifier: exact cache ttl out of range")
	}
	return nil
}

// CachedAssessment is the admitted provider-stage value only — never ResolvedDecision (#138).
type CachedAssessment struct {
	SchemaVersion   string
	Assessment      RawAssessment
	SourceAuditID   string
	ProviderMeta    ProviderMeta
	CreatedAt       time.Time
	ExpiresAt       time.Time
	CacheKeyVersion string
	KeyHash         string
}

// ExactCacheStats is bounded observability for the process-local cache.
type ExactCacheStats struct {
	Lookups    int64
	Hits       int64
	Misses     int64
	Admissions int64
	Rejections int64
	Evictions  int64
	Coalesced  int64
	Entries    int
	Bytes      int
}

// ExactAssessmentCache is a bounded process-local LRU+TTL store.
type ExactAssessmentCache struct {
	mu       sync.Mutex
	cfg      ExactCacheConfig
	entries  map[string]*exactCacheEntry
	order    []string // oldest → newest (LRU)
	bytes    int
	stats    ExactCacheStats
	now      func() time.Time
	inflight map[string]*exactInflight
}

type exactCacheEntry struct {
	key  string
	val  CachedAssessment
	size int
}

type exactInflight struct {
	done chan struct{}
	res  ProviderResult
	err  error
	// wait is the number of active waiters still interested in the shared result.
	// The shared provider call is detached from any single waiter context.
	wait int
}

// NewExactAssessmentCache builds a process-local cache. Disabled configs return a no-op shell.
func NewExactAssessmentCache(cfg ExactCacheConfig, now func() time.Time) (*ExactAssessmentCache, error) {
	cfg = cfg.Normalize()
	if err := ValidateExactCacheConfig(cfg); err != nil {
		return nil, err
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ExactAssessmentCache{
		cfg:      cfg,
		entries:  make(map[string]*exactCacheEntry),
		order:    nil,
		now:      now,
		inflight: make(map[string]*exactInflight),
	}, nil
}

// Enabled reports whether the cache admits lookups/stores.
func (c *ExactAssessmentCache) Enabled() bool {
	return c != nil && c.cfg.Enabled
}

// Stats returns a snapshot of counters.
func (c *ExactAssessmentCache) Stats() ExactCacheStats {
	if c == nil {
		return ExactCacheStats{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.stats
	s.Entries = len(c.entries)
	s.Bytes = c.bytes
	return s
}

// Get returns a non-expired entry.
func (c *ExactAssessmentCache) Get(keyHash string) (CachedAssessment, bool) {
	if c == nil || !c.cfg.Enabled || keyHash == "" {
		return CachedAssessment{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.Lookups++
	ent, ok := c.entries[keyHash]
	if !ok {
		c.stats.Misses++
		return CachedAssessment{}, false
	}
	if !ent.val.ExpiresAt.IsZero() && !c.now().Before(ent.val.ExpiresAt) {
		c.removeLocked(keyHash)
		c.stats.Misses++
		c.stats.Evictions++
		return CachedAssessment{}, false
	}
	// Touch LRU.
	c.touchLocked(keyHash)
	c.stats.Hits++
	return ent.val, true
}

// Admit stores a validated successful assessment. Returns false if rejected.
func (c *ExactAssessmentCache) Admit(keyHash string, val CachedAssessment) bool {
	if c == nil || !c.cfg.Enabled || keyHash == "" {
		return false
	}
	if err := validateCachedAssessment(val); err != nil {
		c.mu.Lock()
		c.stats.Rejections++
		c.mu.Unlock()
		return false
	}
	raw, err := json.Marshal(val)
	if err != nil {
		c.mu.Lock()
		c.stats.Rejections++
		c.mu.Unlock()
		return false
	}
	size := len(raw)
	if size > c.cfg.MaxBytes {
		c.mu.Lock()
		c.stats.Rejections++
		c.mu.Unlock()
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.entries[keyHash]; ok {
		c.bytes -= prev.size
		delete(c.entries, keyHash)
		c.removeOrderLocked(keyHash)
	}
	for (len(c.entries) >= c.cfg.MaxEntries || c.bytes+size > c.cfg.MaxBytes) && len(c.order) > 0 {
		victim := c.order[0]
		c.removeLocked(victim)
		c.stats.Evictions++
	}
	if len(c.entries) >= c.cfg.MaxEntries || c.bytes+size > c.cfg.MaxBytes {
		c.stats.Rejections++
		return false
	}
	val.KeyHash = keyHash
	c.entries[keyHash] = &exactCacheEntry{key: keyHash, val: val, size: size}
	c.order = append(c.order, keyHash)
	c.bytes += size
	c.stats.Admissions++
	return true
}

func (c *ExactAssessmentCache) touchLocked(key string) {
	c.removeOrderLocked(key)
	c.order = append(c.order, key)
}

func (c *ExactAssessmentCache) removeOrderLocked(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

func (c *ExactAssessmentCache) removeLocked(key string) {
	ent, ok := c.entries[key]
	if !ok {
		return
	}
	c.bytes -= ent.size
	delete(c.entries, key)
	c.removeOrderLocked(key)
}

func validateCachedAssessment(v CachedAssessment) error {
	if v.SchemaVersion != "" && v.SchemaVersion != SchemaCachedAssessment {
		return fmt.Errorf("bad cached assessment schema")
	}
	if v.Assessment.SchemaVersion != SchemaRawAssessment {
		return fmt.Errorf("bad assessment schema")
	}
	if v.Assessment.ParseStatus != ParseStatusOK && v.Assessment.ParseStatus != "" {
		return fmt.Errorf("parse status not ok")
	}
	if v.ProviderMeta.ParseStatus == ParseStatusError || v.ProviderMeta.ParseStatus == ParseStatusInvalid {
		return fmt.Errorf("meta parse not ok")
	}
	if v.ProviderMeta.ErrorClass != "" || v.ProviderMeta.FallbackReason != "" {
		return fmt.Errorf("error/fallback not cacheable")
	}
	if !ValidateSeverity(v.Assessment.Severity) || !ValidateRawReasonCode(v.Assessment.ReasonCode) {
		return fmt.Errorf("invalid assessment fields")
	}
	return nil
}

// ExactCacheIdentity is trusted wrapper identity mixed into the key (not model-controlled).
type ExactCacheIdentity struct {
	ProviderKind        string
	ModelID             string
	ModelVersion        string
	CapabilitiesProfile string
	EgressProfile       string
	// ParserSchema is the closed RawAssessment schema version.
	ParserSchema string
}

// BuildExactCacheKeyHash returns a secret-free content-addressed key hash, or "" if non-cacheable.
func BuildExactCacheKeyHash(id ExactCacheIdentity, req ProviderRequest) (string, bool) {
	if id.ProviderKind == "" || id.ModelID == "" {
		return "", false
	}
	// Production reject: legacy fixture mode not identity-safe for exact cache.
	if req.Input.AllowLegacyFixtureIDs {
		return "", false
	}
	// SECURITY hard class: never exact-cache (human/hard-security path isolation).
	if strings.EqualFold(req.Input.PolicyClass, PolicyClassSecurity) {
		return "", false
	}
	// Unknown/empty revisions that compromise identity: require explicit ruleset hash when ruleset id set.
	if req.Input.RulesetID != "" && req.Input.RulesetHash == "" {
		return "", false
	}
	// StablePrefixHash required. PromptHash/InputHash are intentionally omitted:
	// full plan hashes may embed Stage-2-only fields (e.g. challenge retry budget)
	// that must not bust exact Stage-1 cache identity (#138). Dynamic identity is
	// rebuilt from events, action fingerprint, and challenge hashes below.
	if req.Prompt.StablePrefixHash == "" {
		return "", false
	}

	type eventPart struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Seq  uint64 `json:"seq"`
		Sum  string `json:"sum"` // hash of summary, not raw text
	}
	canonEvents := func(evs []EventDigest) []eventPart {
		out := make([]eventPart, 0, len(evs))
		for _, e := range evs {
			sum := e.ContentHash
			if sum == "" {
				sum = shortHash(e.Summary)
			}
			out = append(out, eventPart{
				ID: e.EventID, Type: e.EventType, Seq: e.Sequence,
				Sum: sum + "|" + e.RelatedTo,
			})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Seq != out[j].Seq {
				return out[i].Seq < out[j].Seq
			}
			return out[i].ID < out[j].ID
		})
		return out
	}

	var actionFP string
	if req.Input.ProposedAction != nil {
		// Canonical JSON fingerprint — preserve argument order; no delimiter joining.
		pa := req.Input.ProposedAction
		actionObj := map[string]any{
			"schema":    pa.SchemaVersion,
			"tool":      pa.ToolName,
			"class":     pa.ToolClass,
			"command":   pa.Command,
			"path":      pa.FilePath,
			"args":      append([]string(nil), pa.Arguments...), // original order
			"scope":     append([]string(nil), pa.TargetScope...),
			"ws_rev":    pa.WorkspaceRevision,
			"contract":  pa.ContractRevision,
			"truncated": pa.Truncated,
			"source":    pa.Source,
			"parse":     pa.ParseStatus,
			"payload_h": shortHash(string(pa.RedactedPayload)),
		}
		rawAct, err := json.Marshal(actionObj)
		if err != nil {
			return "", false
		}
		actionFP = shortHash(string(rawAct))
	}
	var ch any
	if req.Input.Challenge != nil {
		c := req.Input.Challenge
		// Full Stage-1-visible challenge identity. RetryBudget excluded so unchanged
		// retries may hit; cache never restores/consumes budget.
		ch = map[string]any{
			"id": c.ChallengeID, "state": c.State, "block_class": c.BlockClass,
			"reason": c.ReasonCode, "appealability": c.Appealability,
			"required_claims": append([]string(nil), c.RequiredClaims...),
			"claims":          append([]string(nil), c.Claims...),
			"evidence":        append([]string(nil), c.EvidenceEventIDs...),
			"expires_seq":     c.ExpiresAtSequence,
			"original_action": c.OriginalActionID,
			"concrete":        shortHash(c.ConcreteValue),
			"prevented":       shortHash(c.PreventedFailureOrThreat),
			"cost":            shortHash(c.EstimatedCost),
			"alts":            shortHash(c.AlternativesConsidered),
			"scope":           shortHash(c.ScopeLimit),
			"verify":          shortHash(c.VerificationPlan),
			"rollback":        shortHash(c.RollbackPlan),
			"action_fp":       c.ActionFingerprint,
		}
	}
	acc := append([]string(nil), req.Input.TaskAnchor.Acceptance...)
	sort.Strings(acc)

	payload := map[string]any{
		"schema":             SchemaExactCacheKey,
		"provider":           id.ProviderKind,
		"model":              id.ModelID,
		"model_version":      id.ModelVersion,
		"capabilities":       id.CapabilitiesProfile,
		"egress":             id.EgressProfile,
		"parser_schema":      firstNonEmpty(id.ParserSchema, SchemaRawAssessment),
		"prompt_schema":      req.Prompt.SchemaVersion,
		"stable_prefix_hash": req.Prompt.StablePrefixHash,
		"ruleset_id":         req.Input.RulesetID,
		"ruleset_hash":       req.Input.RulesetHash,
		"policy_class":       req.Input.PolicyClass,
		"task_id":            req.Input.TaskAnchor.TaskID,
		"task_obj_hash":      shortHash(req.Input.TaskAnchor.Objective),
		"task_acceptance":    acc,
		"contract_rev":       req.Input.ContractRevision,
		"evidence_rev":       req.Input.EvidenceRevision,
		"window":             req.Input.Window,
		"recent":             canonEvents(req.Input.RecentEvents),
		"related":            canonEvents(req.Input.RelatedEvents),
		"action_fp":          actionFP,
		"challenge":          ch,
		// Model-visible exception flags (in dynamic prompt suffix).
		"user_exception": req.Input.UserException,
		"repo_exception": req.Input.RepoPolicyException,
		"flaky_invest":   req.Input.FlakyInvestigation,
		// Session/tenant partition: empty SessionID is a shared default partition only.
		// EgressProfile additionally isolates multi-egress deployments.
		"session_id": req.Input.SessionID,
		// RetryBudget intentionally omitted (Stage-2 budget authority, not Stage-1 identity).
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", false
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16]), true
}

func shortHash(s string) string {
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// CachingClassifierProvider wraps an inner provider with exact cache + optional singleflight (#138).
// Stage-2 is never cached — only Stage-1 ProviderResult.Assessment.
type CachingClassifierProvider struct {
	Inner    ClassifierProvider
	Cache    *ExactAssessmentCache
	Identity ExactCacheIdentity
	Now      func() time.Time
}

// Assess implements ClassifierProvider with exact-hit short-circuit.
func (p *CachingClassifierProvider) Assess(ctx context.Context, req ProviderRequest) (ProviderResult, error) {
	if p == nil || p.Inner == nil {
		return ProviderResult{}, newProviderError("config", "nil caching provider", false, 0)
	}
	if ctx == nil {
		return ProviderResult{}, newProviderError("config", "nil context", false, 0)
	}
	if err := ctx.Err(); err != nil {
		return ProviderResult{}, err
	}

	keyHash, ok := BuildExactCacheKeyHash(p.Identity, req)
	if !ok || p.Cache == nil || !p.Cache.Enabled() {
		return p.Inner.Assess(ctx, req)
	}

	if hit, ok := p.Cache.Get(keyHash); ok {
		return p.hitResult(hit, keyHash, req), nil
	}

	if !p.Cache.cfg.Singleflight {
		return p.assessAndAdmit(ctx, req, keyHash)
	}
	return p.assessCoalesced(ctx, req, keyHash)
}

func (p *CachingClassifierProvider) assessAndAdmit(ctx context.Context, req ProviderRequest, keyHash string) (ProviderResult, error) {
	res, err := p.Inner.Assess(ctx, req)
	if err != nil {
		return res, err
	}
	p.tryAdmit(keyHash, res)
	return res, nil
}

func (p *CachingClassifierProvider) assessCoalesced(ctx context.Context, req ProviderRequest, keyHash string) (ProviderResult, error) {
	c := p.Cache
	c.mu.Lock()
	if hit, ok := c.entries[keyHash]; ok {
		if hit.val.ExpiresAt.IsZero() || c.now().Before(hit.val.ExpiresAt) {
			c.stats.Lookups++
			c.stats.Hits++
			c.touchLocked(keyHash)
			val := hit.val
			c.mu.Unlock()
			return p.hitResult(val, keyHash, req), nil
		}
		c.removeLocked(keyHash)
		c.stats.Evictions++
	}
	if inf, ok := c.inflight[keyHash]; ok {
		inf.wait++
		c.stats.Coalesced++
		c.mu.Unlock()
		return p.waitInflight(ctx, inf, false, keyHash, req)
	}
	// First caller becomes the shared-flight owner but does not bind the provider
	// call to its personal ctx: one waiter cancel must not cancel remaining waiters.
	inf := &exactInflight{done: make(chan struct{}), wait: 1}
	c.inflight[keyHash] = inf
	c.mu.Unlock()

	// Detach shared Assess from any single waiter cancellation.
	sharedCtx := context.WithoutCancel(ctx)
	go func() {
		res, err := p.Inner.Assess(sharedCtx, req)
		if err == nil {
			p.tryAdmit(keyHash, res)
		}
		inf.res, inf.err = copyProviderResult(res), err
		c.mu.Lock()
		delete(c.inflight, keyHash)
		close(inf.done)
		c.mu.Unlock()
	}()

	return p.waitInflight(ctx, inf, true, keyHash, req)
}

// waitInflight waits for a shared flight. Caller cancel only abandons this waiter;
// it does not cancel the shared provider call or other waiters.
// asLeader receives the real provider usage; additional waiters get zero-usage coalesced results.
func (p *CachingClassifierProvider) waitInflight(ctx context.Context, inf *exactInflight, asLeader bool, keyHash string, req ProviderRequest) (ProviderResult, error) {
	select {
	case <-ctx.Done():
		c := p.Cache
		c.mu.Lock()
		if cur, ok := c.inflight[keyHash]; ok && cur == inf {
			cur.wait--
		}
		c.mu.Unlock()
		return ProviderResult{}, ctx.Err()
	case <-inf.done:
		if inf.err != nil {
			return ProviderResult{}, inf.err
		}
		if asLeader {
			// Leader owns the shared provider call for usage accounting.
			return copyProviderResult(inf.res), nil
		}
		// Waiters did not own the provider call — zero usage + exact layer.
		return p.coalescedWaiterResult(inf.res, keyHash, req), nil
	}
}

func (p *CachingClassifierProvider) hitResult(hit CachedAssessment, keyHash string, req ProviderRequest) ProviderResult {
	a := copyRawAssessment(hit.Assessment)
	// Rebind host-owned fields to the current request (session/retry may change PromptHash).
	a.PromptHash = req.Prompt.PromptHash
	a.RulesetID = req.Input.RulesetID
	a.RulesetHash = req.Input.RulesetHash
	return ProviderResult{
		SchemaVersion: SchemaProviderResult,
		Assessment:    a,
		Usage: ProviderUsage{
			UsagePresent: true,
			CacheHit:     true,
			CacheBackend: ExactCacheLayerReinframeExact,
			CacheKeyHash: keyHash,
		},
		Meta: ProviderMeta{
			Provider:            p.Identity.ProviderKind,
			ModelID:             p.Identity.ModelID,
			ModelVersion:        p.Identity.ModelVersion,
			CapabilitiesProfile: p.Identity.CapabilitiesProfile,
			ProviderRequestID:   hit.SourceAuditID,
			ParseStatus:         ParseStatusOK,
			// Do not set FallbackReason — success path; layer is Usage.CacheBackend.
		},
	}
}

func (p *CachingClassifierProvider) coalescedWaiterResult(leader ProviderResult, keyHash string, req ProviderRequest) ProviderResult {
	a := copyRawAssessment(leader.Assessment)
	a.PromptHash = req.Prompt.PromptHash
	a.RulesetID = req.Input.RulesetID
	a.RulesetHash = req.Input.RulesetHash
	return ProviderResult{
		SchemaVersion: SchemaProviderResult,
		Assessment:    a,
		Usage: ProviderUsage{
			UsagePresent: true,
			CacheHit:     true,
			CacheBackend: ExactCacheLayerReinframeExact,
			CacheKeyHash: keyHash,
		},
		Meta: ProviderMeta{
			Provider:            p.Identity.ProviderKind,
			ModelID:             p.Identity.ModelID,
			ModelVersion:        p.Identity.ModelVersion,
			CapabilitiesProfile: p.Identity.CapabilitiesProfile,
			ProviderRequestID:   leader.Meta.ProviderRequestID,
			ParseStatus:         ParseStatusOK,
		},
	}
}

func copyRawAssessment(a RawAssessment) RawAssessment {
	out := a
	if a.EvidenceEventIDs != nil {
		out.EvidenceEventIDs = append([]string(nil), a.EvidenceEventIDs...)
	}
	return out
}

func copyProviderResult(r ProviderResult) ProviderResult {
	out := r
	out.Assessment = copyRawAssessment(r.Assessment)
	return out
}

func (p *CachingClassifierProvider) tryAdmit(keyHash string, res ProviderResult) {
	if res.Meta.ParseStatus != "" && res.Meta.ParseStatus != ParseStatusOK {
		p.Cache.mu.Lock()
		p.Cache.stats.Rejections++
		p.Cache.mu.Unlock()
		return
	}
	if res.Assessment.ParseStatus != "" && res.Assessment.ParseStatus != ParseStatusOK {
		p.Cache.mu.Lock()
		p.Cache.stats.Rejections++
		p.Cache.mu.Unlock()
		return
	}
	if res.Meta.ErrorClass != "" || res.Meta.FallbackReason != "" {
		p.Cache.mu.Lock()
		p.Cache.stats.Rejections++
		p.Cache.mu.Unlock()
		return
	}
	// Never re-admit an exact-hit echo (already layer-tagged).
	if res.Usage.CacheBackend == ExactCacheLayerReinframeExact {
		return
	}
	now := p.now()
	meta := res.Meta
	meta.FallbackReason = "" // never persist failure-class markers
	_ = p.Cache.Admit(keyHash, CachedAssessment{
		SchemaVersion:   SchemaCachedAssessment,
		Assessment:      copyRawAssessment(res.Assessment),
		SourceAuditID:   res.Meta.ProviderRequestID,
		ProviderMeta:    meta,
		CreatedAt:       now,
		ExpiresAt:       now.Add(p.Cache.cfg.TTL),
		CacheKeyVersion: SchemaExactCacheKey,
	})
}

func (p *CachingClassifierProvider) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	if p.Cache != nil && p.Cache.now != nil {
		return p.Cache.now()
	}
	return time.Now().UTC()
}
