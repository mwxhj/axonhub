package biz

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/objects"
)

func testAPIKeyViews(keys ...string) []ChannelCredentialView {
	views := make([]ChannelCredentialView, 0, len(keys))
	for i, key := range keys {
		secret := objects.UpstreamCredentialSecretFromAPIKey(key)
		secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
		views = append(views, ChannelCredentialView{
			CredentialID:      i + 1,
			Name:              fmt.Sprintf("credential-%d", i+1),
			Fingerprint:       ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", key),
			SecretFingerprint: secretFingerprint,
			ResourceScopeKey:  "openai:" + secretFingerprint,
			AuthKind:          channelCredentialAuthKindAPIKey,
			SecretKind:        channelCredentialAuthKindAPIKey,
			KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret),
			QuotaStatus:       "available",
			Secret:            secret,
			Enabled:           true,
			Weight:            1,
			Source:            ChannelCredentialSourceRef,
		})
	}

	return views
}

func testStickyChannel(keys ...string) *Channel {
	views := testAPIKeyViews(keys...)
	return &Channel{
		Channel: &ent.Channel{
			Type:    channel.TypeOpenai,
			BaseURL: "https://api.openai.com/v1",
		},
		cachedCredentialViews: views,
		cachedEnabledAPIKeys:  enabledAPIKeysFromCredentialViews(views),
	}
}

func TestTraceStickyKeyProvider_MultipleKeys_NoTrace(t *testing.T) {
	keys := []string{"key-1", "key-2", "key-3"}
	ch := testStickyChannel(keys...)

	key := NewTraceStickyKeyProvider(ch).Get(context.Background())
	require.Contains(t, keys, key)
}

func TestTraceStickyKeyProvider_MultipleKeys_WithTrace_Sticky(t *testing.T) {
	keys := []string{"key-1", "key-2", "key-3"}
	ch := testStickyChannel(keys...)

	provider := NewTraceStickyKeyProvider(ch)
	ctx := contexts.WithTrace(context.Background(), &ent.Trace{TraceID: "trace-abc-123"})

	key1 := provider.Get(ctx)
	key2 := provider.Get(ctx)
	key3 := provider.Get(ctx)

	require.Equal(t, key1, key2)
	require.Equal(t, key2, key3)
	require.Contains(t, keys, key1)
}

func TestTraceStickyKeyProvider_StickySeedStoresCredentialFingerprint(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2", "key-3")
	provider := NewTraceStickyKeyProvider(ch)
	ctx := contexts.WithCredentialSelectionSeed(context.Background(), "sticky-session-1")

	key1 := provider.Get(ctx)
	key2 := provider.Get(ctx)
	fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx)
	secretFingerprint, okSecret := contexts.GetChannelCredentialSecretFingerprint(ctx)

	require.Equal(t, key1, key2)
	require.True(t, ok)
	require.True(t, okSecret)
	require.Equal(t, ch.CredentialFingerprintForAPIKey(key1), fingerprint)
	require.NotEmpty(t, secretFingerprint)
}

func TestTraceStickyKeyProvider_PrefersCredentialFingerprint(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2", "key-3")
	target := ch.cachedCredentialViews[1]
	ctx := contexts.WithPreferredCredentialFingerprint(context.Background(), target.Fingerprint)

	key := NewTraceStickyKeyProvider(ch).Get(ctx)
	fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx)

	require.Equal(t, "key-2", key)
	require.True(t, ok)
	require.Equal(t, target.Fingerprint, fingerprint)
}

func TestTraceStickyKeyProvider_AllowedCredentialsConstrainSelection(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2", "key-3")
	target := ch.cachedCredentialViews[1]
	ctx := contexts.WithAllowedCredentials(context.Background(), []int{target.CredentialID}, nil)

	key := NewTraceStickyKeyProvider(ch).Get(ctx)
	fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx)

	require.Equal(t, "key-2", key)
	require.True(t, ok)
	require.Equal(t, target.Fingerprint, fingerprint)
}

func TestTraceStickyKeyProvider_AllowedCredentialsDoNotFallbackToDisallowedKeys(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2")
	ctx := contexts.WithAllowedCredentials(context.Background(), nil, []string{"cred:v1:not-present"})

	require.Empty(t, NewTraceStickyKeyProvider(ch).Get(ctx))
}

func TestTraceStickyKeyProvider_ExcludedCredentialsSkipFailedSelection(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2")
	failed := ch.cachedCredentialViews[0]
	target := ch.cachedCredentialViews[1]

	ctx := contexts.WithPreferredCredentialFingerprint(context.Background(), failed.Fingerprint)
	ctx = contexts.WithExcludedCredentials(ctx, []int{failed.CredentialID}, nil)

	key := NewTraceStickyKeyProvider(ch).Get(ctx)
	fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx)

	require.Equal(t, "key-2", key)
	require.True(t, ok)
	require.Equal(t, target.Fingerprint, fingerprint)
}

func TestTraceStickyKeyProvider_ExcludedCredentialsReturnsEmptyWhenAllExcluded(t *testing.T) {
	ch := testStickyChannel("key-1")
	target := ch.cachedCredentialViews[0]
	ctx := contexts.WithExcludedCredentials(context.Background(), []int{target.CredentialID}, nil)

	require.Empty(t, NewTraceStickyKeyProvider(ch).Get(ctx))
}

func TestChannelCredentialFingerprintForAPIKey_DeduplicatesByCredentialScope(t *testing.T) {
	fp1 := ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1/", "shared-key")
	fp2 := ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "shared-key")
	differentKey := ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "other-key")
	differentBase := ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://proxy.example.com/v1", "shared-key")

	require.NotEmpty(t, fp1)
	require.Equal(t, fp1, fp2)
	require.NotEqual(t, fp1, differentKey)
	require.Equal(t, fp1, differentBase)
	require.NotContains(t, fp1, "shared-key")
}

func TestChannelCredentialFingerprintForSecret_LegacyOAuthJSONDoesNotCollapseToEmptyMaterial(t *testing.T) {
	rawOne := `{"access_token":"access-one","refresh_token":"refresh-one"}`
	rawTwo := `{"access_token":"access-two","refresh_token":"refresh-two"}`

	fp1 := ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex/", channelCredentialAuthKindOAuth, objects.UpstreamCredentialSecret{APIKey: rawOne})
	fp2 := ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex", channelCredentialAuthKindOAuth, objects.UpstreamCredentialSecret{APIKey: rawOne})
	different := ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex", channelCredentialAuthKindOAuth, objects.UpstreamCredentialSecret{APIKey: rawTwo})

	require.NotEmpty(t, fp1)
	require.Equal(t, fp1, fp2)
	require.NotEqual(t, fp1, different)
	require.NotContains(t, fp1, "refresh-one")
}

func TestChannelCredentialFingerprintForSecret_OAuthJWTAccountIdentitySurvivesTokenRefresh(t *testing.T) {
	oldToken := unsignedTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-stable",
		},
	})
	newToken := unsignedTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-stable",
		},
	})

	fp1 := ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex", channelCredentialAuthKindOAuth, objects.UpstreamCredentialSecret{
		OAuth: &objects.OAuthCredentials{
			AccessToken:  oldToken,
			RefreshToken: "old-refresh",
		},
	})
	fp2 := ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex", channelCredentialAuthKindOAuth, objects.UpstreamCredentialSecret{
		OAuth: &objects.OAuthCredentials{
			AccessToken:  newToken,
			RefreshToken: "new-refresh",
		},
	})

	require.NotEmpty(t, fp1)
	require.Equal(t, fp1, fp2)
}

func TestTraceStickyKeyProvider_DifferentTraces_MaySelectDifferentKeys(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2", "key-3", "key-4", "key-5")
	provider := NewTraceStickyKeyProvider(ch)

	selectedKeys := make(map[string]bool)
	for i := range 100 {
		trace := &ent.Trace{TraceID: "trace-" + string(rune('A'+i%26)) + "-" + string(rune('0'+i%10))}
		key := provider.Get(contexts.WithTrace(context.Background(), trace))
		selectedKeys[key] = true
	}

	require.Greater(t, len(selectedKeys), 1)
}

func TestTraceStickyKeyProvider_EmptyEnabledKeysReturnsEmpty(t *testing.T) {
	ch := &Channel{
		Channel: &ent.Channel{
			Type:    channel.TypeOpenai,
			BaseURL: "https://api.openai.com/v1",
		},
		cachedCredentialViews: nil,
		cachedEnabledAPIKeys:  nil,
	}

	require.Empty(t, NewTraceStickyKeyProvider(ch).Get(context.Background()))
}

func unsignedTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(claims)
	require.NoError(t, err)

	return header + "." + base64.RawURLEncoding.EncodeToString(payload) + "."
}

func TestTraceStickyKeyProvider_AddKey_MinimalRemapping(t *testing.T) {
	originalKeys := []string{"key-1", "key-2", "key-3"}
	ch := testStickyChannel(originalKeys...)
	provider := NewTraceStickyKeyProvider(ch)

	traces := make([]*ent.Trace, 20)
	for i := range traces {
		traces[i] = &ent.Trace{TraceID: "trace-" + string(rune('A'+i))}
	}

	originalSelections := make(map[string]string)
	for _, trace := range traces {
		originalSelections[trace.TraceID] = provider.Get(contexts.WithTrace(context.Background(), trace))
	}

	newViews := testAPIKeyViews("key-1", "key-2", "key-3", "key-4")
	ch.cachedCredentialViews = newViews
	ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(newViews)

	remappedCount := 0
	for _, trace := range traces {
		newSelection := provider.Get(contexts.WithTrace(context.Background(), trace))
		if originalSelections[trace.TraceID] != newSelection {
			remappedCount++
		}
	}

	require.LessOrEqual(t, remappedCount, len(traces)*2/3)
}

func TestTraceStickyKeyProvider_RemoveKey_MinimalRemapping(t *testing.T) {
	originalKeys := []string{"key-1", "key-2", "key-3", "key-4"}
	ch := testStickyChannel(originalKeys...)
	provider := NewTraceStickyKeyProvider(ch)

	traces := make([]*ent.Trace, 20)
	for i := range traces {
		traces[i] = &ent.Trace{TraceID: "trace-" + string(rune('A'+i))}
	}

	originalSelections := make(map[string]string)
	for _, trace := range traces {
		originalSelections[trace.TraceID] = provider.Get(contexts.WithTrace(context.Background(), trace))
	}

	newViews := testAPIKeyViews("key-1", "key-2", "key-4")
	ch.cachedCredentialViews = newViews
	ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(newViews)

	unaffectedCount := 0
	tracesNotUsingRemovedKey := 0
	for _, trace := range traces {
		newSelection := provider.Get(contexts.WithTrace(context.Background(), trace))
		oldSelection := originalSelections[trace.TraceID]
		if oldSelection != "key-3" {
			tracesNotUsingRemovedKey++
			if oldSelection == newSelection {
				unaffectedCount++
			}
		}
	}

	if tracesNotUsingRemovedKey > 0 {
		require.Equal(t, tracesNotUsingRemovedKey, unaffectedCount)
	}
}

func TestTraceStickyKeyProvider_RemoveKey_Stable(t *testing.T) {
	originalKeys := []string{"key-1", "key-2", "key-3", "key-4"}
	ch := testStickyChannel(originalKeys...)
	trace := &ent.Trace{TraceID: fmt.Sprintf("trace-%d", time.Now().UnixNano())}
	ctx := contexts.WithTrace(context.Background(), trace)
	provider := NewTraceStickyKeyProvider(ch)

	selectedKey1 := provider.Get(ctx)

	otherKeys := lo.Filter(originalKeys, func(k string, _ int) bool {
		return k != selectedKey1
	})

	for _, keyToRemove := range otherKeys {
		newKeys := lo.Filter(originalKeys, func(k string, _ int) bool {
			return k != keyToRemove
		})
		newViews := testAPIKeyViews(newKeys...)
		ch.cachedCredentialViews = newViews
		ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(newViews)

		selectedKey2 := provider.Get(ctx)
		require.Equal(t, selectedKey1, selectedKey2)
	}
}

func TestTraceStickyKeyProvider_AddKey_Stable(t *testing.T) {
	originalKeys := []string{"key-1", "key-2", "key-3"}
	ch := testStickyChannel(originalKeys...)
	trace := &ent.Trace{TraceID: fmt.Sprintf("trace-%d", time.Now().UnixNano())}
	ctx := contexts.WithTrace(context.Background(), trace)
	provider := NewTraceStickyKeyProvider(ch)

	selectedKey1 := provider.Get(ctx)

	newViews := testAPIKeyViews("key-1", "key-2", "key-3", "key-new")
	ch.cachedCredentialViews = newViews
	ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(newViews)

	selectedKey2 := provider.Get(ctx)
	require.Equal(t, selectedKey1, selectedKey2)
}

func TestTraceStickyKeyProvider_DisableKey_SimulatedByRemoval(t *testing.T) {
	allKeys := []string{"key-1", "key-2", "key-3"}
	ch := testStickyChannel(allKeys...)
	provider := NewTraceStickyKeyProvider(ch)

	trace := &ent.Trace{TraceID: "trace-sticky-test"}
	ctx := contexts.WithTrace(context.Background(), trace)

	initialKey := provider.Get(ctx)
	require.Contains(t, allKeys, initialKey)

	ch2 := testStickyChannel("key-1", "key-3")
	provider2 := NewTraceStickyKeyProvider(ch2)

	keyAfterDisable := provider2.Get(ctx)
	require.Contains(t, []string{"key-1", "key-3"}, keyAfterDisable)
	require.NotEqual(t, "key-2", keyAfterDisable)

	if initialKey != "key-2" {
		require.Equal(t, initialKey, keyAfterDisable)
	}
}

func TestTraceStickyKeyProvider_EnableKey_AfterDisable(t *testing.T) {
	ch := testStickyChannel("key-1", "key-3")
	provider := NewTraceStickyKeyProvider(ch)
	trace := &ent.Trace{TraceID: "trace-reenable-test"}
	ctx := contexts.WithTrace(context.Background(), trace)

	_ = provider.Get(ctx)

	newViews := testAPIKeyViews("key-1", "key-2", "key-3")
	ch.cachedCredentialViews = newViews
	ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(newViews)

	keyAfterEnable := provider.Get(ctx)
	require.Contains(t, []string{"key-1", "key-2", "key-3"}, keyAfterEnable)
}

func TestTraceStickyKeyProvider_AllCredentialsFilteredReturnsEmpty(t *testing.T) {
	ch := testStickyChannel("key-1", "key-2")
	for i := range ch.cachedCredentialViews {
		ch.cachedCredentialViews[i].QuotaStatus = "disabled"
	}
	ch.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(ch.cachedCredentialViews)

	require.Empty(t, NewTraceStickyKeyProvider(ch).Get(context.Background()))
}

func TestTraceStickyKeyProvider_KeyOrderIndependence(t *testing.T) {
	keys1 := []string{"key-a", "key-b", "key-c"}
	keys2 := []string{"key-c", "key-a", "key-b"}

	ch1 := testStickyChannel(keys1...)
	ch2 := testStickyChannel(keys2...)
	provider1 := NewTraceStickyKeyProvider(ch1)
	provider2 := NewTraceStickyKeyProvider(ch2)

	trace := &ent.Trace{TraceID: "trace-order-test"}
	ctx := contexts.WithTrace(context.Background(), trace)

	key1 := provider1.Get(ctx)
	key2 := provider2.Get(ctx)
	require.Equal(t, key1, key2)
}

func TestEnabledAPIKeyCredentialViewsHonorsQuota(t *testing.T) {
	views := testAPIKeyViews("key-1", "key-2")
	views[1].QuotaScopeStatus = credentialquotascope.StatusDisabled.String()
	views[1].QuotaScopeSource = credentialquotascope.SourceLocalBudget.String()

	enabled := enabledAPIKeyCredentialViews(views)
	require.Len(t, enabled, 1)
	require.Equal(t, "key-1", enabled[0].Secret.APIKey)
}

func TestRendezvousSelect_Deterministic(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c", "key-d"}
	seed := "test-seed-123"

	result1 := rendezvousSelect(keys, seed)
	result2 := rendezvousSelect(keys, seed)
	result3 := rendezvousSelect(keys, seed)

	require.Equal(t, result1, result2)
	require.Equal(t, result2, result3)
}

func TestRendezvousSelect_DifferentSeeds(t *testing.T) {
	keys := []string{"key-a", "key-b", "key-c", "key-d", "key-e"}
	results := make(map[string]int)

	for i := range 100 {
		seed := "seed-" + string(rune('0'+i%10)) + string(rune('A'+i%26))
		results[rendezvousSelect(keys, seed)]++
	}

	require.Greater(t, len(results), 1)
}

func TestRendezvousSelect_StableWithKeyAddition(t *testing.T) {
	originalKeys := []string{"key-a", "key-b", "key-c"}
	newKeys := []string{"key-a", "key-b", "key-c", "key-d"}
	seeds := []string{"seed1", "seed2", "seed3", "seed4", "seed5"}

	stableCount := 0
	for _, seed := range seeds {
		if rendezvousSelect(originalKeys, seed) == rendezvousSelect(newKeys, seed) {
			stableCount++
		}
	}

	require.GreaterOrEqual(t, stableCount, len(seeds)/2)
}

func TestRendezvousSelect_OnlyAffectedKeysRemap(t *testing.T) {
	originalKeys := []string{"key-a", "key-b", "key-c"}
	newKeys := []string{"key-a", "key-c"}

	seeds := make([]string, 50)
	for i := range seeds {
		seeds[i] = "seed-" + string(rune('A'+i))
	}

	for _, seed := range seeds {
		original := rendezvousSelect(originalKeys, seed)
		after := rendezvousSelect(newKeys, seed)

		if original != "key-b" {
			require.Equal(t, original, after)
		} else {
			require.NotEqual(t, "key-b", after)
		}
	}
}

func TestHash64_Deterministic(t *testing.T) {
	input := "test-input-string"
	h1 := hashAPIKey(input)
	h2 := hashAPIKey(input)
	h3 := hashAPIKey(input)

	require.Equal(t, h1, h2)
	require.Equal(t, h2, h3)
}

func TestHash64_DifferentInputs(t *testing.T) {
	h1 := hashAPIKey("input-1")
	h2 := hashAPIKey("input-2")
	h3 := hashAPIKey("input-3")

	require.NotEqual(t, h1, h2)
	require.NotEqual(t, h2, h3)
	require.NotEqual(t, h1, h3)
}
