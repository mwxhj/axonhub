package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm"
)

func TestProviderQuotaSelector_ExhaustedOnlyMode(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusWarning, Ready: true},
			3: {Status: providerquotastatus.StatusAvailable, Ready: true},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "exhausted"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "warning"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "available"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, 2, got[0].Channel.ID)
	require.Equal(t, 3, got[1].Channel.ID)
}

func TestProviderQuotaSelector_DePrioritizeMode(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusWarning, Ready: true},
			3: {Status: providerquotastatus.StatusAvailable, Ready: true},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeDePrioritize},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "exhausted"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "warning"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "available"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 3)

	ids := make([]int, len(got))
	for i, c := range got {
		ids[i] = c.Channel.ID
	}
	require.ElementsMatch(t, []int{1, 2, 3}, ids)
}

func TestProviderQuotaSelector_EnforcementDisabled(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: false, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "exhausted"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestProviderQuotaSelector_AllExhausted(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusExhausted, Ready: false},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "c1"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "c2"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestProviderQuotaSelector_NoQuotaData(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "no-data"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestProviderQuotaSelector_NilProvider(t *testing.T) {
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "test"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, nil, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestProviderQuotaSelector_WrappedError(t *testing.T) {
	provider := &mockQuotaStatusProvider{}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{err: errors.New("inner error")}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	_, err := selector.Select(context.Background(), &llm.Request{})

	require.Error(t, err)
	require.Equal(t, "inner error", err.Error())
}

func TestProviderQuotaSelector_EmptyCandidates(t *testing.T) {
	provider := &mockQuotaStatusProvider{}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{candidates: []*ChannelModelsCandidate{}}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Empty(t, got)
}

func TestProviderQuotaSelector_UnknownStatusKept(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusUnknown, Ready: false},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "unknown"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 1)
}

func TestProviderQuotaSelector_MixedCandidates(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusWarning, Ready: true},
			3: {Status: providerquotastatus.StatusAvailable, Ready: true},
			4: {Status: providerquotastatus.StatusUnknown, Ready: false},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeDePrioritize},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "exhausted"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "warning"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "available"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 4, Name: "unknown"}}},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 5, Name: "no-data"}}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 5)

	ids := make([]int, len(got))
	for i, c := range got {
		ids[i] = c.Channel.ID
	}
	require.ElementsMatch(t, []int{1, 2, 3, 4, 5}, ids)
}

func TestProviderQuotaSelector_PerLimit_ImageExhausted_KeptForToken(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {
				Status: providerquotastatus.StatusWarning,
				Ready:  true,
				Limits: []provider_quota.QuotaLimitStatus{
					{Type: provider_quota.QuotaLimitTypeImage, Status: "exhausted", UsageRatio: 1.0, Ready: false},
					{Type: provider_quota.QuotaLimitTypeToken, Status: "available", UsageRatio: 0.3, Ready: true},
				},
			},
		},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		},
	}

	systemService := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	selector := WithProviderQuotaSelector(inner, provider, systemService)

	tokenReq := &llm.Request{Model: "gpt-4"}
	result, err := selector.Select(context.Background(), tokenReq)
	require.NoError(t, err)
	require.Len(t, result, 1, "channel should be kept for token request when only image limit is exhausted")

	imageReq := &llm.Request{Model: "dall-e-3", Image: &llm.ImageRequest{}}
	result, err = selector.Select(context.Background(), imageReq)
	require.NoError(t, err)
	require.Len(t, result, 0, "channel should be filtered for image request when image limit is exhausted")
}

func TestProviderQuotaSelector_FiltersExhaustedBeforeLoadBalancer(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusExhausted, Ready: false},
			3: {Status: providerquotastatus.StatusAvailable, Ready: true},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "exhausted-1"}}, Priority: 0},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 2, Name: "exhausted-2"}}, Priority: 0},
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 3, Name: "available"}}, Priority: 1},
		},
	}

	quotaSelector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := quotaSelector.Select(context.Background(), &llm.Request{})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 3, got[0].Channel.ID, "available channel should be preserved after quota filtering")
}

func TestProviderQuotaSelector_ChannelExhaustedOverridesPerLimitAvailable(t *testing.T) {
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {
				Status: providerquotastatus.StatusExhausted,
				Ready:  false,
				Limits: []provider_quota.QuotaLimitStatus{
					{Type: provider_quota.QuotaLimitTypeToken, Status: "available", UsageRatio: 0.3, Ready: true},
				},
			},
		},
	}

	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: &ent.Channel{ID: 1, Name: "ch1"}}},
		},
	}

	systemService := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}

	selector := WithProviderQuotaSelector(inner, provider, systemService)

	tokenReq := &llm.Request{Model: "gpt-4"}
	result, err := selector.Select(context.Background(), tokenReq)
	require.NoError(t, err)
	require.Empty(t, result, "channel with Exhausted channel-level status must be filtered even if per-limit token status is available")
}

func TestAreAllChannelsExhausted_UsesCredentialDerivedQuotaStatus(t *testing.T) {
	sharedFingerprint := biz.ChannelCredentialFingerprintForAPIKey("openai", "https://api.openai.com/v1", "shared-key")
	otherFingerprint := biz.ChannelCredentialFingerprintForAPIKey("openai", "https://api.openai.com/v1", "other-key")
	provider := &mockQuotaStatusProvider{
		statuses: map[int]*biz.QuotaChannelStatus{
			1: {Status: providerquotastatus.StatusExhausted, Ready: false},
			2: {Status: providerquotastatus.StatusExhausted, Ready: false},
		},
		credentialStatuses: map[string]*biz.QuotaChannelStatus{
			sharedFingerprint: {Status: providerquotastatus.StatusExhausted, Ready: false},
			otherFingerprint:  {Status: providerquotastatus.StatusAvailable, Ready: true},
		},
	}

	candidates := []*ChannelModelsCandidate{
		{
			Channel: &biz.Channel{Channel: &ent.Channel{
				ID:          1,
				Type:        "openai",
				BaseURL:     "https://api.openai.com/v1",
				Credentials: objects.ChannelCredentials{APIKeys: []string{"shared-key"}},
			}},
		},
		{
			Channel: &biz.Channel{Channel: &ent.Channel{
				ID:          2,
				Type:        "openai",
				BaseURL:     "https://api.openai.com/v1",
				Credentials: objects.ChannelCredentials{APIKeys: []string{"other-key"}},
			}},
		},
	}

	require.False(t, areAllChannelsExhausted(candidates, provider, &llm.Request{Model: "gpt-4"}))
}

func TestProviderQuotaSelector_UsesResourceScopeBeforeCredentialIdentity(t *testing.T) {
	secret := objects.UpstreamCredentialSecretFromAPIKey("shared-key")
	secretFingerprint := biz.CredentialSecretFingerprintForSecret("api_key", secret)
	chA := &ent.Channel{
		ID:          1,
		Name:        "resource-a",
		Type:        "ollama",
		BaseURL:     "https://gateway-a.example.com/v1",
		Credentials: objects.ChannelCredentials{APIKeys: []string{"shared-key"}},
	}
	chB := &ent.Channel{
		ID:          2,
		Name:        "resource-b",
		Type:        "ollama",
		BaseURL:     "https://gateway-b.example.com/v1",
		Credentials: objects.ChannelCredentials{APIKeys: []string{"shared-key"}},
	}
	resourceA := biz.ChannelCredentialResourceScopeKey(chA, secretFingerprint)
	resourceB := biz.ChannelCredentialResourceScopeKey(chB, secretFingerprint)
	fingerprint := biz.ChannelCredentialFingerprintForAPIKey("ollama", "https://gateway-a.example.com/v1", "shared-key")

	provider := &mockQuotaStatusProvider{
		credentialStatuses: map[string]*biz.QuotaChannelStatus{
			fingerprint: {Status: providerquotastatus.StatusExhausted, Ready: false},
		},
		resourceStatuses: map[string]*biz.QuotaChannelStatus{
			resourceA: {Status: providerquotastatus.StatusExhausted, Ready: false, ResourceScopeKey: resourceA},
			resourceB: {Status: providerquotastatus.StatusAvailable, Ready: true, ResourceScopeKey: resourceB},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}
	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{
			{Channel: &biz.Channel{Channel: chA}},
			{Channel: &biz.Channel{Channel: chB}},
		},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{Model: "gpt-4"})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 2, got[0].Channel.ID)
}

func TestProviderQuotaSelector_NarrowsCredentialViewsBeforeAPIKeySelection(t *testing.T) {
	exhaustedKey := "exhausted-key"
	availableKey := "available-key"
	channel := &biz.Channel{Channel: &ent.Channel{
		ID:      1,
		Name:    "multi-key",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Credentials: objects.ChannelCredentials{
			APIKeys: []string{exhaustedKey, availableKey},
		},
	}}
	exhaustedFingerprint := channel.CredentialFingerprintForAPIKey(exhaustedKey)
	availableFingerprint := channel.CredentialFingerprintForAPIKey(availableKey)

	provider := &mockQuotaStatusProvider{
		credentialStatuses: map[string]*biz.QuotaChannelStatus{
			exhaustedFingerprint: {Status: providerquotastatus.StatusExhausted, Ready: false},
			availableFingerprint: {Status: providerquotastatus.StatusAvailable, Ready: true},
		},
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}
	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{{Channel: channel}},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{Model: "gpt-4"})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Len(t, got[0].Channel.CredentialViews(), 1)
	require.Equal(t, availableFingerprint, got[0].Channel.CredentialViews()[0].Fingerprint)
	require.Equal(t, availableKey, biz.NewTraceStickyKeyProvider(got[0].Channel).Get(context.Background()))
}

func TestProviderQuotaSelector_FiltersChannelWhenAllCredentialViewsExhausted(t *testing.T) {
	channel := &biz.Channel{Channel: &ent.Channel{
		ID:      1,
		Name:    "all-exhausted",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Credentials: objects.ChannelCredentials{
			APIKeys: []string{"key-1", "key-2"},
		},
	}}

	provider := &mockQuotaStatusProvider{credentialStatuses: map[string]*biz.QuotaChannelStatus{}}
	for _, view := range channel.CredentialViews() {
		provider.credentialStatuses[view.Fingerprint] = &biz.QuotaChannelStatus{
			Status: providerquotastatus.StatusExhausted,
			Ready:  false,
		}
	}
	settings := &mockQuotaEnforcementSettingsProvider{
		settings: &biz.QuotaEnforcementSettings{Enabled: true, Mode: biz.QuotaEnforcementModeExhaustedOnly},
	}
	inner := &mockSelector{
		candidates: []*ChannelModelsCandidate{{Channel: channel}},
	}

	selector := WithProviderQuotaSelector(inner, provider, settings)
	got, err := selector.Select(context.Background(), &llm.Request{Model: "gpt-4"})

	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, 1, selector.FilteredCount)
}

func TestProviderQuotaSelector_DoesNotFilterLocalQuotaScopeWithoutProviderData(t *testing.T) {
	base := &biz.Channel{Channel: &ent.Channel{
		ID:      1,
		Name:    "locally-paused",
		Type:    "openai",
		BaseURL: "https://api.openai.com/v1",
		Credentials: objects.ChannelCredentials{
			APIKeys: []string{"paused-key"},
		},
	}}
	channel := base.WithCredentialViewsForSelection([]biz.ChannelCredentialView{
		{
			CredentialID:              10,
			Fingerprint:               "cred:v1:paused",
			SecretFingerprint:         "secret:v1:paused",
			ResourceScopeKey:          "openai:secret:v1:paused",
			AuthKind:                  "api_key",
			SecretKind:                "api_key",
			KeyHint:                   "paused-key",
			Secret:                    objects.UpstreamCredentialSecretFromAPIKey("paused-key"),
			Enabled:                   true,
			Source:                    biz.ChannelCredentialSourceRef,
			QuotaScopeID:              20,
			QuotaScopeStatus:          "paused",
			QuotaScopeOverLimitAction: "pause",
		},
	})
	selector := WithProviderQuotaSelector(
		&mockSelector{candidates: []*ChannelModelsCandidate{{Channel: channel}}},
		nil,
		&mockQuotaEnforcementSettingsProvider{},
	)

	got, err := selector.Select(context.Background(), &llm.Request{Model: "gpt-4"})

	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, 0, selector.FilteredCount)
}
