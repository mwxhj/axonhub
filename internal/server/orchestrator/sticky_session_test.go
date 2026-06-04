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

type stickyEligibilityStrategy struct {
	exhaustedChannelID int
}

func (s stickyEligibilityStrategy) Score(ctx context.Context, channel *biz.Channel) float64 {
	if channel.ID == s.exhaustedChannelID {
		return rateLimitExhaustedScore
	}

	return 100
}

func (s stickyEligibilityStrategy) ScoreWithDebug(ctx context.Context, channel *biz.Channel) (float64, StrategyScore) {
	score := s.Score(ctx, channel)
	return score, StrategyScore{StrategyName: s.Name(), Score: score}
}

func (s stickyEligibilityStrategy) Name() string {
	return "stickyEligibility"
}

func stickyTestMessage(role, content string) llm.Message {
	return llm.Message{
		Role: role,
		Content: llm.MessageContent{
			Content: &content,
		},
	}
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

func stickyTestLoadBalancer(retryEnabled bool) *LoadBalancer {
	return NewLoadBalancer(
		&mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: retryEnabled, MaxChannelRetries: 3}},
		nil,
		NewWeightStrategy(),
		NewRandomStrategy(),
	)
}

func TestDefaultStickyKeyExtractor_RejectsOnlyLatestUserMessage(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()

	result := extractor.Extract(context.Background(), nil, &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			stickyTestMessage("user", "hello"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})

	require.False(t, result.OK)
	require.Empty(t, result.Key)
	require.Equal(t, "only latest user message", result.Reason)
}

func TestDefaultStickyKeyExtractor_UsesStablePrefixNotLatestUserOnly(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()

	first := extractor.Extract(context.Background(), nil, &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			stickyTestMessage("system", "You are a precise coding assistant."),
			stickyTestMessage("user", "first turn"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})
	second := extractor.Extract(context.Background(), nil, &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			stickyTestMessage("system", "You are a precise coding assistant."),
			stickyTestMessage("user", "different latest turn"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})
	changedPrefix := extractor.Extract(context.Background(), nil, &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			stickyTestMessage("system", "You are a terse coding assistant."),
			stickyTestMessage("user", "first turn"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	})

	require.True(t, first.OK)
	require.True(t, second.OK)
	require.True(t, changedPrefix.OK)
	require.Equal(t, first.Key, second.Key)
	require.NotEqual(t, first.Key, changedPrefix.Key)
}

func TestDefaultStickyKeyExtractor_AcceptsExplicitCacheSignals(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()
	promptCacheKey := "cache-prefix-1"

	result := extractor.Extract(context.Background(), nil, &llm.Request{
		Model:          "gpt-4",
		PromptCacheKey: &promptCacheKey,
		Messages: []llm.Message{
			stickyTestMessage("user", "hello"),
		},
		APIFormat: llm.APIFormatOpenAIResponse,
	})

	require.True(t, result.OK)
	require.NotEmpty(t, result.Key)
	require.Equal(t, "prompt cache key", result.Reason)
}

func TestDefaultStickyKeyExtractor_UsesCodexSessionBeforeTranscript(t *testing.T) {
	extractor := NewDefaultStickyKeyExtractor()
	req := &httpclient.Request{Headers: make(map[string][]string)}
	req.Headers.Set("Session_id", "codex-session-1")

	result := extractor.Extract(context.Background(), nil, &llm.Request{
		Model:      "gpt-4",
		RawRequest: req,
		Messages: []llm.Message{
			stickyTestMessage("user", "first turn with enough content to be independently cacheable and recognizable"),
			stickyTestMessage("assistant", "tool call"),
			stickyTestMessage("tool", "tool result"),
			stickyTestMessage("user", "next turn"),
		},
		APIFormat: llm.APIFormatOpenAIResponse,
	})

	require.True(t, result.OK)
	require.NotEmpty(t, result.Lookups)
	require.Equal(t, "session", result.Lookups[0].Kind)
	require.Equal(t, "codex session", result.Lookups[0].Reason)
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

func TestStickySessionRouter_UnboundPrimaryStaysInsideBestTier(t *testing.T) {
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(1, 0, 100),
		stickyTestCandidate(2, 0, 100),
		stickyTestCandidate(3, 0, 50),
		stickyTestCandidate(4, 1, 100),
	}

	for range 50 {
		state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 3}}}
		ordered := router.Order(context.Background(), StickySessionOrderRequest{
			Request:      &llm.Request{Model: "gpt-4"},
			State:        state,
			Candidates:   candidates,
			LoadBalancer: stickyTestLoadBalancer(true),
		})

		require.NotEmpty(t, ordered)
		require.Contains(t, []int{1, 2}, ordered[0].Channel.ID)
		require.True(t, state.StickyKeyOK)
	}
}

func TestStickySessionRouter_UnboundPrimaryPrefersLowestLocalQuotaRatio(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 10, stickyTestQuotaCredentialView(10, 100, "cred:low", "low-key", "10", "100", resetAt)),
		stickyTestQuotaCandidate(2, 0, 100, stickyTestQuotaCredentialView(20, 200, "cred:high", "high-key", "80", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceQuotaRatioFirstBind, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNone, state.StickyRoutingDegradeReason)
	require.Equal(t, 10, state.PreferredCredentialID)
	require.Equal(t, "cred:low", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_BoundPrimaryDoesNotRebalanceByQuotaRatio(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.BindTarget("key", StickySessionTarget{ChannelID: 2, CredentialID: 20, CredentialFingerprint: "cred:high"})
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 10, stickyTestQuotaCredentialView(10, 100, "cred:low", "low-key", "10", "100", resetAt)),
		stickyTestQuotaCandidate(2, 0, 100, stickyTestQuotaCredentialView(20, 200, "cred:high", "high-key", "80", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceBinding, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNone, state.StickyRoutingDegradeReason)
	require.Equal(t, 20, state.PreferredCredentialID)
	require.Equal(t, "cred:high", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_ExpiredBindingRebindsByLowestUSDQuotaRatio(t *testing.T) {
	now := time.Date(2026, 6, 3, 23, 22, 35, 0, time.UTC)
	resetAt := time.Now().Add(time.Hour)
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.now = func() time.Time { return now }
	store.BindTarget("key", StickySessionTarget{ChannelID: 1, CredentialID: 10, CredentialFingerprint: "cred:lite"})
	now = now.Add(6 * time.Minute)

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, stickyTestUSDQuotaCredentialView(10, 100, "cred:lite", "lite-key", "59.137625", "100", resetAt)),
		stickyTestQuotaCandidate(2, 0, 90, stickyTestUSDQuotaCredentialView(20, 200, "cred:air", "air-key", "37.154457", "300", resetAt)),
		stickyTestQuotaCandidate(3, 0, 80, stickyTestUSDQuotaCredentialView(30, 300, "cred:plus", "plus-key", "10.255685", "500", resetAt)),
		stickyTestQuotaCandidate(4, 0, 70, stickyTestUSDQuotaCredentialView(40, 400, "cred:std", "std-key", "45.703334", "500", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 3, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceQuotaRatioFirstBind, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNone, state.StickyRoutingDegradeReason)
	require.Equal(t, 30, state.PreferredCredentialID)
	require.Equal(t, "cred:plus", state.PreferredCredentialFingerprint)
	_, ok := store.GetTarget("key")
	require.False(t, ok)
}

func TestStickySessionRouter_ExpiredBindingRebindsWithinSharedChannelWhenSeededKeyQuotaWindowExpired(t *testing.T) {
	now := time.Date(2026, 6, 4, 4, 45, 15, 0, time.UTC)
	expiredResetAt := time.Now().Add(-time.Minute)
	freshResetAt := time.Now().Add(time.Hour)

	liteView := stickyTestUSDQuotaCredentialView(10, 100, "cred:lite", "lite-key", "107.118356", "100", expiredResetAt)
	plusView := stickyTestUSDQuotaCredentialView(11, 110, "cred:plus", "plus-key", "11.341823", "500", freshResetAt)
	airView := stickyTestUSDQuotaCredentialView(12, 120, "cred:air", "air-key", "7.190881", "300", freshResetAt)
	stdView := stickyTestUSDQuotaCredentialView(20, 200, "cred:std", "std-key", "21.774737", "500", freshResetAt)

	inputViews := []biz.ChannelCredentialView{liteView, plusView, airView}
	stickyKey := stickyTestSeedForCredential(t, inputViews, liteView.Fingerprint)

	store := NewStickySessionBindingStore(5 * time.Minute)
	store.now = func() time.Time { return now }
	store.BindTarget(stickyKey, StickySessionTarget{
		ChannelID:              1,
		CredentialID:           liteView.CredentialID,
		CredentialFingerprint:  liteView.Fingerprint,
	})
	store.now = func() time.Time { return now.Add(6 * time.Minute) }

	router := NewStickySessionRouter(store, fixedStickyExtractor{
		result: StickyKeyExtraction{Key: stickyKey, OK: true, Reason: "test"},
	})
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, inputViews...),
		stickyTestQuotaCandidate(2, 0, 90, stdView),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceQuotaRatioFirstBind, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNone, state.StickyRoutingDegradeReason)
	require.Equal(t, plusView.CredentialID, state.PreferredCredentialID)
	require.Equal(t, plusView.Fingerprint, state.PreferredCredentialFingerprint)
	_, ok := store.GetTarget(stickyKey)
	require.False(t, ok)
}

func TestStickySessionRouter_QuotaRatioFirstBindDoesNotCrossPriority(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 2}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, stickyTestQuotaCredentialView(10, 100, "cred:high", "high-key", "80", "100", resetAt)),
		stickyTestQuotaCandidate(2, 1, 100, stickyTestQuotaCredentialView(20, 200, "cred:low", "low-key", "10", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceQuotaRatioFirstBind, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNone, state.StickyRoutingDegradeReason)
	require.Equal(t, "cred:high", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_NoQuotaPrimaryKeepsNormalLoadBalancerChoice(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, stickyTestCredentialView(10, "cred:no-quota", "no-quota-key")),
		stickyTestQuotaCandidate(2, 0, 10, stickyTestQuotaCredentialView(20, 200, "cred:low", "low-key", "10", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Equal(t, stickyRoutingSourceLoadBalancedNoComparableKey, state.StickyRoutingSource)
	require.Equal(t, stickyRoutingDegradeNoComparableQuota, state.StickyRoutingDegradeReason)
	require.Empty(t, state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_QuotaPrimaryIgnoresNoQuotaKeysForRatioPool(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, stickyTestQuotaCredentialView(10, 100, "cred:high", "high-key", "80", "100", resetAt)),
		stickyTestQuotaCandidate(2, 0, 80, stickyTestCredentialView(20, "cred:no-quota", "no-quota-key")),
		stickyTestQuotaCandidate(3, 0, 10, stickyTestQuotaCredentialView(30, 300, "cred:low", "low-key", "10", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 3, ordered[0].Channel.ID)
	require.Equal(t, "cred:low", state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_SharedQuotaScopeComparesOnce(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100,
			stickyTestQuotaCredentialView(10, 100, "cred:shared-a", "shared-a-key", "80", "100", resetAt),
			stickyTestQuotaCredentialView(11, 100, "cred:shared-b", "shared-b-key", "80", "100", resetAt),
		),
		stickyTestQuotaCandidate(2, 0, 10, stickyTestQuotaCredentialView(20, 200, "cred:low", "low-key", "10", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Equal(t, 20, state.PreferredCredentialID)
}

func TestStickySessionRouter_InvalidQuotaDataDoesNotParticipate(t *testing.T) {
	resetAt := time.Now().Add(time.Hour)
	router := NewStickySessionRouter(
		NewStickySessionBindingStore(5*time.Minute),
		fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}},
	)
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}}
	invalidView := stickyTestQuotaCredentialView(10, 100, "cred:invalid", "invalid-key", "1", "0", resetAt)
	candidates := []*ChannelModelsCandidate{
		stickyTestQuotaCandidate(1, 0, 100, invalidView),
		stickyTestQuotaCandidate(2, 0, 10, stickyTestQuotaCredentialView(20, 200, "cred:low", "low-key", "10", "100", resetAt)),
	}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: NewLoadBalancer(state.RetryPolicyProvider, nil, NewWeightStrategy()),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	require.Empty(t, state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_DoesNotCrossPriorityForBoundFallback(t *testing.T) {
	now := time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC)
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.now = func() time.Time { return now }
	store.Bind("key", 3)
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	candidates := []*ChannelModelsCandidate{
		stickyTestCandidate(1, 0, 100),
		stickyTestCandidate(2, 0, 100),
		stickyTestCandidate(3, 1, 50),
	}
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 2}}}

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: stickyTestLoadBalancer(true),
	})
	require.Contains(t, []int{1, 2}, ordered[0].Channel.ID)

	now = now.Add(6 * time.Minute)
	ordered = router.Order(context.Background(), StickySessionOrderRequest{
		Request:      &llm.Request{Model: "gpt-4"},
		State:        state,
		Candidates:   candidates,
		LoadBalancer: stickyTestLoadBalancer(true),
	})
	require.Contains(t, []int{1, 2}, ordered[0].Channel.ID)
}

func TestStickySessionRouter_KeepsCredentialOnSamePriorityChannel(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	fp := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com", "shared-key")
	store.BindTarget("key", StickySessionTarget{ChannelID: 1, CredentialFingerprint: fp})

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 2}}}
	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: &llm.Request{Model: "gpt-4"},
		State:   state,
		Candidates: []*ChannelModelsCandidate{
			stickyTestCredentialCandidate(2, 0, 100, "shared-key"),
			stickyTestCredentialCandidate(3, 1, 100, "shared-key"),
		},
		LoadBalancer: stickyTestLoadBalancer(true),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Equal(t, fp, state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_DoesNotUseLowerPriorityCredentialBinding(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	fp := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com", "shared-key")
	store.BindTarget("key", StickySessionTarget{ChannelID: 1, CredentialFingerprint: fp})

	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	state := &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 2}}}
	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: &llm.Request{Model: "gpt-4"},
		State:   state,
		Candidates: []*ChannelModelsCandidate{
			stickyTestCredentialCandidate(2, 0, 100, "other-key"),
			stickyTestCredentialCandidate(3, 1, 100, "shared-key"),
		},
		LoadBalancer: stickyTestLoadBalancer(true),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	require.Empty(t, state.PreferredCredentialFingerprint)
}

func TestStickySessionRouter_UsesResponsesPreviousResponseIDBeforePrefix(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	previousResponseID := "resp_1"
	extractor := NewDefaultStickyKeyExtractor()
	req := &llm.Request{
		Model:              "gpt-4",
		PreviousResponseID: &previousResponseID,
		Messages: []llm.Message{
			stickyTestMessage("user", "first turn with enough stable content to create a transcript prefix binding"),
			stickyTestMessage("assistant", "first answer"),
			stickyTestMessage("user", "second turn"),
		},
		APIFormat: llm.APIFormatOpenAIResponse,
	}
	extraction := extractor.Extract(context.Background(), nil, req)
	require.True(t, extraction.OK)
	require.NotEmpty(t, extraction.Lookups)
	require.Equal(t, "responses", extraction.Lookups[0].Kind)
	store.Bind(extraction.Lookups[0].Key, 2)

	router := NewStickySessionRouter(store, extractor)
	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: req,
		State:   &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}}},
		Candidates: []*ChannelModelsCandidate{
			stickyTestCandidate(1, 0, 100),
			stickyTestCandidate(2, 0, 100),
		},
		LoadBalancer: stickyTestLoadBalancer(true),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
}

func TestStickySessionRouter_TranscriptPrefixMatchesGrowingChat(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	extractor := NewDefaultStickyKeyExtractor()
	round1Request := &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			stickyTestMessage("user", "please analyze this large project context and keep the answer grounded in the exact files"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	}
	round1State := &PersistenceState{
		LlmRequest:      round1Request,
		StickyKeyOK:     true,
		StickyKeyReason: "pending transcript prefix",
		StickyBindings:  nil,
		StickyResponseMessage: &llm.Message{
			Role:    "assistant",
			Content: llm.MessageContent{Content: ptrString("first answer")},
		},
	}
	bindings := stickyBindingAliases(round1State)
	require.NotEmpty(t, bindings)
	store.Bind(bindings[0].Key, 1)

	round2Request := &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{
			round1Request.Messages[0],
			*round1State.StickyResponseMessage,
			stickyTestMessage("user", "now continue with the next part"),
		},
		APIFormat: llm.APIFormatOpenAIChatCompletion,
	}
	router := NewStickySessionRouter(store, extractor)
	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: round2Request,
		State:   &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}}},
		Candidates: []*ChannelModelsCandidate{
			stickyTestCandidate(1, 0, 100),
			stickyTestCandidate(2, 0, 100),
		},
		LoadBalancer: stickyTestLoadBalancer(true),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
}

func TestStickySessionRouter_StaleBindingIgnoredAndDeleted(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 99)
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: &llm.Request{Model: "gpt-4"},
		State:   &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: false}}},
		Candidates: []*ChannelModelsCandidate{
			stickyTestCandidate(1, 0, 100),
			stickyTestCandidate(2, 0, 100),
		},
		LoadBalancer: stickyTestLoadBalancer(false),
	})

	require.NotEmpty(t, ordered)
	require.Contains(t, []int{1, 2}, ordered[0].Channel.ID)
	_, ok := store.Get("key")
	require.False(t, ok)
}

func TestStickySessionRouter_SkipsBoundPrimaryWhenIneligible(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 1)
	router := NewStickySessionRouter(store, fixedStickyExtractor{result: StickyKeyExtraction{Key: "key", OK: true, Reason: "test"}})
	loadBalancer := NewLoadBalancer(
		&mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}},
		nil,
		stickyEligibilityStrategy{exhaustedChannelID: 1},
	)

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: &llm.Request{Model: "gpt-4"},
		State:   &PersistenceState{RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}}},
		Candidates: []*ChannelModelsCandidate{
			stickyTestCandidate(1, 0, 100),
			stickyTestCandidate(2, 0, 100),
		},
		LoadBalancer: loadBalancer,
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 2, ordered[0].Channel.ID)
	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 1, channelID)
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

	ordered := router.Order(context.Background(), StickySessionOrderRequest{
		Request: &llm.Request{Model: "gpt-4"},
		State: &PersistenceState{
			OriginalModel:       "gpt-4",
			RetryPolicyProvider: &mockRetryPolicyProvider{policy: &biz.RetryPolicy{Enabled: true, MaxChannelRetries: 1}},
		},
		Candidates: []*ChannelModelsCandidate{
			stickyTestCandidate(1, 0, 100),
			stickyTestCandidate(2, 0, 100),
		},
		LoadBalancer: stickyTestLoadBalancer(true),
	})

	require.NotEmpty(t, ordered)
	require.Equal(t, 1, ordered[0].Channel.ID)
	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 1, channelID)
}

func TestModelCircuitBreakerMiddleware_InactiveForStickySession(t *testing.T) {
	cb := biz.NewModelCircuitBreaker()
	for range 5 {
		cb.RecordError(context.Background(), 1, "gpt-4")
	}
	require.Equal(t, biz.StateOpen, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").State)

	outbound := &PersistentOutboundTransformer{
		state: &PersistenceState{
			OriginalModel: "gpt-4",
			CurrentCandidate: &ChannelModelsCandidate{
				Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "channel"}},
			},
		},
	}
	middleware := withModelCircuitBreaker(outbound, cb, biz.LoadBalancerStrategyStickySession)

	request := &httpclient.Request{}
	got, err := middleware.OnOutboundRawRequest(context.Background(), request)
	require.NoError(t, err)
	require.Same(t, request, got)

	middleware.OnOutboundRawError(context.Background(), errors.New("upstream failed"))
	require.Equal(t, 5, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").ConsecutiveFailures)
}

func TestModelCircuitBreakerMiddleware_StillSkipsForCircuitBreaker(t *testing.T) {
	cb := biz.NewModelCircuitBreaker()
	for range 5 {
		cb.RecordError(context.Background(), 1, "gpt-4")
	}
	require.Equal(t, biz.StateOpen, cb.GetModelCircuitBreakerStats(context.Background(), 1, "gpt-4").State)

	outbound := &PersistentOutboundTransformer{
		state: &PersistenceState{
			OriginalModel: "gpt-4",
			CurrentCandidate: &ChannelModelsCandidate{
				Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "channel"}},
			},
		},
	}
	middleware := withModelCircuitBreaker(outbound, cb, biz.LoadBalancerStrategyCircuitBreaker)

	got, err := middleware.OnOutboundRawRequest(context.Background(), &httpclient.Request{})
	require.ErrorIs(t, err, errSkipCandidateByCircuitBreaker)
	require.Nil(t, got)
}

func TestStickySessionBindingMiddleware_BindsSuccessfulFallbackChannel(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	store.Bind("key", 1)

	state := &PersistenceState{
		StickyKey:             "key",
		StickyKeyOK:           true,
		StickyKeyReason:       "test",
		CurrentCandidate:      stickyTestCandidate(3, 1, 50),
		CurrentModelIndex:     0,
		CurrentCandidateIndex: 0,
	}
	middleware := &stickySessionBindingMiddleware{
		outbound: &PersistentOutboundTransformer{state: state},
		store:    store,
		strategy: biz.LoadBalancerStrategyStickySession,
	}

	middleware.bindCurrentChannel(context.Background())

	channelID, ok := store.Get("key")
	require.True(t, ok)
	require.Equal(t, 3, channelID)
}

func TestStickySessionBindingMiddleware_BindsCredentialTarget(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)

	state := &PersistenceState{
		StickyKey:                    "key",
		StickyKeyOK:                  true,
		StickyKeyReason:              "test",
		CurrentCandidate:             stickyTestCandidate(3, 1, 50),
		CurrentModelIndex:            0,
		CurrentCandidateIndex:        0,
		CurrentCredentialFingerprint: "cred:v1:selected",
	}
	middleware := &stickySessionBindingMiddleware{
		outbound: &PersistentOutboundTransformer{state: state},
		store:    store,
		strategy: biz.LoadBalancerStrategyStickySession,
	}

	middleware.bindCurrentChannel(context.Background())

	target, ok := store.GetTarget("key")
	require.True(t, ok)
	require.Equal(t, 3, target.ChannelID)
	require.Equal(t, "cred:v1:selected", target.CredentialFingerprint)
}

func TestStickySessionBindingMiddleware_BindsResponsesIDAndMigratesActiveAliases(t *testing.T) {
	store := NewStickySessionBindingStore(5 * time.Minute)
	previousResponseID := "resp_1"
	req := &llm.Request{
		Model:              "gpt-4",
		PreviousResponseID: &previousResponseID,
		APIFormat:          llm.APIFormatOpenAIResponse,
	}
	extraction := NewDefaultStickyKeyExtractor().Extract(context.Background(), nil, req)
	require.True(t, extraction.OK)
	require.NotEmpty(t, extraction.Bindings)
	store.Bind(extraction.Bindings[0].Key, 1)

	state := &PersistenceState{
		LlmRequest:               req,
		StickyKeyOK:              true,
		StickyKeyReason:          extraction.Reason,
		StickyBindings:           extraction.Bindings,
		StickyResponseID:         "resp_2",
		StickyPreviousResponseID: previousResponseID,
		CurrentCandidate:         stickyTestCandidate(2, 0, 100),
		CurrentModelIndex:        0,
	}
	middleware := &stickySessionBindingMiddleware{
		outbound: &PersistentOutboundTransformer{state: state},
		store:    store,
		strategy: biz.LoadBalancerStrategyStickySession,
	}

	middleware.bindCurrentChannel(context.Background())

	channelID, ok := store.Get(extraction.Bindings[0].Key)
	require.True(t, ok)
	require.Equal(t, 2, channelID)

	responseLookup := stickyValueLookup(stickyKindResponse, "resp_2", stickyStrengthResponse, "response id", stickyBasePayload(state, req))
	channelID, ok = store.Get(responseLookup.Key)
	require.True(t, ok)
	require.Equal(t, 2, channelID)
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
			state := &PersistenceState{
				StickyKey:             "key",
				StickyKeyOK:           true,
				StickyKeyReason:       "test",
				CurrentCandidate:      stickyTestCandidate(3, 1, 50),
				CurrentModelIndex:     0,
				CurrentCandidateIndex: 0,
				StreamCompleted:       true,
				OriginalRequestStream: &tt.originalStream,
			}
			middleware := &stickySessionBindingMiddleware{
				outbound: &PersistentOutboundTransformer{state: state},
				store:    store,
				strategy: biz.LoadBalancerStrategyStickySession,
			}

			stream, err := middleware.OnInboundRawStream(
				context.Background(),
				streams.SliceStream([]*httpclient.StreamEvent{}),
			)
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
	state := &PersistenceState{
		StickyKey:             "key",
		StickyKeyOK:           true,
		StickyKeyReason:       "test",
		CurrentCandidate:      stickyTestCandidate(3, 1, 50),
		CurrentModelIndex:     0,
		CurrentCandidateIndex: 0,
		StreamCompleted:       false,
		OriginalRequestStream: &originalStream,
	}
	middleware := &stickySessionBindingMiddleware{
		outbound: &PersistentOutboundTransformer{state: state},
		store:    store,
		strategy: biz.LoadBalancerStrategyStickySession,
	}

	stream, err := middleware.OnInboundRawStream(
		context.Background(),
		streams.SliceStream([]*httpclient.StreamEvent{}),
	)
	require.NoError(t, err)
	require.NoError(t, stream.Close())

	_, ok := store.Get("key")
	require.False(t, ok)
}

func ptrString(value string) *string {
	return &value
}
