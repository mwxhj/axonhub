package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math/rand/v2"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/anthropic/claudecode"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

const (
	stickySessionBindingTTL     = 5 * time.Minute
	stickyKeyVersion            = 1
	stickyMaxStableMessages     = 6
	stickyMaxMessageContentSize = 4096
)

type StickySessionBinding struct {
	ChannelID int
	ExpiresAt time.Time
}

type StickySessionStore interface {
	Get(key string) (int, bool)
	Bind(key string, channelID int)
	Delete(key string)
}

type StickySessionBindingStore struct {
	mu       sync.Mutex
	ttl      time.Duration
	now      func() time.Time
	bindings map[string]StickySessionBinding
}

func NewStickySessionBindingStore(ttl time.Duration) *StickySessionBindingStore {
	if ttl <= 0 {
		ttl = stickySessionBindingTTL
	}

	return &StickySessionBindingStore{
		ttl:      ttl,
		now:      time.Now,
		bindings: make(map[string]StickySessionBinding),
	}
}

func (s *StickySessionBindingStore) Get(key string) (int, bool) {
	if s == nil || key == "" {
		return 0, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[key]
	if !ok {
		return 0, false
	}

	if !binding.ExpiresAt.After(s.now()) {
		delete(s.bindings, key)
		return 0, false
	}

	return binding.ChannelID, true
}

func (s *StickySessionBindingStore) Bind(key string, channelID int) {
	if s == nil || key == "" || channelID == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bindings[key] = StickySessionBinding{
		ChannelID: channelID,
		ExpiresAt: s.now().Add(s.ttl),
	}
}

func (s *StickySessionBindingStore) Delete(key string) {
	if s == nil || key == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.bindings, key)
}

type StickyKeyExtraction struct {
	Key    string
	OK     bool
	Reason string
}

type StickyKeyExtractor interface {
	Extract(ctx context.Context, state *PersistenceState, req *llm.Request) StickyKeyExtraction
}

type DefaultStickyKeyExtractor struct{}

func NewDefaultStickyKeyExtractor() *DefaultStickyKeyExtractor {
	return &DefaultStickyKeyExtractor{}
}

type stickyKeyPayload struct {
	Version int                `json:"version"`
	Scope   stickyKeyScope     `json:"scope"`
	Request stickyKeyRequest   `json:"request"`
	Signals stickyKeySignals   `json:"signals"`
	Prefix  []stickyKeyMessage `json:"prefix,omitempty"`
	Tools   json.RawMessage    `json:"tools,omitempty"`
	Format  json.RawMessage    `json:"response_format,omitempty"`
	Choice  json.RawMessage    `json:"tool_choice,omitempty"`
	Extra   map[string]any     `json:"extra,omitempty"`
}

type stickyKeyScope struct {
	APIKeyID           int    `json:"api_key_id,omitempty"`
	ProjectID          int    `json:"project_id,omitempty"`
	APIKeyProfile      string `json:"api_key_profile,omitempty"`
	ProjectProfile     string `json:"project_profile,omitempty"`
	UnauthenticatedKey string `json:"unauthenticated_key,omitempty"`
}

type stickyKeyRequest struct {
	Model        string `json:"model"`
	RequestType  string `json:"request_type"`
	APIFormat    string `json:"api_format"`
	ClientFormat string `json:"client_format,omitempty"`
}

type stickyKeySignals struct {
	PreviousResponseID string `json:"previous_response_id,omitempty"`
	PromptCacheKey     string `json:"prompt_cache_key,omitempty"`
}

type stickyKeyMessage struct {
	Index   int    `json:"index"`
	Role    string `json:"role"`
	Name    string `json:"name,omitempty"`
	Content any    `json:"content,omitempty"`
}

func (e *DefaultStickyKeyExtractor) Extract(ctx context.Context, state *PersistenceState, req *llm.Request) StickyKeyExtraction {
	if req == nil {
		return StickyKeyExtraction{OK: false, Reason: "missing request"}
	}

	payload := stickyKeyPayload{
		Version: stickyKeyVersion,
		Scope:   stickyScopeFromState(state),
		Request: stickyKeyRequest{
			Model:        req.Model,
			RequestType:  stickyRequestType(req),
			APIFormat:    req.APIFormat.String(),
			ClientFormat: stickyClientFormat(req),
		},
		Signals: stickyKeySignals{
			PreviousResponseID: stringValue(req.PreviousResponseID),
			PromptCacheKey:     stringValue(req.PromptCacheKey),
		},
		Extra: map[string]any{},
	}

	if req.ParallelToolCalls != nil {
		payload.Extra["parallel_tool_calls"] = *req.ParallelToolCalls
	}

	if len(req.Tools) > 0 {
		tools := stableJSON(req.Tools)
		payload.Tools = tools
	}
	if format := stableJSON(req.ResponseFormat); len(format) > 0 && string(format) != "null" {
		payload.Format = format
	}
	if choice := stableJSON(req.ToolChoice); len(choice) > 0 && string(choice) != "null" {
		payload.Choice = choice
	}
	if len(payload.Extra) == 0 {
		payload.Extra = nil
	}

	prefixMessages, prefixReason := stablePrefixMessages(req.Messages)
	payload.Prefix = prefixMessages

	ok, reason := stickyPayloadSuitable(payload, prefixReason)
	if !ok {
		return StickyKeyExtraction{OK: false, Reason: reason}
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		log.Warn(ctx, "failed to marshal sticky key payload", log.Cause(err))
		return StickyKeyExtraction{OK: false, Reason: "canonicalization failed"}
	}

	sum := sha256.Sum256(encoded)
	return StickyKeyExtraction{
		Key:    "sticky:v1:" + hex.EncodeToString(sum[:16]),
		OK:     true,
		Reason: reason,
	}
}

func stickyScopeFromState(state *PersistenceState) stickyKeyScope {
	scope := stickyKeyScope{UnauthenticatedKey: "system"}
	if state == nil || state.APIKey == nil {
		return scope
	}

	apiKey := state.APIKey
	scope.UnauthenticatedKey = ""
	scope.APIKeyID = apiKey.ID
	scope.ProjectID = apiKey.ProjectID

	if apiKey.Profiles != nil {
		scope.APIKeyProfile = apiKey.Profiles.ActiveProfile
	}

	if project := apiKey.Edges.Project; project != nil && project.Profiles != nil {
		scope.ProjectProfile = project.Profiles.ActiveProfile
	}

	return scope
}

func stickyRequestType(req *llm.Request) string {
	if req.RequestType != "" {
		return req.RequestType.String()
	}

	return llm.RequestTypeChat.String()
}

func stickyClientFormat(req *llm.Request) string {
	if req == nil {
		return ""
	}

	if req.RawRequest != nil && req.RawRequest.Headers != nil {
		if req.RawRequest.Headers.Get(codex.SessionHeader) != "" ||
			req.RawRequest.Headers.Get(codex.TurnMetadataHeader) != "" ||
			req.RawRequest.Headers.Get(codex.WindowIDHeader) != "" {
			return "codex"
		}
	}

	if req.Metadata != nil {
		if uid := claudecode.ParseUserID(req.Metadata["user_id"]); uid != nil {
			return "claude-code"
		}
	}

	if req.RawRequest != nil {
		path := req.RawRequest.Path
		if path == "" && req.RawRequest.RawRequest != nil && req.RawRequest.RawRequest.URL != nil {
			path = req.RawRequest.RawRequest.URL.Path
		}
		if strings.Contains(path, "/anthropic/") {
			return "anthropic"
		}
	}

	return req.APIFormat.String()
}

func stablePrefixMessages(messages []llm.Message) ([]stickyKeyMessage, string) {
	if len(messages) == 0 {
		return nil, "no messages"
	}

	latestUserIndex := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if normalizeRole(messages[i].Role) == "user" {
			latestUserIndex = i
			break
		}
	}

	prefixEnd := len(messages)
	if latestUserIndex >= 0 {
		prefixEnd = latestUserIndex
	}

	result := make([]stickyKeyMessage, 0, stickyMaxStableMessages)
	seen := make(map[int]struct{})

	for i, msg := range messages {
		role := normalizeRole(msg.Role)
		if role != "system" && role != "developer" {
			continue
		}

		if content := stickyMessageContent(msg); content != nil {
			result = append(result, stickyKeyMessage{
				Index:   i,
				Role:    role,
				Name:    stringValue(msg.Name),
				Content: content,
			})
			seen[i] = struct{}{}
		}
	}

	for i := 0; i < prefixEnd && len(result) < stickyMaxStableMessages; i++ {
		if _, ok := seen[i]; ok {
			continue
		}

		msg := messages[i]
		role := normalizeRole(msg.Role)
		if role == "" {
			continue
		}

		if content := stickyMessageContent(msg); content != nil {
			result = append(result, stickyKeyMessage{
				Index:   i,
				Role:    role,
				Name:    stringValue(msg.Name),
				Content: content,
			})
		}
	}

	slices.SortFunc(result, func(a, b stickyKeyMessage) int {
		if a.Index < b.Index {
			return -1
		}
		if a.Index > b.Index {
			return 1
		}
		return 0
	})

	if len(result) == 0 {
		if latestUserIndex == 0 && len(messages) == 1 {
			return nil, "only latest user message"
		}
		return nil, "no stable prefix messages"
	}

	return result, "stable prefix"
}

func stickyMessageContent(msg llm.Message) any {
	if msg.Content.Content != nil {
		content := strings.TrimSpace(*msg.Content.Content)
		if content == "" {
			return nil
		}
		return truncateStickyContent(content)
	}

	if len(msg.Content.MultipleContent) == 0 {
		return nil
	}

	parts := make([]map[string]any, 0, len(msg.Content.MultipleContent))
	for _, part := range msg.Content.MultipleContent {
		item := map[string]any{"type": part.Type}
		switch {
		case part.Text != nil:
			text := strings.TrimSpace(*part.Text)
			if text == "" {
				continue
			}
			item["text"] = truncateStickyContent(text)
		case part.Compact != nil:
			item["compact_id"] = part.Compact.ID
			item["compact_created_by"] = stringValue(part.Compact.CreatedBy)
			if part.Compact.EncryptedContent != "" {
				item["compact_encrypted_content_hash"] = stickyContentHash(part.Compact.EncryptedContent)
			}
		case part.Document != nil:
			item["mime_type"] = part.Document.MIMEType
			if part.Document.URL != "" {
				item["url_hash"] = stickyContentHash(part.Document.URL)
			}
		case part.ImageURL != nil:
			item["detail"] = stringValue(part.ImageURL.Detail)
			if part.ImageURL.URL != "" {
				item["url_hash"] = stickyContentHash(part.ImageURL.URL)
			}
		case part.VideoURL != nil:
			if part.VideoURL.URL != "" {
				item["url_hash"] = stickyContentHash(part.VideoURL.URL)
			}
		case part.InputAudio != nil:
			item["format"] = part.InputAudio.Format
			if part.InputAudio.Data != "" {
				item["data_hash"] = stickyContentHash(part.InputAudio.Data)
			}
		default:
			continue
		}
		parts = append(parts, item)
	}

	if len(parts) == 0 {
		return nil
	}

	return parts
}

func stickyPayloadSuitable(payload stickyKeyPayload, prefixReason string) (bool, string) {
	if payload.Signals.PreviousResponseID != "" {
		return true, "previous response id"
	}
	if payload.Signals.PromptCacheKey != "" {
		return true, "prompt cache key"
	}
	if len(payload.Tools) > 0 {
		return true, "tools schema"
	}
	if len(payload.Format) > 0 {
		return true, "response format"
	}
	if len(payload.Choice) > 0 {
		return true, "tool choice"
	}

	hasSystemOrDeveloper := false
	prefixTextSize := 0
	for _, msg := range payload.Prefix {
		if msg.Role == "system" || msg.Role == "developer" {
			hasSystemOrDeveloper = true
		}
		prefixTextSize += stickyContentSize(msg.Content)
	}

	if hasSystemOrDeveloper && prefixTextSize > 0 {
		return true, "system or developer prompt"
	}

	if len(payload.Prefix) >= 2 && prefixTextSize >= 64 {
		return true, "early conversation prefix"
	}

	if prefixReason != "" {
		return false, prefixReason
	}

	return false, "insufficient stable context"
}

func stickyContentSize(content any) int {
	switch value := content.(type) {
	case string:
		return len(strings.TrimSpace(value))
	case []map[string]any:
		total := 0
		for _, item := range value {
			if text, ok := item["text"].(string); ok {
				total += len(strings.TrimSpace(text))
			}
		}
		return total
	default:
		return 0
	}
}

func truncateStickyContent(value string) string {
	if len(value) <= stickyMaxMessageContentSize {
		return value
	}

	return value[:stickyMaxMessageContentSize]
}

func stickyContentHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func normalizeRole(role string) string {
	return strings.ToLower(strings.TrimSpace(role))
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func stableJSON(value any) json.RawMessage {
	if value == nil {
		return nil
	}

	data, err := json.Marshal(value)
	if err != nil || len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil
	}

	var normalized any
	if err := json.Unmarshal(data, &normalized); err != nil {
		return json.RawMessage(data)
	}

	normalizedData, err := json.Marshal(normalized)
	if err != nil || len(normalizedData) == 0 || bytes.Equal(normalizedData, []byte("null")) {
		return json.RawMessage(data)
	}

	return json.RawMessage(normalizedData)
}

type StickySessionOrderRequest struct {
	Request      *llm.Request
	State        *PersistenceState
	Candidates   []*ChannelModelsCandidate
	LoadBalancer *LoadBalancer
}

type StickySessionRouter struct {
	store               StickySessionStore
	extractor           StickyKeyExtractor
	modelCircuitBreaker *biz.ModelCircuitBreaker
}

func NewStickySessionRouter(store StickySessionStore, extractor StickyKeyExtractor, modelCircuitBreaker ...*biz.ModelCircuitBreaker) *StickySessionRouter {
	var cb *biz.ModelCircuitBreaker
	if len(modelCircuitBreaker) > 0 {
		cb = modelCircuitBreaker[0]
	}

	return &StickySessionRouter{
		store:               store,
		extractor:           extractor,
		modelCircuitBreaker: cb,
	}
}

func (r *StickySessionRouter) Order(ctx context.Context, req StickySessionOrderRequest) []*ChannelModelsCandidate {
	if req.State != nil {
		req.State.StickyKey = ""
		req.State.StickyKeyOK = false
		req.State.StickyKeyReason = ""
	}

	if len(req.Candidates) == 0 || req.LoadBalancer == nil || req.Request == nil {
		return req.Candidates
	}

	if r == nil || r.store == nil || r.extractor == nil {
		return loadBalancedCandidates(ctx, req.Candidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	}

	extraction := r.extractor.Extract(ctx, req.State, req.Request)
	if req.State != nil {
		req.State.StickyKey = extraction.Key
		req.State.StickyKeyOK = extraction.OK
		req.State.StickyKeyReason = extraction.Reason
	}

	if !extraction.OK {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "sticky-session key unavailable",
				log.String("reason", extraction.Reason),
				log.String("model", req.Request.Model))
		}
		return loadBalancedCandidates(ctx, req.Candidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	}

	eligibleCandidates := r.eligibleCandidates(ctx, req)
	primary, excludedChannelID := r.boundCandidate(ctx, extraction.Key, req.Candidates, eligibleCandidates)
	source := "binding"
	if primary == nil {
		primary = randomBestTierCandidate(eligibleCandidates)
		source = "best-tier-random"
	}
	if primary == nil {
		return loadBalancedCandidates(ctx, req.Candidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	}

	ordered := stickyOrderedCandidates(ctx, req, primary, excludedChannelID)
	req.LoadBalancer.TrackSelection(ordered)

	if log.DebugEnabled(ctx) && len(ordered) > 0 && ordered[0] != nil && ordered[0].Channel != nil {
		log.Debug(ctx, "sticky-session ordered candidates",
			log.String("source", source),
			log.String("reason", extraction.Reason),
			log.Int("channel_id", ordered[0].Channel.ID),
			log.String("channel_name", ordered[0].Channel.Name),
			log.Int("candidate_count", len(ordered)))
	}

	return ordered
}

func (r *StickySessionRouter) boundCandidate(
	ctx context.Context,
	key string,
	candidates []*ChannelModelsCandidate,
	eligibleCandidates []*ChannelModelsCandidate,
) (*ChannelModelsCandidate, int) {
	channelID, ok := r.store.Get(key)
	if !ok {
		return nil, 0
	}

	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil || candidate.Channel.ID != channelID {
			continue
		}

		for _, eligible := range eligibleCandidates {
			if eligible != nil && eligible.Channel != nil && eligible.Channel.ID == channelID {
				return eligible, 0
			}
		}

		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "sticky-session binding skipped because channel is not currently eligible",
				log.Int("bound_channel_id", channelID))
		}

		return nil, channelID
	}

	r.store.Delete(key)
	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding ignored because channel is absent from candidates",
			log.Int("bound_channel_id", channelID))
	}

	return nil, 0
}

func (r *StickySessionRouter) eligibleCandidates(ctx context.Context, req StickySessionOrderRequest) []*ChannelModelsCandidate {
	if len(req.Candidates) == 0 || req.LoadBalancer == nil || req.Request == nil {
		return req.Candidates
	}

	useStream := req.Request.Stream != nil && *req.Request.Stream
	ctx = contextWithQuotaLimitType(ctx, string(provider_quota.RequestModality(req.Request.Image != nil)))

	result := make([]*ChannelModelsCandidate, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		if !req.LoadBalancer.IsStickyPrimaryEligible(ctx, candidate, req.Request.Model, useStream) {
			continue
		}
		if r != nil && r.modelCircuitBreaker != nil && stickyCircuitOpen(ctx, r.modelCircuitBreaker, req, candidate) {
			continue
		}
		result = append(result, candidate)
	}

	return result
}

func stickyCircuitOpen(ctx context.Context, cb *biz.ModelCircuitBreaker, req StickySessionOrderRequest, candidate *ChannelModelsCandidate) bool {
	if cb == nil || candidate == nil || candidate.Channel == nil {
		return false
	}

	modelID := stickyRequestedModel(req)
	if modelID == "" {
		return false
	}

	stats := cb.GetModelCircuitBreakerStats(ctx, candidate.Channel.ID, modelID)
	return stats != nil && stats.State == biz.StateOpen
}

func stickyRequestedModel(req StickySessionOrderRequest) string {
	if req.State != nil && req.State.OriginalModel != "" {
		return req.State.OriginalModel
	}
	if req.Request != nil {
		return req.Request.Model
	}

	return ""
}

func randomBestTierCandidate(candidates []*ChannelModelsCandidate) *ChannelModelsCandidate {
	bestPrioritySet := false
	bestPriority := 0
	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil {
			continue
		}
		if !bestPrioritySet || candidate.Priority < bestPriority {
			bestPrioritySet = true
			bestPriority = candidate.Priority
		}
	}
	if !bestPrioritySet {
		return nil
	}

	bestWeightSet := false
	bestWeight := 0
	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil || candidate.Priority != bestPriority {
			continue
		}
		if !bestWeightSet || candidate.Channel.OrderingWeight > bestWeight {
			bestWeightSet = true
			bestWeight = candidate.Channel.OrderingWeight
		}
	}
	if !bestWeightSet {
		return nil
	}

	tier := make([]*ChannelModelsCandidate, 0)
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil && candidate.Priority == bestPriority && candidate.Channel.OrderingWeight == bestWeight {
			tier = append(tier, candidate)
		}
	}
	if len(tier) == 0 {
		return nil
	}

	//nolint:gosec // Sticky first-bind distribution does not need cryptographic randomness.
	return tier[rand.IntN(len(tier))]
}

func stickyOrderedCandidates(
	ctx context.Context,
	req StickySessionOrderRequest,
	primary *ChannelModelsCandidate,
	excludedChannelID int,
) []*ChannelModelsCandidate {
	requiredCount := req.LoadBalancer.RequiredCandidateCount(ctx, req.Candidates)
	if requiredCount <= 0 {
		requiredCount = 1
	}

	result := []*ChannelModelsCandidate{primary}
	if requiredCount == 1 {
		return result
	}

	remaining := make([]*ChannelModelsCandidate, 0, len(req.Candidates)-1)
	for _, candidate := range req.Candidates {
		if candidate == nil || candidate.Channel == nil {
			continue
		}
		if primary != nil && primary.Channel != nil && candidate.Channel.ID == primary.Channel.ID {
			continue
		}
		if excludedChannelID != 0 && candidate.Channel.ID == excludedChannelID {
			continue
		}
		remaining = append(remaining, candidate)
	}

	if len(remaining) == 0 {
		return result
	}

	orderedRemaining := loadBalancedCandidatesWithoutTracking(ctx, remaining, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	for _, candidate := range orderedRemaining {
		if len(result) >= requiredCount {
			break
		}
		result = append(result, candidate)
	}

	return result
}

func stickyRetryPolicyProvider(req StickySessionOrderRequest) RetryPolicyProvider {
	if req.State != nil && req.State.RetryPolicyProvider != nil {
		return req.State.RetryPolicyProvider
	}
	if req.LoadBalancer != nil {
		return req.LoadBalancer.systemService
	}
	return nil
}

func withStickySessionBinding(outbound *PersistentOutboundTransformer, store StickySessionStore, strategy string) pipeline.Middleware {
	return &stickySessionBindingMiddleware{
		outbound: outbound,
		store:    store,
		strategy: strategy,
	}
}

type stickySessionBindingMiddleware struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	store    StickySessionStore
	strategy string
}

func (m *stickySessionBindingMiddleware) Name() string {
	return "sticky-session-binding"
}

func (m *stickySessionBindingMiddleware) OnInboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	m.bindCurrentChannel(ctx)
	return response, nil
}

func (m *stickySessionBindingMiddleware) OnInboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	if m.strategy != biz.LoadBalancerStrategyStickySession {
		return stream, nil
	}
	if m.outbound == nil || m.outbound.state == nil {
		return stream, nil
	}

	return &stickyInboundStream{
		ctx:    ctx,
		stream: stream,
		bind:   m.bindCurrentChannel,
		state:  m.outbound.state,
	}, nil
}

func (m *stickySessionBindingMiddleware) bindCurrentChannel(ctx context.Context) {
	if m == nil || m.strategy != biz.LoadBalancerStrategyStickySession || m.store == nil || m.outbound == nil || m.outbound.state == nil {
		return
	}

	state := m.outbound.state
	if !state.StickyKeyOK || state.StickyKey == "" {
		return
	}

	channel := m.outbound.GetCurrentChannel()
	if channel == nil {
		return
	}

	m.store.Bind(state.StickyKey, channel.ID)

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding refreshed",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("reason", state.StickyKeyReason))
	}
}

type stickyInboundStream struct {
	ctx    context.Context
	stream streams.Stream[*httpclient.StreamEvent]
	bind   func(context.Context)
	state  *PersistenceState
	bound  bool
}

func (s *stickyInboundStream) Next() bool {
	return s.stream.Next()
}

func (s *stickyInboundStream) Current() *httpclient.StreamEvent {
	return s.stream.Current()
}

func (s *stickyInboundStream) Err() error {
	return s.stream.Err()
}

func (s *stickyInboundStream) Close() error {
	err := s.stream.Close()
	if !s.bound && s.state != nil && s.state.StreamCompleted && stickyOriginalRequestStream(s.state) {
		s.bind(s.ctx)
		s.bound = true
	}
	return err
}

func stickyOriginalRequestStream(state *PersistenceState) bool {
	return state != nil && state.OriginalRequestStream != nil && *state.OriginalRequestStream
}
