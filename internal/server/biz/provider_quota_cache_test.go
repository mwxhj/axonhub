package biz

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/stretchr/testify/assert"
)

func TestProviderQuotaService_GetQuotaStatus_ReturnsCorrectData(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	svc.quotaCache.Store(1, &QuotaChannelStatus{Status: providerquotastatus.StatusAvailable, Ready: true})
	svc.quotaCache.Store(2, &QuotaChannelStatus{Status: providerquotastatus.StatusExhausted, Ready: false})
	svc.quotaCache.Store(3, &QuotaChannelStatus{Status: providerquotastatus.StatusWarning, Ready: true})

	status1 := svc.GetQuotaStatus(1)
	assert.NotNil(t, status1)
	assert.Equal(t, providerquotastatus.StatusAvailable, status1.Status)
	assert.True(t, status1.Ready)

	status2 := svc.GetQuotaStatus(2)
	assert.NotNil(t, status2)
	assert.Equal(t, providerquotastatus.StatusExhausted, status2.Status)
	assert.False(t, status2.Ready)

	status3 := svc.GetQuotaStatus(3)
	assert.NotNil(t, status3)
	assert.Equal(t, providerquotastatus.StatusWarning, status3.Status)
	assert.True(t, status3.Ready)
}

func TestProviderQuotaService_GetQuotaStatus_UnknownChannel(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	status := svc.GetQuotaStatus(999)
	assert.Nil(t, status)
}

func TestProviderQuotaService_UpdateQuotaCache(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	svc.updateQuotaCache(1, providerquotastatus.StatusAvailable, true, nil)
	svc.updateQuotaCache(2, providerquotastatus.StatusExhausted, false, nil)

	status1 := svc.GetQuotaStatus(1)
	assert.NotNil(t, status1)
	assert.Equal(t, providerquotastatus.StatusAvailable, status1.Status)
	assert.True(t, status1.Ready)

	status2 := svc.GetQuotaStatus(2)
	assert.NotNil(t, status2)
	assert.Equal(t, providerquotastatus.StatusExhausted, status2.Status)
	assert.False(t, status2.Ready)
}

func TestProviderQuotaService_UpdateQuotaCache_Overwrite(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	svc.updateQuotaCache(1, providerquotastatus.StatusAvailable, true, nil)
	svc.updateQuotaCache(1, providerquotastatus.StatusExhausted, false, nil)

	status := svc.GetQuotaStatus(1)
	assert.NotNil(t, status)
	assert.Equal(t, providerquotastatus.StatusExhausted, status.Status)
	assert.False(t, status.Ready)
}

func TestProviderQuotaService_ConcurrentAccess(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	var wg sync.WaitGroup
	const goroutines = 50

	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			svc.updateQuotaCache(id, providerquotastatus.StatusAvailable, true, nil)
		}(i)
	}

	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			_ = svc.GetQuotaStatus(id)
		}(i)
	}

	wg.Wait()

	for i := range goroutines {
		status := svc.GetQuotaStatus(i)
		assert.NotNil(t, status, "channel %d should have quota status", i)
		assert.Equal(t, providerquotastatus.StatusAvailable, status.Status)
		assert.True(t, status.Ready)
	}
}

func TestProviderQuotaService_ConcurrentReadWrite(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	svc.updateQuotaCache(1, providerquotastatus.StatusAvailable, true, nil)

	var wg sync.WaitGroup
	const iterations = 100

	wg.Add(iterations)
	for range iterations {
		go func() {
			defer wg.Done()
			svc.updateQuotaCache(1, providerquotastatus.StatusExhausted, false, nil)
		}()
	}

	wg.Add(iterations)
	for range iterations {
		go func() {
			defer wg.Done()
			_ = svc.GetQuotaStatus(1)
		}()
	}

	wg.Wait()

	status := svc.GetQuotaStatus(1)
	assert.NotNil(t, status)
	assert.Equal(t, providerquotastatus.StatusExhausted, status.Status)
	assert.False(t, status.Ready)
}

func TestProviderQuotaService_UpdateQuotaCache_WithLimits(t *testing.T) {
	svc := &ProviderQuotaService{
		quotaCache: sync.Map{},
	}

	limits := []provider_quota.QuotaLimitStatus{
		{Type: provider_quota.QuotaLimitTypeToken, Status: "available", UsageRatio: 0.3, Ready: true},
		{Type: provider_quota.QuotaLimitTypeImage, Status: "exhausted", UsageRatio: 1.0, Ready: false},
	}

	svc.updateQuotaCache(1, providerquotastatus.StatusWarning, true, limits)

	status := svc.GetQuotaStatus(1)
	assert.NotNil(t, status)
	assert.Equal(t, providerquotastatus.StatusWarning, status.Status)
	assert.True(t, status.Ready)
	assert.Len(t, status.Limits, 2)

	assert.Equal(t, provider_quota.QuotaLimitTypeToken, status.Limits[0].Type)
	assert.Equal(t, "available", status.Limits[0].Status)
	assert.InDelta(t, 0.3, status.Limits[0].UsageRatio, 0.001)
	assert.True(t, status.Limits[0].Ready)

	assert.Equal(t, provider_quota.QuotaLimitTypeImage, status.Limits[1].Type)
	assert.Equal(t, "exhausted", status.Limits[1].Status)
	assert.InDelta(t, 1.0, status.Limits[1].UsageRatio, 0.001)
	assert.False(t, status.Limits[1].Ready)

	effectiveStatus, ready := status.EffectiveStatus(provider_quota.QuotaLimitTypeImage)
	assert.Equal(t, providerquotastatus.StatusExhausted, effectiveStatus)
	assert.False(t, ready)

	effectiveStatus, ready = status.EffectiveStatus(provider_quota.QuotaLimitTypeToken)
	assert.Equal(t, providerquotastatus.StatusAvailable, effectiveStatus)
	assert.True(t, ready)
}

func TestMergeAndExtractLimitsRoundTrip(t *testing.T) {
	svc := &ProviderQuotaService{}
	resetAt := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)

	t.Run("basic round trip", func(t *testing.T) {
		quotaData := provider_quota.QuotaData{
			Status:       "available",
			ProviderType: "test",
			RawData:      map[string]any{"key": "value"},
			Limits: []provider_quota.QuotaLimitStatus{
				{Type: provider_quota.QuotaLimitTypeToken, Status: "available", UsageRatio: 0.3, Ready: true},
				{Type: provider_quota.QuotaLimitTypeImage, Status: "exhausted", UsageRatio: 1.0, Ready: false, NextResetAt: &resetAt},
			},
		}

		merged := svc.mergeLimitsIntoQuotaData(quotaData)
		extracted := extractLimitsFromQuotaData(merged)

		assert.Len(t, extracted, 2)
		tokenLimits := lo.Filter(extracted, func(l provider_quota.QuotaLimitStatus, _ int) bool {
			return l.Type == provider_quota.QuotaLimitTypeToken
		})
		assert.Len(t, tokenLimits, 1)
		assert.Equal(t, "available", tokenLimits[0].Status)
		assert.InDelta(t, 0.3, tokenLimits[0].UsageRatio, 0.001)
		assert.True(t, tokenLimits[0].Ready)
		assert.Nil(t, tokenLimits[0].NextResetAt)

		imageLimits := lo.Filter(extracted, func(l provider_quota.QuotaLimitStatus, _ int) bool {
			return l.Type == provider_quota.QuotaLimitTypeImage
		})
		assert.Len(t, imageLimits, 1)
		assert.Equal(t, "exhausted", imageLimits[0].Status)
		assert.InDelta(t, 1.0, imageLimits[0].UsageRatio, 0.001)
		assert.False(t, imageLimits[0].Ready)
		assert.NotNil(t, imageLimits[0].NextResetAt)
		assert.Equal(t, resetAt, *imageLimits[0].NextResetAt)

		assert.Equal(t, "value", merged["key"])
	})

	t.Run("empty limits", func(t *testing.T) {
		quotaData := provider_quota.QuotaData{
			Status:       "available",
			ProviderType: "test",
		}

		merged := svc.mergeLimitsIntoQuotaData(quotaData)
		extracted := extractLimitsFromQuotaData(merged)

		assert.Nil(t, extracted)
	})

	t.Run("preserves raw data", func(t *testing.T) {
		quotaData := provider_quota.QuotaData{
			Status:       "available",
			ProviderType: "test",
			RawData:      map[string]any{"existing": "data"},
			Limits: []provider_quota.QuotaLimitStatus{
				{Type: provider_quota.QuotaLimitTypeToken, Status: "available", UsageRatio: 0.5, Ready: true},
			},
		}

		merged := svc.mergeLimitsIntoQuotaData(quotaData)
		assert.Equal(t, "data", merged["existing"])
		assert.NotNil(t, merged["_limits"])
	})
}

func TestProviderQuotaService_SaveQuotaStatusPersistsCredentialObservationOnly(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	ch, err := client.Channel.Create().
		SetName("nanogpt").
		SetType(channel.TypeNanogpt).
		SetBaseURL("https://nano-gpt.com/api").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	scope, err := client.CredentialQuotaScope.Create().
		SetName("provider scope").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitRequest).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-provider-quota")
	credential, err := client.UpstreamCredential.Create().
		SetName("provider key").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("nanogpt").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("nanogpt", channelCredentialAuthKindAPIKey, secret)).
		SetSecretFingerprint(CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetQuotaScopeID(scope.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	svc := &ProviderQuotaService{
		AbstractService:           &AbstractService{db: client},
		quotaCache:                sync.Map{},
		credentialIDCache:         sync.Map{},
		credentialQuotaCache:      sync.Map{},
		resourceScopeCache:        sync.Map{},
		checkInterval:             time.Minute,
		checkers:                  map[string]provider_quota.QuotaChecker{},
		SystemService:             nil,
		httpClient:                nil,
		warningCheckIntervalRatio: 1,
	}
	resetAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	target := quotaCredentialTarget{
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		SecretFingerprint:     *credential.SecretFingerprint,
		ResourceScopeKey:      ChannelCredentialResourceScopeKey(ch, *credential.SecretFingerprint),
		QuotaScopeID:          scope.ID,
	}

	svc.saveQuotaStatus(ctx, ch.ID, target, "nanogpt", provider_quota.QuotaData{
		Status:      string(providerquotastatus.StatusExhausted),
		Ready:       false,
		NextResetAt: &resetAt,
	}, time.Now())

	status, err := client.ProviderQuotaStatus.Query().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, credential.ID, status.CredentialID)
	require.Equal(t, credential.Fingerprint, status.CredentialFingerprint)
	require.Equal(t, *credential.SecretFingerprint, status.SecretFingerprint)
	require.Equal(t, target.ResourceScopeKey, status.ResourceScopeKey)
	require.Equal(t, scope.ID, status.QuotaScopeID)

	updatedCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, string(providerquotastatus.StatusExhausted), updatedCredential.QuotaStatus)
	require.Empty(t, updatedCredential.LastError)

	updatedScope, err := client.CredentialQuotaScope.Get(ctx, scope.ID)
	require.NoError(t, err)
	require.Equal(t, credentialquotascope.StatusAvailable, updatedScope.Status)
	require.Equal(t, credentialquotascope.SourceLocalBudget, updatedScope.Source)
	require.Nil(t, updatedScope.ResetAt)

	cached := svc.GetCredentialQuotaStatusByID(credential.ID)
	require.NotNil(t, cached)
	require.Equal(t, target.ResourceScopeKey, cached.ResourceScopeKey)
	require.Equal(t, scope.ID, cached.QuotaScopeID)
}

func TestProviderQuotaService_SaveQuotaStatusKeepsCredentialScopedAndChannelScopedRowsSeparate(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	ch, err := client.Channel.Create().
		SetName("nanogpt").
		SetType(channel.TypeNanogpt).
		SetBaseURL("https://nano-gpt.com/api").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	svc := &ProviderQuotaService{
		AbstractService:           &AbstractService{db: client},
		quotaCache:                sync.Map{},
		credentialIDCache:         sync.Map{},
		credentialQuotaCache:      sync.Map{},
		resourceScopeCache:        sync.Map{},
		checkInterval:             time.Minute,
		checkers:                  map[string]provider_quota.QuotaChecker{},
		warningCheckIntervalRatio: 1,
	}

	svc.saveQuotaStatus(ctx, ch.ID, quotaCredentialTarget{
		CredentialID:          123,
		CredentialFingerprint: "cred:v1:test",
		SecretFingerprint:     "secret:v1:test",
		ResourceScopeKey:      "nanogpt:secret:v1:test",
		QuotaScopeID:          456,
	}, "nanogpt", provider_quota.QuotaData{
		Status: string(providerquotastatus.StatusAvailable),
		Ready:  true,
	}, time.Now())

	svc.saveQuotaStatus(ctx, ch.ID, quotaCredentialTarget{}, "nanogpt", provider_quota.QuotaData{
		Status: string(providerquotastatus.StatusWarning),
		Ready:  true,
	}, time.Now())

	statuses, err := client.ProviderQuotaStatus.Query().All(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, 2)

	credentialStatus, err := client.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ScopeKey("resource:nanogpt:secret:v1:test"),
		).
		Only(ctx)
	require.NoError(t, err)
	require.Equal(t, 123, credentialStatus.CredentialID)
	require.Equal(t, "cred:v1:test", credentialStatus.CredentialFingerprint)
	require.Equal(t, "secret:v1:test", credentialStatus.SecretFingerprint)
	require.Equal(t, "nanogpt:secret:v1:test", credentialStatus.ResourceScopeKey)
	require.Equal(t, 456, credentialStatus.QuotaScopeID)
	require.Equal(t, providerquotastatus.StatusAvailable, credentialStatus.Status)

	status, err := client.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ScopeKey(providerQuotaChannelScopeKey(ch.ID)),
		).
		Only(ctx)
	require.NoError(t, err)
	require.Zero(t, status.CredentialID)
	require.Empty(t, status.CredentialFingerprint)
	require.Empty(t, status.SecretFingerprint)
	require.Empty(t, status.ResourceScopeKey)
	require.Zero(t, status.QuotaScopeID)
	require.Equal(t, providerquotastatus.StatusWarning, status.Status)
}

func TestProviderQuotaService_BackfillProviderQuotaStatusScopeKeysMigratesLegacyChannelRows(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	ch, err := client.Channel.Create().
		SetName("nanogpt").
		SetType(channel.TypeNanogpt).
		SetBaseURL("https://nano-gpt.com/api").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	legacyStatus, err := client.ProviderQuotaStatus.Create().
		SetChannelID(ch.ID).
		SetScopeKey(providerQuotaLegacyChannelScopeKey).
		SetProviderType(providerquotastatus.ProviderTypeNanogpt).
		SetStatus(providerquotastatus.StatusWarning).
		SetQuotaData(map[string]any{"source": "legacy"}).
		SetReady(true).
		SetNextCheckAt(now.Add(time.Hour)).
		SetCreatedAt(now.Add(-time.Hour)).
		SetUpdatedAt(now.Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	svc := &ProviderQuotaService{
		AbstractService: &AbstractService{db: client},
	}

	payload, err := svc.BackfillProviderQuotaStatusScopeKeys(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, payload.ScannedStatuses)
	require.Equal(t, 1, payload.UpdatedStatuses)
	require.Zero(t, payload.SkippedStatuses)

	updated, err := client.ProviderQuotaStatus.Get(ctx, legacyStatus.ID)
	require.NoError(t, err)
	require.Equal(t, providerQuotaChannelScopeKey(ch.ID), updated.ScopeKey)
	require.Equal(t, ch.ID, updated.ChannelID)

	legacyCount, err := client.ProviderQuotaStatus.Query().
		Where(
			providerquotastatus.ScopeKey(providerQuotaLegacyChannelScopeKey),
			providerquotastatus.ChannelID(ch.ID),
		).
		Count(ctx)
	require.NoError(t, err)
	require.Zero(t, legacyCount)
}

func TestProviderQuotaService_LoadQuotaCacheUsesLatestProviderScopeRow(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	ch, err := client.Channel.Create().
		SetName("nanogpt").
		SetType(channel.TypeNanogpt).
		SetBaseURL("https://nano-gpt.com/api").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC().Truncate(time.Second)
	scopeKey := providerQuotaChannelScopeKey(ch.ID)
	_, err = client.ProviderQuotaStatus.Create().
		SetChannelID(ch.ID).
		SetScopeKey(scopeKey).
		SetProviderType(providerquotastatus.ProviderTypeNanogpt).
		SetStatus(providerquotastatus.StatusAvailable).
		SetQuotaData(map[string]any{"version": "old"}).
		SetReady(true).
		SetNextCheckAt(now.Add(time.Hour)).
		SetCreatedAt(now.Add(-2 * time.Hour)).
		SetUpdatedAt(now.Add(-2 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ProviderQuotaStatus.Create().
		SetChannelID(ch.ID).
		SetScopeKey(scopeKey).
		SetProviderType(providerquotastatus.ProviderTypeNanogpt).
		SetStatus(providerquotastatus.StatusExhausted).
		SetQuotaData(map[string]any{"version": "new"}).
		SetReady(false).
		SetNextCheckAt(now.Add(time.Hour)).
		SetCreatedAt(now.Add(-time.Hour)).
		SetUpdatedAt(now.Add(-time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	svc := &ProviderQuotaService{
		AbstractService: &AbstractService{db: client},
		quotaCache:      sync.Map{},
	}

	svc.loadQuotaCache(ctx)

	status := svc.GetQuotaStatus(ch.ID)
	require.NotNil(t, status)
	require.Equal(t, providerquotastatus.StatusExhausted, status.Status)
	require.False(t, status.Ready)
}
