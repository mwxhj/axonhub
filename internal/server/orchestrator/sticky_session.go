package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

const (
	stickySessionBindingTTL = 5 * time.Minute
	stickyKeyVersion        = 2
	stickyKindAPIKey        = "api-key"
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

type stickyKeyScope struct {
	APIKeyID  int `json:"api_key_id,omitempty"`
	ProjectID int `json:"project_id,omitempty"`
}

type stickyLookupPayload struct {
	Version int            `json:"version"`
	Kind    string         `json:"kind"`
	Scope   stickyKeyScope `json:"scope"`
}

func (e *DefaultStickyKeyExtractor) Extract(ctx context.Context, state *PersistenceState, req *llm.Request) StickyKeyExtraction {
	_ = ctx
	if req == nil {
		return StickyKeyExtraction{OK: false, Reason: "missing request"}
	}

	scope := stickyScopeFromState(state)
	if scope.APIKeyID == 0 {
		return StickyKeyExtraction{OK: false, Reason: "missing api key identity"}
	}

	return StickyKeyExtraction{
		Key:    stickyAPIKeyKey(scope),
		OK:     true,
		Reason: "api key sticky",
	}
}

func stickyAPIKeyKey(scope stickyKeyScope) string {
	payload := stickyLookupPayload{
		Version: stickyKeyVersion,
		Kind:    stickyKindAPIKey,
		Scope:   scope,
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(encoded)
	return stickyKindAPIKey + ":v2:" + hex.EncodeToString(sum[:16])
}

func stickyNormalizeExtraction(extraction StickyKeyExtraction) StickyKeyExtraction {
	if !extraction.OK {
		return extraction
	}
	if extraction.Reason == "" {
		extraction.Reason = "api key sticky"
	}
	if extraction.Key == "" {
		extraction.OK = false
		extraction.Reason = "missing sticky key"
	}
	return extraction
}

func stickyScopeFromState(state *PersistenceState) stickyKeyScope {
	scope := stickyKeyScope{}
	if state == nil || state.APIKey == nil {
		return scope
	}

	apiKey := state.APIKey
	scope.APIKeyID = apiKey.ID
	scope.ProjectID = apiKey.ProjectID
	return scope
}

type StickySessionOrderRequest struct {
	Request    *llm.Request
	State      *PersistenceState
	Candidates []*ChannelModelsCandidate
}

type StickySessionRouter struct {
	store     StickySessionStore
	extractor StickyKeyExtractor
}

const (
	stickyRoutingSourceBinding         = "binding-hit"
	stickyRoutingSourceNormalFirstBind = "normal-first-bind"
	stickyRoutingSourceNoCandidates    = "normal-first-bind-no-candidate"

	stickyRoutingDegradeNone = ""
)

func NewStickySessionRouter(store StickySessionStore, extractor StickyKeyExtractor) *StickySessionRouter {
	return &StickySessionRouter{
		store:     store,
		extractor: extractor,
	}
}

func (r *StickySessionRouter) Order(ctx context.Context, req StickySessionOrderRequest) []*ChannelModelsCandidate {
	resetStickyRoutingState(req.State)
	if len(req.Candidates) == 0 || req.Request == nil || r == nil || r.store == nil || r.extractor == nil {
		return req.Candidates
	}

	extraction := stickyNormalizeExtraction(r.extractor.Extract(ctx, req.State, req.Request))
	applyStickyExtraction(req.State, extraction)
	if !extraction.OK {
		if log.DebugEnabled(ctx) {
			log.Debug(ctx, "sticky-session key unavailable",
				log.String("reason", extraction.Reason),
				log.String("model", req.Request.Model))
		}
		return req.Candidates
	}

	primary, preferredTarget := r.boundCandidateForKey(ctx, extraction.Key, req.Candidates)
	source := stickyRoutingSourceBinding
	degradeReason := stickyRoutingDegradeNone
	if primary == nil {
		primary, preferredTarget, source, degradeReason = stickyFirstBindCandidate(ctx, req.Candidates, extraction.Key)
	}
	if primary == nil {
		return req.Candidates
	}

	ordered := stickyOrderedCandidates(req, primary)
	applyStickyPrimary(req.State, source, degradeReason, preferredTarget)
	logStickyOrder(ctx, req.Request, ordered, extraction, source, degradeReason)
	return ordered
}

func resetStickyRoutingState(state *PersistenceState) {
	if state == nil {
		return
	}

	state.StickyKey = ""
	state.StickyKeyOK = false
	state.StickyKeyReason = ""
	state.StickyRoutingSource = ""
	state.StickyRoutingDegradeReason = ""
	state.PreferredCredentialID = 0
	state.PreferredCredentialFingerprint = ""
}

func applyStickyExtraction(state *PersistenceState, extraction StickyKeyExtraction) {
	if state == nil {
		return
	}

	state.StickyKey = extraction.Key
	state.StickyKeyOK = extraction.OK
	state.StickyKeyReason = extraction.Reason
}

func applyStickyPrimary(
	state *PersistenceState,
	source string,
	degradeReason string,
	target StickySessionTarget,
) {
	if state == nil {
		return
	}

	state.StickyRoutingSource = source
	state.StickyRoutingDegradeReason = degradeReason
	state.PreferredCredentialID = target.CredentialID
	state.PreferredCredentialFingerprint = target.CredentialFingerprint
}

func logStickyOrder(
	ctx context.Context,
	req *llm.Request,
	ordered []*ChannelModelsCandidate,
	extraction StickyKeyExtraction,
	source string,
	degradeReason string,
) {
	if !log.DebugEnabled(ctx) || len(ordered) == 0 || ordered[0] == nil || ordered[0].Channel == nil {
		return
	}

	model := ""
	if req != nil {
		model = req.Model
	}
	log.Debug(ctx, "sticky-session ordered candidates",
		log.String("source", source),
		log.String("degrade_reason", degradeReason),
		log.String("reason", extraction.Reason),
		log.String("sticky_kind", stickyKindAPIKey),
		log.Int("channel_id", ordered[0].Channel.ID),
		log.String("channel_name", ordered[0].Channel.Name),
		log.String("model", model),
		log.Int("candidate_count", len(ordered)))
}

func (r *StickySessionRouter) boundCandidateForKey(
	ctx context.Context,
	key string,
	candidates []*ChannelModelsCandidate,
) (*ChannelModelsCandidate, StickySessionTarget) {
	if key == "" {
		return nil, StickySessionTarget{}
	}

	target, ok := stickyStoreGetTarget(r.store, key)
	if !ok {
		return nil, StickySessionTarget{}
	}

	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil || candidate.Channel.ID != target.ChannelID {
			continue
		}
		return candidate, target
	}

	r.store.Delete(key)
	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding ignored because channel is absent from candidates",
			log.Int("bound_channel_id", target.ChannelID))
	}
	return nil, StickySessionTarget{}
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

func stickyOrderedCandidates(
	req StickySessionOrderRequest,
	primary *ChannelModelsCandidate,
) []*ChannelModelsCandidate {
	result := []*ChannelModelsCandidate{primary}
	remaining := make([]*ChannelModelsCandidate, 0, len(req.Candidates)-1)
	for _, candidate := range req.Candidates {
		if candidate == nil || candidate.Channel == nil {
			continue
		}
		if primary != nil && primary.Channel != nil && candidate.Channel.ID == primary.Channel.ID {
			continue
		}
		remaining = append(remaining, candidate)
	}

	return append(result, remaining...)
}

func stickyFirstBindCandidate(
	ctx context.Context,
	candidates []*ChannelModelsCandidate,
	stickyKey string,
) (*ChannelModelsCandidate, StickySessionTarget, string, string) {
	_ = ctx
	if len(candidates) == 0 || candidates[0] == nil {
		return nil, StickySessionTarget{}, stickyRoutingSourceNoCandidates, stickyRoutingDegradeNone
	}

	primary := candidates[0]
	return primary, stickyCandidatePreferredTarget(primary, stickyKey), stickyRoutingSourceNormalFirstBind, stickyRoutingDegradeNone
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

func withStickySessionBinding(outbound *PersistentOutboundTransformer, store StickySessionStore, enabled bool) pipeline.Middleware {
	return &stickySessionBindingMiddleware{
		outbound: outbound,
		store:    store,
		enabled:  enabled,
	}
}

type stickySessionBindingMiddleware struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	store    StickySessionStore
	enabled  bool
}

func (m *stickySessionBindingMiddleware) Name() string {
	return "sticky-session-binding"
}

func (m *stickySessionBindingMiddleware) OnInboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	m.bindCurrentChannel(ctx)
	return response, nil
}

func (m *stickySessionBindingMiddleware) OnInboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	if !m.enabled || m.outbound == nil || m.outbound.state == nil {
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
	if m == nil || !m.enabled || m.store == nil || m.outbound == nil || m.outbound.state == nil {
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

	target := StickySessionTarget{
		CredentialID:          state.CurrentCredentialID,
		ChannelID:             channel.ID,
		CredentialFingerprint: state.CurrentCredentialFingerprint,
	}
	if targetStore, ok := m.store.(StickySessionTargetStore); ok {
		targetStore.BindTarget(state.StickyKey, target)
	} else {
		m.store.Bind(state.StickyKey, channel.ID)
	}

	if log.DebugEnabled(ctx) {
		log.Debug(ctx, "sticky-session binding refreshed",
			log.Int("channel_id", channel.ID),
			log.String("channel_name", channel.Name),
			log.String("reason", state.StickyKeyReason),
			log.String("sticky_kind", stickyKindAPIKey))
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
