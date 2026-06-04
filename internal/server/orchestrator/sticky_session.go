package orchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
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
	stickyMaxPrefixMessages     = 32
	stickyMaxMessageContentSize = 4096
)

const (
	stickyKindSession     = "session"
	stickyKindResponse    = "responses"
	stickyKindPromptCache = "prompt-cache"
	stickyKindPrefix      = "prefix"
	stickyKindLegacy      = "sticky"
)

const (
	stickyStrengthSession     = 100
	stickyStrengthResponse    = 90
	stickyStrengthPromptCache = 80
	stickyStrengthPrefix      = 40
	stickyMinPrefixTextSize   = 64
)

type StickySessionBinding struct {
	CredentialID          int
	ChannelID             int
	CredentialFingerprint string
	ExpiresAt             time.Time
}

type StickySessionTarget struct {
	CredentialID          int
	ChannelID             int
	CredentialFingerprint string
}

type StickySessionStore interface {
	Get(key string) (int, bool)
	Bind(key string, channelID int)
	Delete(key string)
}

type StickySessionTargetStore interface {
	GetTarget(key string) (StickySessionTarget, bool)
	BindTarget(key string, target StickySessionTarget)
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
	target, ok := s.GetTarget(key)
	if !ok {
		return 0, false
	}

	return target.ChannelID, true
}

func (s *StickySessionBindingStore) GetTarget(key string) (StickySessionTarget, bool) {
	if s == nil || key == "" {
		return StickySessionTarget{}, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	binding, ok := s.bindings[key]
	if !ok {
		return StickySessionTarget{}, false
	}

	if !binding.ExpiresAt.After(s.now()) {
		delete(s.bindings, key)
		return StickySessionTarget{}, false
	}

	return StickySessionTarget{
		CredentialID:          binding.CredentialID,
		ChannelID:             binding.ChannelID,
		CredentialFingerprint: binding.CredentialFingerprint,
	}, true
}

func (s *StickySessionBindingStore) Bind(key string, channelID int) {
	s.BindTarget(key, StickySessionTarget{ChannelID: channelID})
}

func (s *StickySessionBindingStore) BindTarget(key string, target StickySessionTarget) {
	if s == nil || key == "" || target.ChannelID == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.bindings[key] = StickySessionBinding{
		CredentialID:          target.CredentialID,
		ChannelID:             target.ChannelID,
		CredentialFingerprint: target.CredentialFingerprint,
		ExpiresAt:             s.now().Add(s.ttl),
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
	Key      string
	OK       bool
	Reason   string
	Lookups  []StickyLookup
	Bindings []StickyLookup
}

type StickyLookup struct {
	Key            string
	Kind           string
	Strength       int
	Reason         string
	PrefixMessages int
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
	Index      int    `json:"index"`
	Role       string `json:"role"`
	Name       string `json:"name,omitempty"`
	Content    any    `json:"content,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolCalls  any    `json:"tool_calls,omitempty"`
}

func (e *DefaultStickyKeyExtractor) Extract(ctx context.Context, state *PersistenceState, req *llm.Request) StickyKeyExtraction {
	_ = ctx

	if req == nil {
		return StickyKeyExtraction{OK: false, Reason: "missing request"}
	}

	payload := stickyBasePayload(state, req)
	lookups := make([]StickyLookup, 0, 8)
	bindings := make([]StickyLookup, 0, 8)

	if sessionID, reason := stickyProtocolSessionID(req); sessionID != "" {
		lookup := stickyValueLookup(stickyKindSession, sessionID, stickyStrengthSession, reason, payload)
		lookups = append(lookups, lookup)
		bindings = append(bindings, lookup)
	}

	if previousResponseID := stringValue(req.PreviousResponseID); previousResponseID != "" {
		lookup := stickyValueLookup(stickyKindResponse, previousResponseID, stickyStrengthResponse, "previous response id", payload)
		lookups = append(lookups, lookup)
		bindings = append(bindings, lookup)
	}

	if promptCacheKey := stringValue(req.PromptCacheKey); promptCacheKey != "" {
		lookup := stickyValueLookup(stickyKindPromptCache, promptCacheKey, stickyStrengthPromptCache, "prompt cache key", payload)
		lookups = append(lookups, lookup)
		bindings = append(bindings, lookup)
	}

	prefixLookups := stickyPrefixLookups(payload, req.Messages)
	lookups = append(lookups, prefixLookups...)

	lookups = uniqueStickyLookups(lookups)
	bindings = uniqueStickyLookups(bindings)
	if len(lookups) == 0 && !stickyHasPotentialCompletedPrefix(req.Messages) {
		return StickyKeyExtraction{OK: false, Reason: stickyNoPrefixReason(req.Messages)}
	}

	reason := stickyExtractionReason(lookups)
	if reason == "" {
		reason = "pending transcript prefix"
	}

	key := ""
	if len(lookups) > 0 {
		key = lookups[0].Key
	}

	return StickyKeyExtraction{
		Key:      key,
		OK:       true,
		Reason:   reason,
		Lookups:  lookups,
		Bindings: bindings,
	}
}

func stickyBasePayload(state *PersistenceState, req *llm.Request) stickyKeyPayload {
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
		payload.Tools = stableJSON(req.Tools)
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

	return payload
}

type stickyLookupPayload struct {
	Version int                `json:"version"`
	Kind    string             `json:"kind"`
	Scope   stickyKeyScope     `json:"scope"`
	Request stickyKeyRequest   `json:"request"`
	Value   string             `json:"value,omitempty"`
	Prefix  []stickyKeyMessage `json:"prefix,omitempty"`
	Tools   json.RawMessage    `json:"tools,omitempty"`
	Format  json.RawMessage    `json:"response_format,omitempty"`
	Choice  json.RawMessage    `json:"tool_choice,omitempty"`
	Extra   map[string]any     `json:"extra,omitempty"`
}

func stickyValueLookup(kind, value string, strength int, reason string, base stickyKeyPayload) StickyLookup {
	payload := stickyLookupPayload{
		Version: base.Version,
		Kind:    kind,
		Scope:   base.Scope,
		Request: base.Request,
		Value:   strings.TrimSpace(value),
		Tools:   base.Tools,
		Format:  base.Format,
		Choice:  base.Choice,
		Extra:   base.Extra,
	}

	return StickyLookup{
		Key:      stickyLookupKey(kind, payload),
		Kind:     kind,
		Strength: strength,
		Reason:   reason,
	}
}

func stickyPrefixLookup(base stickyKeyPayload, prefix []stickyKeyMessage, reason string) StickyLookup {
	payload := stickyLookupPayload{
		Version: base.Version,
		Kind:    stickyKindPrefix,
		Scope:   base.Scope,
		Request: base.Request,
		Prefix:  prefix,
		Tools:   base.Tools,
		Format:  base.Format,
		Choice:  base.Choice,
		Extra:   base.Extra,
	}

	return StickyLookup{
		Key:            stickyLookupKey(stickyKindPrefix, payload),
		Kind:           stickyKindPrefix,
		Strength:       stickyStrengthPrefix,
		Reason:         reason,
		PrefixMessages: len(prefix),
	}
}

func stickyLookupKey(kind string, payload stickyLookupPayload) string {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(encoded)
	if kind == "" {
		kind = stickyKindLegacy
	}

	return kind + ":v1:" + hex.EncodeToString(sum[:16])
}

func stickyProtocolSessionID(req *llm.Request) (string, string) {
	if req == nil {
		return "", ""
	}

	if req.RawRequest != nil && req.RawRequest.Headers != nil {
		if sessionID := codex.GetSessionIDFromHeaders(req.RawRequest.Headers); sessionID != "" {
			return sessionID, "codex session"
		}
		if windowID := strings.TrimSpace(req.RawRequest.Headers.Get(codex.WindowIDHeader)); windowID != "" {
			return windowID, "codex window"
		}
	}

	if req.Metadata != nil {
		if uid := claudecode.ParseUserID(req.Metadata["user_id"]); uid != nil && uid.SessionID != "" {
			return uid.SessionID, "claude-code session"
		}
	}

	return "", ""
}

func stickyPrefixLookups(base stickyKeyPayload, messages []llm.Message) []StickyLookup {
	prefixes := stickyCanonicalPrefixes(messages, false)
	result := make([]StickyLookup, 0, len(prefixes))
	for i := len(prefixes) - 1; i >= 0; i-- {
		prefix := prefixes[i]
		if !stickyPrefixSuitable(prefix) {
			continue
		}
		result = append(result, stickyPrefixLookup(base, prefix, "transcript prefix"))
	}
	return result
}

func stickyCompletedPrefixLookup(base stickyKeyPayload, messages []llm.Message) (StickyLookup, bool) {
	prefixes := stickyCanonicalPrefixes(messages, true)
	if len(prefixes) == 0 {
		return StickyLookup{}, false
	}
	prefix := prefixes[len(prefixes)-1]
	if !stickyPrefixSuitable(prefix) {
		return StickyLookup{}, false
	}
	return stickyPrefixLookup(base, prefix, "completed transcript prefix"), true
}

func stickyCanonicalPrefixes(messages []llm.Message, includeFull bool) [][]stickyKeyMessage {
	if len(messages) == 0 {
		return nil
	}

	limit := len(messages)
	if !includeFull && limit > 0 {
		limit--
	}
	if limit <= 0 {
		return nil
	}
	if limit > stickyMaxPrefixMessages {
		limit = stickyMaxPrefixMessages
	}

	result := make([][]stickyKeyMessage, 0, limit)
	current := make([]stickyKeyMessage, 0, limit)
	for i := 0; i < limit; i++ {
		msg := messages[i]
		role := normalizeRole(msg.Role)
		if role == "" {
			continue
		}
		content := stickyMessageContent(msg)
		if content == nil && len(msg.ToolCalls) == 0 && msg.ToolCallID == nil {
			continue
		}

		item := stickyKeyMessage{
			Index:      i,
			Role:       role,
			Name:       stringValue(msg.Name),
			Content:    content,
			ToolCallID: stringValue(msg.ToolCallID),
			ToolCalls:  stickyToolCalls(msg.ToolCalls),
		}
		current = append(current, item)
		copied := append([]stickyKeyMessage(nil), current...)
		result = append(result, copied)
	}

	return result
}

func stickyPrefixSuitable(prefix []stickyKeyMessage) bool {
	if len(prefix) == 0 {
		return false
	}

	hasSystemOrDeveloper := false
	textSize := 0
	for _, msg := range prefix {
		if msg.Role == "system" || msg.Role == "developer" {
			hasSystemOrDeveloper = true
		}
		textSize += stickyContentSize(msg.Content)
	}

	return (hasSystemOrDeveloper && textSize > 0) || textSize >= stickyMinPrefixTextSize
}

func stickyHasPotentialCompletedPrefix(messages []llm.Message) bool {
	if len(messages) == 0 {
		return false
	}
	prefixes := stickyCanonicalPrefixes(messages, true)
	if len(prefixes) == 0 {
		return false
	}
	return stickyPrefixSuitable(prefixes[len(prefixes)-1])
}

func stickyNoPrefixReason(messages []llm.Message) string {
	if len(messages) == 0 {
		return "no messages"
	}
	if len(messages) == 1 && normalizeRole(messages[0].Role) == "user" {
		return "only latest user message"
	}
	return "insufficient stable context"
}

func uniqueStickyLookups(lookups []StickyLookup) []StickyLookup {
	if len(lookups) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(lookups))
	result := make([]StickyLookup, 0, len(lookups))
	for _, lookup := range lookups {
		if lookup.Key == "" {
			continue
		}
		if _, ok := seen[lookup.Key]; ok {
			continue
		}
		seen[lookup.Key] = struct{}{}
		result = append(result, lookup)
	}
	return result
}

func stickyExtractionReason(lookups []StickyLookup) string {
	if len(lookups) == 0 {
		return ""
	}
	return lookups[0].Reason
}

func stickyNormalizeExtraction(extraction StickyKeyExtraction) StickyKeyExtraction {
	if !extraction.OK {
		return extraction
	}

	if len(extraction.Lookups) == 0 && extraction.Key != "" {
		lookup := StickyLookup{
			Key:      extraction.Key,
			Kind:     stickyKindLegacy,
			Strength: stickyStrengthPrefix,
			Reason:   extraction.Reason,
		}
		extraction.Lookups = []StickyLookup{lookup}
		if len(extraction.Bindings) == 0 {
			extraction.Bindings = []StickyLookup{lookup}
		}
	}
	if extraction.Key == "" && len(extraction.Lookups) > 0 {
		extraction.Key = extraction.Lookups[0].Key
	}
	if extraction.Reason == "" {
		extraction.Reason = stickyExtractionReason(extraction.Lookups)
	}
	extraction.Lookups = uniqueStickyLookups(extraction.Lookups)
	extraction.Bindings = uniqueStickyLookups(extraction.Bindings)
	return extraction
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

func stickyToolCalls(toolCalls []llm.ToolCall) any {
	if len(toolCalls) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(toolCalls))
	for _, toolCall := range toolCalls {
		item := map[string]any{
			"id":   toolCall.ID,
			"type": toolCall.Type,
		}
		if toolCall.Function.Name != "" {
			item["function_name"] = toolCall.Function.Name
		}
		if toolCall.Function.Arguments != "" {
			item["arguments_hash"] = stickyContentHash(toolCall.Function.Arguments)
		}
		if toolCall.ResponseCustomToolCall != nil {
			item["custom_call_id"] = toolCall.ResponseCustomToolCall.CallID
			item["custom_name"] = toolCall.ResponseCustomToolCall.Name
			if toolCall.ResponseCustomToolCall.Input != "" {
				item["custom_input_hash"] = stickyContentHash(toolCall.ResponseCustomToolCall.Input)
			}
		}
		result = append(result, item)
	}
	return result
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
	store     StickySessionStore
	extractor StickyKeyExtractor
}

const (
	stickyRoutingSourceBinding                     = "binding-hit"
	stickyRoutingSourceQuotaRatioFirstBind         = "rebind-by-quota-ratio"
	stickyRoutingSourceLoadBalancedNoCandidates    = "rebind-degraded-no-candidate"
	stickyRoutingSourceLoadBalancedNoComparableKey = "rebind-degraded-normal-lb"

	stickyRoutingDegradeNone              = ""
	stickyRoutingDegradeNoComparableQuota = "no-comparable-local-quota"
)

func NewStickySessionRouter(store StickySessionStore, extractor StickyKeyExtractor) *StickySessionRouter {
	return &StickySessionRouter{
		store:     store,
		extractor: extractor,
	}
}

func (r *StickySessionRouter) Order(ctx context.Context, req StickySessionOrderRequest) []*ChannelModelsCandidate {
	if req.State != nil {
		req.State.StickyKey = ""
		req.State.StickyKeyOK = false
		req.State.StickyKeyReason = ""
		req.State.StickyLookups = nil
		req.State.StickyBindings = nil
		req.State.StickyBasePayload = stickyKeyPayload{}
		req.State.StickyBasePayloadOK = false
		req.State.StickyResponseID = ""
		req.State.StickyPreviousResponseID = ""
		req.State.StickyResponseMessage = nil
		req.State.StickyRoutingSource = ""
		req.State.StickyRoutingDegradeReason = ""
		req.State.PreferredCredentialID = 0
		req.State.PreferredCredentialFingerprint = ""
	}

	if len(req.Candidates) == 0 || req.LoadBalancer == nil || req.Request == nil {
		return req.Candidates
	}

	if r == nil || r.store == nil || r.extractor == nil {
		return loadBalancedCandidates(ctx, req.Candidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	}

	extraction := stickyNormalizeExtraction(r.extractor.Extract(ctx, req.State, req.Request))
	if req.State != nil {
		req.State.StickyKey = extraction.Key
		req.State.StickyKeyOK = extraction.OK
		req.State.StickyKeyReason = extraction.Reason
		req.State.StickyLookups = extraction.Lookups
		req.State.StickyBindings = extraction.Bindings
		req.State.StickyBasePayload = stickyBasePayload(req.State, req.Request)
		req.State.StickyBasePayloadOK = extraction.OK
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
	primary, excludedChannelID, boundLookup, preferredTarget := r.boundCandidateForLookups(ctx, extraction.Lookups, req.Candidates, eligibleCandidates)
	source := stickyRoutingSourceBinding
	degradeReason := stickyRoutingDegradeNone
	if primary == nil {
		primary, preferredTarget, source, degradeReason = stickyFirstBindCandidate(ctx, req, eligibleCandidates, extraction.Key)
	}
	if primary == nil {
		return loadBalancedCandidates(ctx, req.Candidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	}

	ordered := stickyOrderedCandidates(ctx, req, primary, excludedChannelID)
	if req.State != nil {
		req.State.StickyRoutingSource = source
		req.State.StickyRoutingDegradeReason = degradeReason
		req.State.PreferredCredentialID = preferredTarget.CredentialID
		req.State.PreferredCredentialFingerprint = preferredTarget.CredentialFingerprint
	}
	req.LoadBalancer.TrackSelection(ordered)

	if log.DebugEnabled(ctx) && len(ordered) > 0 && ordered[0] != nil && ordered[0].Channel != nil {
		reason := extraction.Reason
		if boundLookup.Reason != "" {
			reason = boundLookup.Reason
		}
		log.Debug(ctx, "sticky-session ordered candidates",
			log.String("source", source),
			log.String("degrade_reason", degradeReason),
			log.String("reason", reason),
			log.String("sticky_kind", boundLookup.Kind),
			log.Int("channel_id", ordered[0].Channel.ID),
			log.String("channel_name", ordered[0].Channel.Name),
			log.Int("candidate_count", len(ordered)))
	}

	return ordered
}

func (r *StickySessionRouter) boundCandidateForLookups(
	ctx context.Context,
	lookups []StickyLookup,
	candidates []*ChannelModelsCandidate,
	eligibleCandidates []*ChannelModelsCandidate,
) (*ChannelModelsCandidate, int, StickyLookup, StickySessionTarget) {
	excludedChannelID := 0
	var excludedTarget StickySessionTarget
	for _, lookup := range lookups {
		if lookup.Key == "" {
			continue
		}
		primary, excluded, preferredTarget := r.boundCandidate(ctx, lookup.Key, candidates, eligibleCandidates)
		if primary != nil {
			return primary, excludedChannelID, lookup, preferredTarget
		}
		if excluded != 0 && excludedChannelID == 0 {
			excludedChannelID = excluded
			excludedTarget = preferredTarget
		}
	}

	return nil, excludedChannelID, StickyLookup{}, excludedTarget
}

func (r *StickySessionRouter) boundCandidate(
	ctx context.Context,
	key string,
	candidates []*ChannelModelsCandidate,
	eligibleCandidates []*ChannelModelsCandidate,
) (*ChannelModelsCandidate, int, StickySessionTarget) {
	target, ok := stickyStoreGetTarget(r.store, key)
	if !ok {
		return nil, 0, StickySessionTarget{}
	}

	primaryTier := stickyBestPriorityCandidates(eligibleCandidates)
	channelID := target.ChannelID
	boundChannelPresent := false
	boundChannelInPrimaryTier := false

	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil || candidate.Channel.ID != channelID {
			continue
		}
		boundChannelPresent = true

		for _, eligible := range primaryTier {
			if eligible != nil && eligible.Channel != nil && eligible.Channel.ID == channelID && stickyCandidateMatchesTarget(eligible, target) {
				return eligible, 0, target
			}
			if eligible != nil && eligible.Channel != nil && eligible.Channel.ID == channelID {
				boundChannelInPrimaryTier = true
			}
		}

		break
	}

	if target.CredentialID > 0 || target.CredentialFingerprint != "" {
		for _, eligible := range primaryTier {
			if stickyCandidateMatchesTarget(eligible, target) {
				if log.DebugEnabled(ctx) {
					log.Debug(ctx, "sticky-session binding kept credential on same priority channel",
						log.Int("bound_channel_id", channelID),
						log.Int("selected_channel_id", eligible.Channel.ID),
						log.String("credential_fingerprint", target.CredentialFingerprint))
				}

				return eligible, 0, target
			}
		}
	}

	if boundChannelPresent {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "sticky-session binding skipped because channel is not currently eligible in the active priority tier",
				log.Int("bound_channel_id", channelID),
				log.String("credential_fingerprint", target.CredentialFingerprint))
		}
		if boundChannelInPrimaryTier {
			return nil, channelID, target
		}
		return nil, 0, target
	}

	r.store.Delete(key)
	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding ignored because channel is absent from candidates",
			log.Int("bound_channel_id", channelID))
	}

	return nil, 0, StickySessionTarget{}
}

func stickyStoreGetTarget(store StickySessionStore, key string) (StickySessionTarget, bool) {
	if targetStore, ok := store.(StickySessionTargetStore); ok {
		return targetStore.GetTarget(key)
	}

	channelID, ok := store.Get(key)
	if !ok {
		return StickySessionTarget{}, false
	}

	return StickySessionTarget{ChannelID: channelID}, true
}

func stickyCandidateMatchesTarget(candidate *ChannelModelsCandidate, target StickySessionTarget) bool {
	if candidate == nil || candidate.Channel == nil {
		return false
	}

	if target.CredentialID > 0 && candidate.Channel.HasEnabledCredentialID(target.CredentialID) {
		return true
	}

	if target.CredentialFingerprint == "" {
		return true
	}

	return candidate.Channel.HasEnabledCredentialFingerprint(target.CredentialFingerprint)
}

func stickyBestPriorityCandidates(candidates []*ChannelModelsCandidate) []*ChannelModelsCandidate {
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

	result := make([]*ChannelModelsCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate != nil && candidate.Channel != nil && candidate.Priority == bestPriority {
			result = append(result, candidate)
		}
	}

	return result
}

func (r *StickySessionRouter) eligibleCandidates(ctx context.Context, req StickySessionOrderRequest) []*ChannelModelsCandidate {
	if len(req.Candidates) == 0 || req.LoadBalancer == nil || req.Request == nil {
		return req.Candidates
	}

	useStream := req.Request.Stream != nil && *req.Request.Stream

	result := make([]*ChannelModelsCandidate, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		if !req.LoadBalancer.IsStickyPrimaryEligible(ctx, candidate, req.Request.Model, useStream) {
			continue
		}
		result = append(result, candidate)
	}

	return result
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

func stickyFirstBindCandidate(
	ctx context.Context,
	req StickySessionOrderRequest,
	eligibleCandidates []*ChannelModelsCandidate,
	stickyKey string,
) (*ChannelModelsCandidate, StickySessionTarget, string, string) {
	ordered := loadBalancedCandidatesWithoutTracking(ctx, eligibleCandidates, req.Request, req.LoadBalancer, stickyRetryPolicyProvider(req))
	if len(ordered) == 0 || ordered[0] == nil {
		return nil, StickySessionTarget{}, stickyRoutingSourceLoadBalancedNoCandidates, stickyRoutingDegradeNone
	}

	primary := ordered[0]
	baseTarget := stickyCandidatePreferredTarget(primary, stickyKey)
	if balanced, target, ok := stickyQuotaRatioFirstBindCandidate(primary, baseTarget, eligibleCandidates, stickyKey); ok {
		return balanced, target, stickyRoutingSourceQuotaRatioFirstBind, stickyRoutingDegradeNone
	}

	return primary, StickySessionTarget{}, stickyRoutingSourceLoadBalancedNoComparableKey, stickyRoutingDegradeNoComparableQuota
}

func stickyQuotaRatioFirstBindCandidate(
	primary *ChannelModelsCandidate,
	preferredTarget StickySessionTarget,
	eligibleCandidates []*ChannelModelsCandidate,
	stickyKey string,
) (*ChannelModelsCandidate, StickySessionTarget, bool) {
	now := time.Now()
	if !stickyCandidateHasComparableLocalQuota(primary, preferredTarget, now) {
		return nil, StickySessionTarget{}, false
	}

	type scopeChoice struct {
		candidate *ChannelModelsCandidate
		view      biz.ChannelCredentialView
		ratio     decimal.Decimal
	}

	choices := make([]scopeChoice, 0, len(eligibleCandidates))
	seenScopes := make(map[int]struct{}, len(eligibleCandidates))
	for _, candidate := range eligibleCandidates {
		if candidate == nil || candidate.Channel == nil || primary == nil || candidate.Priority != primary.Priority {
			continue
		}
		for _, view := range stickyEnabledCredentialViews(candidate) {
			ratio, ok := stickyLocalQuotaRatio(view, now)
			if !ok {
				continue
			}
			if _, exists := seenScopes[view.QuotaScopeID]; exists {
				continue
			}
			seenScopes[view.QuotaScopeID] = struct{}{}
			choices = append(choices, scopeChoice{
				candidate: candidate,
				view:      stickyConcreteViewForScope(candidate, view.QuotaScopeID, stickyKey),
				ratio:     ratio,
			})
		}
	}
	if len(choices) == 0 {
		return nil, StickySessionTarget{}, false
	}

	best := choices[0]
	for _, choice := range choices[1:] {
		if choice.ratio.Cmp(best.ratio) < 0 {
			best = choice
		}
	}

	return best.candidate, stickyTargetFromCredentialView(best.candidate, best.view), true
}

func stickyCandidateHasComparableLocalQuota(
	candidate *ChannelModelsCandidate,
	preferredTarget StickySessionTarget,
	now time.Time,
) bool {
	if view, ok := stickyTargetCredentialView(candidate, preferredTarget); ok {
		if _, ok := stickyLocalQuotaRatio(view, now); ok {
			return true
		}
	}

	for _, view := range stickyEnabledCredentialViews(candidate) {
		if _, ok := stickyLocalQuotaRatio(view, now); ok {
			return true
		}
	}

	return false
}

func stickyCandidatePreferredTarget(candidate *ChannelModelsCandidate, stickyKey string) StickySessionTarget {
	view, ok := stickySeededCredentialView(candidate, stickyKey)
	if !ok {
		if candidate == nil || candidate.Channel == nil {
			return StickySessionTarget{}
		}
		return StickySessionTarget{ChannelID: candidate.Channel.ID}
	}

	return stickyTargetFromCredentialView(candidate, view)
}

func stickyTargetCredentialView(candidate *ChannelModelsCandidate, target StickySessionTarget) (biz.ChannelCredentialView, bool) {
	views := stickyEnabledCredentialViews(candidate)
	if len(views) == 0 {
		return biz.ChannelCredentialView{}, false
	}

	for _, view := range views {
		if target.CredentialID > 0 && view.CredentialID == target.CredentialID {
			return view, true
		}
		if target.CredentialFingerprint != "" && view.Fingerprint == target.CredentialFingerprint {
			return view, true
		}
	}

	if len(views) == 1 {
		return views[0], true
	}

	return biz.ChannelCredentialView{}, false
}

func stickyConcreteViewForScope(candidate *ChannelModelsCandidate, quotaScopeID int, stickyKey string) biz.ChannelCredentialView {
	views := stickyEnabledCredentialViews(candidate)
	matching := make([]biz.ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if view.QuotaScopeID == quotaScopeID {
			matching = append(matching, view)
		}
	}

	if len(matching) == 0 {
		return biz.ChannelCredentialView{}
	}
	if len(matching) == 1 {
		return matching[0]
	}

	if stickyKey == "" {
		return matching[0]
	}
	if selected, ok := biz.SelectCredentialViewBySeed(matching, "sticky:"+stickyKey); ok {
		return selected
	}

	return matching[0]
}

func stickySeededCredentialView(candidate *ChannelModelsCandidate, stickyKey string) (biz.ChannelCredentialView, bool) {
	views := stickyEnabledCredentialViews(candidate)
	if len(views) == 0 {
		return biz.ChannelCredentialView{}, false
	}
	if len(views) == 1 {
		return views[0], true
	}
	if stickyKey == "" {
		return biz.ChannelCredentialView{}, false
	}

	return biz.SelectCredentialViewBySeed(views, "sticky:"+stickyKey)
}

func stickyEnabledCredentialViews(candidate *ChannelModelsCandidate) []biz.ChannelCredentialView {
	if candidate == nil || candidate.Channel == nil {
		return nil
	}

	views := candidate.Channel.CredentialViews()
	result := make([]biz.ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if view.Enabled {
			result = append(result, view)
		}
	}

	return result
}

func stickyTargetFromCredentialView(candidate *ChannelModelsCandidate, view biz.ChannelCredentialView) StickySessionTarget {
	target := StickySessionTarget{}
	if candidate != nil && candidate.Channel != nil {
		target.ChannelID = candidate.Channel.ID
	}
	target.CredentialID = view.CredentialID
	target.CredentialFingerprint = view.Fingerprint
	return target
}

func stickyLocalQuotaRatio(view biz.ChannelCredentialView, now time.Time) (decimal.Decimal, bool) {
	if view.QuotaScopeID <= 0 || view.QuotaScopeAutoResetDue(now) {
		return decimal.Decimal{}, false
	}

	source := strings.ToLower(strings.TrimSpace(view.QuotaScopeSource))
	if source != "local_budget" {
		return decimal.Decimal{}, false
	}

	unit := strings.ToLower(strings.TrimSpace(view.QuotaScopeUnit))
	if unit == "" || unit == "unknown" {
		return decimal.Decimal{}, false
	}

	resetPolicy := strings.ToLower(strings.TrimSpace(view.QuotaScopeResetPolicy))
	if resetPolicy != "daily" {
		return decimal.Decimal{}, false
	}
	if view.QuotaScopeResetAt == nil || !view.QuotaScopeResetAt.After(now) {
		return decimal.Decimal{}, false
	}

	limit, err := decimal.NewFromString(strings.TrimSpace(view.QuotaScopeLimitAmount))
	if err != nil || !limit.GreaterThan(decimal.Zero) {
		return decimal.Decimal{}, false
	}

	used, err := decimal.NewFromString(strings.TrimSpace(view.QuotaScopeUsedAmount))
	if err != nil || used.IsNegative() {
		return decimal.Decimal{}, false
	}

	return used.Div(limit), true
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

func (m *stickySessionBindingMiddleware) OnOutboundLlmResponse(ctx context.Context, response *llm.Response) (*llm.Response, error) {
	if m.strategy != biz.LoadBalancerStrategyStickySession {
		return response, nil
	}
	m.captureLlmResponse(response)
	return response, nil
}

func (m *stickySessionBindingMiddleware) OnOutboundLlmStream(ctx context.Context, stream streams.Stream[*llm.Response]) (streams.Stream[*llm.Response], error) {
	if m.strategy != biz.LoadBalancerStrategyStickySession {
		return stream, nil
	}
	if m.outbound == nil || m.outbound.state == nil {
		return stream, nil
	}

	return &stickyLlmStream{
		stream:  stream,
		capture: m.captureLlmResponse,
		state:   m.outbound.state,
	}, nil
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
	if !state.StickyKeyOK {
		return
	}

	channel := m.outbound.GetCurrentChannel()
	if channel == nil {
		return
	}

	bindings := stickyBindingAliases(state)
	if len(bindings) == 0 {
		return
	}

	for _, binding := range bindings {
		target := StickySessionTarget{
			CredentialID:          state.CurrentCredentialID,
			ChannelID:             channel.ID,
			CredentialFingerprint: state.CurrentCredentialFingerprint,
		}
		if targetStore, ok := m.store.(StickySessionTargetStore); ok {
			targetStore.BindTarget(binding.Key, target)
		} else {
			m.store.Bind(binding.Key, channel.ID)
		}
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding refreshed",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("reason", state.StickyKeyReason),
			log.Int("alias_count", len(bindings)))
	}
}

func (m *stickySessionBindingMiddleware) captureLlmResponse(response *llm.Response) {
	if m == nil || m.outbound == nil || m.outbound.state == nil || response == nil {
		return
	}

	state := m.outbound.state
	if response.ID != "" {
		state.StickyResponseID = response.ID
	}
	if response.PreviousResponseID != nil && *response.PreviousResponseID != "" {
		state.StickyPreviousResponseID = *response.PreviousResponseID
	}
	if msg := stickyAssistantMessageFromResponse(response); msg != nil {
		state.StickyResponseMessage = msg
	}
}

func stickyAssistantMessageFromResponse(response *llm.Response) *llm.Message {
	if response == nil {
		return nil
	}
	for _, choice := range response.Choices {
		if choice.Message != nil {
			msg := *choice.Message
			if normalizeRole(msg.Role) == "" {
				msg.Role = "assistant"
			}
			return &msg
		}
	}
	return nil
}

func stickyBindingAliases(state *PersistenceState) []StickyLookup {
	if state == nil || state.LlmRequest == nil {
		if state == nil {
			return nil
		}
		return stickyBindingsWithLegacyFallback(state.StickyBindings, state.StickyKey, state.StickyKeyReason)
	}

	base := stickyBindingBasePayload(state)
	aliases := make([]StickyLookup, 0, len(state.StickyBindings)+4)
	aliases = append(aliases, state.StickyBindings...)
	if len(aliases) == 0 && state.StickyKey != "" {
		aliases = append(aliases, stickyLegacyLookup(state.StickyKey, state.StickyKeyReason))
	}

	if state.StickyResponseID != "" {
		aliases = append(aliases, stickyValueLookup(stickyKindResponse, state.StickyResponseID, stickyStrengthResponse, "response id", base))
	}
	if state.StickyPreviousResponseID != "" {
		aliases = append(aliases, stickyValueLookup(stickyKindResponse, state.StickyPreviousResponseID, stickyStrengthResponse, "previous response id refresh", base))
	}
	if lookup, ok := stickyCompletedPrefixLookup(base, state.LlmRequest.Messages); ok {
		aliases = append(aliases, lookup)
	}
	if completed := stickyCompletedMessages(state); len(completed) > 0 {
		if lookup, ok := stickyCompletedPrefixLookup(base, completed); ok {
			aliases = append(aliases, lookup)
		}
	}

	return uniqueStickyLookups(aliases)
}

func stickyBindingBasePayload(state *PersistenceState) stickyKeyPayload {
	if state == nil {
		return stickyKeyPayload{}
	}
	if state.StickyBasePayloadOK {
		return state.StickyBasePayload
	}
	if state.LlmRequest != nil {
		return stickyBasePayload(state, state.LlmRequest)
	}
	return stickyKeyPayload{}
}

func stickyBindingsWithLegacyFallback(bindings []StickyLookup, key, reason string) []StickyLookup {
	if len(bindings) == 0 && key != "" {
		bindings = append(bindings, stickyLegacyLookup(key, reason))
	}
	return uniqueStickyLookups(bindings)
}

func stickyLegacyLookup(key, reason string) StickyLookup {
	return StickyLookup{
		Key:      key,
		Kind:     stickyKindLegacy,
		Strength: stickyStrengthPrefix,
		Reason:   reason,
	}
}

func stickyCompletedMessages(state *PersistenceState) []llm.Message {
	if state == nil || state.LlmRequest == nil || state.StickyResponseMessage == nil {
		return nil
	}
	messages := append([]llm.Message(nil), state.LlmRequest.Messages...)
	messages = append(messages, *state.StickyResponseMessage)
	return messages
}

type stickyLlmStream struct {
	stream streams.Stream[*llm.Response]
	state  *PersistenceState

	capture func(*llm.Response)
	role    string
	text    strings.Builder
}

func (s *stickyLlmStream) Next() bool {
	return s.stream.Next()
}

func (s *stickyLlmStream) Current() *llm.Response {
	response := s.stream.Current()
	if s.capture != nil && response != nil {
		s.capture(response)
	}
	s.captureDelta(response)
	return response
}

func (s *stickyLlmStream) Err() error {
	return s.stream.Err()
}

func (s *stickyLlmStream) Close() error {
	return s.stream.Close()
}

func (s *stickyLlmStream) captureDelta(response *llm.Response) {
	if s == nil || s.state == nil || response == nil {
		return
	}

	for _, choice := range response.Choices {
		if choice.Delta != nil {
			delta := choice.Delta
			if role := normalizeRole(delta.Role); role != "" {
				s.role = role
			}
			if delta.Content.Content != nil {
				s.text.WriteString(*delta.Content.Content)
			}
			for _, part := range delta.Content.MultipleContent {
				if part.Text != nil {
					s.text.WriteString(*part.Text)
				}
			}
		}
		if choice.FinishReason != nil {
			s.setCompletedMessage()
		}
	}
}

func (s *stickyLlmStream) setCompletedMessage() {
	if s == nil || s.state == nil || s.state.StickyResponseMessage != nil {
		return
	}

	content := strings.TrimSpace(s.text.String())
	if content == "" {
		return
	}
	role := s.role
	if role == "" {
		role = "assistant"
	}
	s.state.StickyResponseMessage = &llm.Message{
		Role:    role,
		Content: llm.MessageContent{Content: &content},
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
