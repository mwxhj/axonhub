package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

type fixedStickyExtractor struct {
	result StickyKeyExtraction
}

func (e fixedStickyExtractor) Extract(ctx context.Context, state *PersistenceState, req *llm.Request) StickyKeyExtraction {
	return e.result
}

func stickyTestCandidate(id int, priority int, weight int) *ChannelModelsCandidate {
	return &ChannelModelsCandidate{
		Channel: &biz.Channel{
			Channel: &ent.Channel{
				ID:             id,
				Name:           "channel",
				OrderingWeight: weight,
			},
		},
		Priority: priority,
		Models: []biz.ChannelModelEntry{{
			RequestModel: "gpt-4",
			ActualModel:  "gpt-4",
			Source:       "test",
		}},
	}
}

func stickyCandidateIDs(candidates []*ChannelModelsCandidate) []int {
	ids := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == nil || candidate.Channel == nil {
			continue
		}
		ids = append(ids, candidate.Channel.ID)
	}
	return ids
}

func stickyTestCredentialCandidate(id int, priority int, weight int, keys ...string) *ChannelModelsCandidate {
	candidate := stickyTestCandidate(id, priority, weight)
	candidate.Channel.Type = channel.TypeOpenai
	candidate.Channel.BaseURL = "https://api.openai.com"
	if len(keys) == 0 {
		return candidate
	}

	views := make([]biz.ChannelCredentialView, 0, len(keys))
	for i, key := range keys {
		fingerprint := biz.ChannelCredentialFingerprintForAPIKey(candidate.Channel.Type.String(), candidate.Channel.BaseURL, key)
		views = append(views, stickyTestCredentialView(i+1, fingerprint, key))
	}

	candidate.Channel = candidate.Channel.WithCredentialViewsForSelection(views)
	return candidate
}

func stickyTestCredentialView(credentialID int, fingerprint string, apiKey string) biz.ChannelCredentialView {
	secret := objects.UpstreamCredentialSecretFromAPIKey(apiKey)
	return biz.ChannelCredentialView{
		CredentialID:      credentialID,
		Name:              fingerprint,
		Fingerprint:       fingerprint,
		SecretFingerprint: "secret:v1:" + fingerprint,
		ResourceScopeKey:  "openai:secret:v1:" + fingerprint,
		AuthKind:          "api_key",
		SecretKind:        "api_key",
		IssuerScope:       "openai",
		KeyHint:           apiKey,
		Secret:            secret,
		Enabled:           true,
		Weight:            1,
		Source:            biz.ChannelCredentialSourceRef,
	}
}

func stickyTestQuotaCredentialView(
	credentialID int,
	quotaScopeID int,
	fingerprint string,
	apiKey string,
	usedAmount string,
	limitAmount string,
	resetAt time.Time,
) biz.ChannelCredentialView {
	view := stickyTestCredentialView(credentialID, fingerprint, apiKey)
	view.QuotaScopeID = quotaScopeID
	view.QuotaScopeName = "quota"
	view.QuotaScopeStatus = credentialquotascope.StatusAvailable.String()
	view.QuotaScopeOverLimitAction = credentialquotascope.OverLimitActionWarn.String()
	view.QuotaScopeResetPolicy = credentialquotascope.ResetPolicyDaily.String()
	view.QuotaScopeResetAt = &resetAt
	view.QuotaScopeUnit = credentialquotascope.UnitToken.String()
	view.QuotaScopeLimitAmount = limitAmount
	view.QuotaScopeUsedAmount = usedAmount
	view.QuotaScopeSource = credentialquotascope.SourceLocalBudget.String()
	return view
}

func stickyTestUSDQuotaCredentialView(
	credentialID int,
	quotaScopeID int,
	fingerprint string,
	apiKey string,
	usedAmount string,
	limitAmount string,
	resetAt time.Time,
) biz.ChannelCredentialView {
	view := stickyTestQuotaCredentialView(credentialID, quotaScopeID, fingerprint, apiKey, usedAmount, limitAmount, resetAt)
	view.QuotaScopeUnit = credentialquotascope.UnitUsd.String()
	return view
}

func stickyTestQuotaCandidate(
	id int,
	priority int,
	weight int,
	views ...biz.ChannelCredentialView,
) *ChannelModelsCandidate {
	candidate := stickyTestCandidate(id, priority, weight)
	candidate.Channel.Type = channel.TypeOpenai
	candidate.Channel.BaseURL = "https://api.openai.com"
	candidate.Channel = candidate.Channel.WithCredentialViewsForSelection(views)
	return candidate
}

func stickyTestSeedForCredential(t *testing.T, views []biz.ChannelCredentialView, fingerprint string) string {
	t.Helper()

	for i := range 10_000 {
		seed := fmt.Sprintf("sticky-seed-%d", i)
		selected, ok := biz.SelectCredentialViewBySeed(views, "sticky:"+seed)
		if ok && selected.Fingerprint == fingerprint {
			return seed
		}
	}

	t.Fatalf("failed to find deterministic seed for credential %q", fingerprint)
	return ""
}

func TestDefaultStickyKeyExtractor_UsesAPIKeyIdentityOnly(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 11, ProjectID: 22}}

	first := extractor.Extract(context.Background(), state, &llm.Request{Model: "gpt-4", APIFormat: llm.APIFormatOpenAIChatCompletion})
	second := extractor.Extract(context.Background(), state, &llm.Request{Model: "gpt-4", APIFormat: llm.APIFormatOpenAIResponse, PreviousResponseID: ptrString("resp_1")})
	other := extractor.Extract(context.Background(), &PersistenceState{APIKey: &ent.APIKey{ID: 12, ProjectID: 22}}, &llm.Request{Model: "gpt-4", APIFormat: llm.APIFormatOpenAIChatCompletion})

	require.True(t, first.OK)
	require.True(t, second.OK)
	require.Equal(t, first.Key, second.Key)
	require.NotEqual(t, first.Key, other.Key)
	require.Equal(t, "api key sticky", first.Reason)
	require.Contains(t, first.Key, "api-key:v2:")
}

func TestDefaultStickyKeyExtractor_RejectsMissingAPIKeyIdentity(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()

	result := extractor.Extract(context.Background(), &PersistenceState{}, &llm.Request{Model: "gpt-4", APIFormat: llm.APIFormatOpenAIChatCompletion})

	require.False(t, result.OK)
	require.Empty(t, result.Key)
	require.Equal(t, "missing api key identity", result.Reason)
}

func TestStickySessionBindingStore_TTLAndLatestWriteWins(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.now = func() time.Time { return now }

	store.Bind("key", 1)
	store.Bind("key", 2)

	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 2, channelID)

	now = now.Add(6 * time.Minute)

	_, ok = store.Get("key")
	require.False(t, ok)
	require.Empty(t, store.bindings)
}

func TestStickySessionBindingStore_TargetPreservesCredentialFingerprint(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.BindTarget("key", StickySessionTarget{ChannelID: 7, CredentialFingerprint: "cred:v1:test"})

	target, ok := store.GetTarget("key")
	require.True(t, ok)
	require.Equal(t, 7, target.ChannelID)
	require.Equal(t, "cred:v1:test", target.CredentialFingerprint)

	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 7, channelID)
}

func TestStickySessionRouter_UnboundPrimaryPreservesRouteTierOrder(t *testing.T) {
	router := NewStickySessionRouter(NewStickySessionBindingStore(5*time.Minute), fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	candidates := []*ChannelModelsCandidate{stickyTestCandidate(1, 0, 100), stickyTestCandidate(2, 0, 100), stickyTestCandidate(3, 0, 50), stickyTestCandidate(4, 1, 100)}

	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 3}}}
	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: candidates})

	require.NotEmpty(t, ordered)
	require.Equal(t, []int{1, 2, 3, 4}, stickyCandidateIDs(ordered))
	require.Equal(t, stickyRoutingSourceNormalFirstBind, state.StickyRoutingSource)
	require.True(t, state.StickyKeyOK)
}

func TestStickySessionRouter_BoundPrimaryUsesBinding(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.BindTarget("key", StickySessionTarget{ChannelID: 2, CredentialID: 20, CredentialFingerprint: "cred:high"})
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{stickyTestQuotaCandidate(1, 0, 10), stickyTestQuotaCandidate(2, 0, 100)}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: candidates})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceBinding, state.StickyRoutingSource)
	require.Equal(t, 20, state.PreferredCredentialID)
	require.Equal(t, "cred:high", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_ExpiredBindingDeletesAndFallsBackToFirstBind(t *testing.T) {
	now := time.Date(2026, 6, 3, 23, 22, 35, 0, time.UTC)
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.now = func() time.Time { return now }
	store.BindTarget("key", StickySessionTarget{ChannelID: 1, CredentialID: 10, CredentialFingerprint: "cred:lite"})
	now = now.Add(6 * time.Minute)

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{stickyTestQuotaCandidate(1, 0, 100), stickyTestQuotaCandidate(2, 0, 90)}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: candidates})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceNormalFirstBind, state.StickyRoutingSource)
	_, ok := store.GetTarget("key")
	require.False(t, ok)
}

func TestStickySessionRouter_UnboundPrimaryKeepsSeededCredentialInsideNormalPrimary(t *testing.T) {
	router := NewStickySessionRouter(NewStickySessionBindingStore(5*time.Minute), fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	views := []biz.ChannelCredentialView{stickyTestCredentialView(10, "cred:first", "first-key"), stickyTestCredentialView(11, "cred:second", "second-key")}
	stickyKey := stickyTestSeedForCredential(t, views, "cred:second")
	router.extractor = fixedStickyExtractor{result: StickyKeyExtraction{Key: stickyKey, OK: true, Reason: "test"}}
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidate := stickyTestQuotaCandidate(1, 0, 100, views...)
	candidates := []*ChannelModelsCandidate{candidate, stickyTestCredentialCandidate(2, 0, 10, "other-key")}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: candidates})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceNormalFirstBind, state.StickyRoutingSource)
	require.Equal(t, 11, state.PreferredCredentialID)
	require.Equal(t, "cred:second", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_FirstBindIgnoresLowerPriorityBindingTarget(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	fp := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com", "shared-key")
	store.BindTarget("key", StickySessionTarget{ChannelID: 1, CredentialFingerprint: fp})

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 2}}}
	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: []*ChannelModelsCandidate{stickyTestCredentialCandidate(2, 0, 100, "other-key"), stickyTestCredentialCandidate(3, 1, 100, "shared-key")}})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceNormalFirstBind, state.StickyRoutingSource)
	require.Equal(t, biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com", "other-key"), state.PreferredCredentialFingerprint)
	require.NotEqual(t, fp, state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_SkipsBoundPrimaryWhenChannelMissing(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 99)
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{APIKey: &ent.APIKey{ID: 1}, RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: state, Candidates: []*ChannelModelsCandidate{stickyTestCandidate(1, 0, 100), stickyTestCandidate(2, 0, 100)}})

	require.NotEmpty(t, ordered)
	require.Contains(t, []int{1, 2}, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceNormalFirstBind, state.StickyRoutingSource)
	_, ok := store.Get("key")
	require.False(t, ok)
}

func TestStickySessionRouter_DoesNotConsultCircuitBreakerForBoundPrimary(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 1)
	cb := biz.NewModelCircuitBreaker()
	for range 5 {
		cb.RecordError(context.Background(), 1, "gpt-4")
	}
	require.Equal(t, biz.StateOpen, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").State)

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	ordered := router.Order(context.Background(), StickySessionOrderRequest{Request: &llm.Request{Model: "gpt-4"}, State: &PersistenceState{APIKey: &ent.APIKey{ID: 1}, OriginalModel: "gpt-4", RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}}}, Candidates: []*ChannelModelsCandidate{stickyTestCandidate(1, 0, 100), stickyTestCandidate(2, 0, 100)}})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 1, channelID)
}

func TestStickySessionBindingMiddleware_BindsSuccessfulFallbackChannel(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 1)

	state := &PersistenceState{StickyKey: "key", StickyKeyOK: true, StickyKeyReason: "test", CurrentCandidate: stickyTestCandidate(3, 1, 50), CurrentModelIndex: 0, CurrentCandidateIndex: 0}
	middleware := &stickySessionBindingMiddleware{outbound: &PersistentOutboundTransformer{state: state}, store: store, enabled: true}

	middleware.bindCurrentChannel(context.Background())

	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 3, channelID)
}

func TestStickySessionBindingMiddleware_BindsCredentialTarget(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)

	state := &PersistenceState{StickyKey: "key", StickyKeyOK: true, StickyKeyReason: "test", CurrentCandidate: stickyTestCandidate(3, 1, 50), CurrentModelIndex: 0, CurrentCandidateIndex: 0, CurrentCredentialFingerprint: "cred:v1:selected"}
	middleware := &stickySessionBindingMiddleware{outbound: &PersistentOutboundTransformer{state: state}, store: store, enabled: true}

	middleware.bindCurrentChannel(context.Background())

	target, ok := store.GetTarget("key")
	require.True(t, ok)
	require.Equal(t, 3, target.ChannelID)
	require.Equal(t, "cred:v1:selected", target.CredentialFingerprint)
}

func TestStickySessionBindingMiddleware_StreamCloseBindsOnlyForOriginalStreamingRequest(t *testing.T) {
	for _, tt := range []struct {
		name           string
		originalStream bool
		wantBinding    bool
	}{
		{name: "original stream", originalStream: true, wantBinding: true},
		{name: "auto aggregate", originalStream: false, wantBinding: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := NewStickySessionBindingStore(5 * time.Minute)
			state := &PersistenceState{StickyKey: "key", StickyKeyOK: true, StickyKeyReason: "test", CurrentCandidate: stickyTestCandidate(3, 1, 50), CurrentModelIndex: 0, CurrentCandidateIndex: 0, StreamCompleted: true, OriginalRequestStream: &tt.originalStream}
			middleware := &stickySessionBindingMiddleware{outbound: &PersistentOutboundTransformer{state: state}, store: store, enabled: true}

			stream, err := middleware.OnInboundRawStream(context.Background(), streams.SliceStream([]*httpclient.StreamEvent{}))
			require.NoError(t, err)
			require.NoError(t, stream.Close())

			channelID, ok := store.Get("key")
			if tt.wantBinding {
				require.True(t, ok)
				require.Equal(t, 3, channelID)
			} else {
				require.False(t, ok)
			}
		})
	}
}

func TestStickySessionBindingMiddleware_StreamCloseDoesNotBindIncompleteStream(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	originalStream := true
	state := &PersistenceState{StickyKey: "key", StickyKeyOK: true, StickyKeyReason: "test", CurrentCandidate: stickyTestCandidate(3, 1, 50), CurrentModelIndex: 0, CurrentCandidateIndex: 0, StreamCompleted: false, OriginalRequestStream: &originalStream}
	middleware := &stickySessionBindingMiddleware{outbound: &PersistentOutboundTransformer{state: state}, store: store, enabled: true}

	stream, err := middleware.OnInboundRawStream(context.Background(), streams.SliceStream([]*httpclient.StreamEvent{}))
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	_, ok := store.Get("key")
	require.False(t, ok)
}

func TestModelCircuitBreakerMiddleware_SkipsOpenRouteTierTarget(t *testing.T) {
	cb := biz.NewModelCircuitBreaker()
	for range 5 {
		cb.RecordError(context.Background(), 1, "gpt-4")
	}
	require.Equal(t, biz.StateOpen, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").State)

	outbound := &PersistentOutboundTransformer{state: &PersistenceState{OriginalModel: "gpt-4", CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "channel"}}}}}
	middleware := withModelCircuitBreaker(outbound, cb)

	got, err := middleware.OnOutboundRawRequest(context.Background(), &httpclient.Request{})
	require.ErrorIs(t, err, errSkipCandidateByCircuitBreaker)
	require.Nil(t, got)
}

func TestModelCircuitBreakerMiddleware_RecordsTargetError(t *testing.T) {
	cb := biz.NewModelCircuitBreaker()

	outbound := &PersistentOutboundTransformer{state: &PersistenceState{OriginalModel: "gpt-4", CurrentCandidate: &ChannelModelsCandidate{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "channel"}}}}}
	middleware := withModelCircuitBreaker(outbound, cb)

	middleware.OnOutboundRawError(context.Background(), errors.New("upstream failed"))
	require.Equal(t, 1, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").ConsecutiveFailures)
}

func ptrString(value string) *string {
	return &value
}
