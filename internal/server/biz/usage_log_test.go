package biz

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestUsageLogService_CreateUsageLog_PromptWriteCachedTokens(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetModelID("test-model").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	require.NoError(t, systemService.SetGeneralSettings(ctx, SystemGeneralSettings{
		CurrencyCode:                  "USD",
		Timezone:                      "Asia/Shanghai",
		CredentialQuotaDailyResetTime: "09:30",
	}))
	channelService := NewChannelServiceForTest(client)
	svc := NewUsageLogService(client, systemService, channelService)

	usage := &llm.Usage{
		PromptTokens:     10,
		CompletionTokens: 20,
		TotalTokens:      30,
		PromptTokensDetails: &llm.PromptTokensDetails{
			CachedTokens:      2,
			WriteCachedTokens: 3,
		},
	}

	created, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:             req.ID,
		ProjectID:             p.ID,
		ChannelID:             0,
		ActualModelID:         "test-model",
		CredentialFingerprint: "cred:v1:test",
		Usage:                 usage,
		Source:                usagelog.SourceAPI,
		Format:                "openai/chat_completions",
		APIKeyID:              nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	require.Equal(t, int64(2), created.PromptCachedTokens)
	require.Equal(t, int64(3), created.PromptWriteCachedTokens)
	require.Equal(t, "cred:v1:test", created.CredentialFingerprint)
}

func TestUsageLogService_CreateUsageLogStoresCredentialScopeAndAccountsQuota(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetModelID("test-model").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("token budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		SetLimitAmount("40").
		SetUsedAmount("10").
		SetWarningThresholdPercent(50).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	require.NoError(t, systemService.SetGeneralSettings(ctx, SystemGeneralSettings{
		CurrencyCode:                  "USD",
		Timezone:                      "Asia/Shanghai",
		CredentialQuotaDailyResetTime: "09:30",
	}))
	channelService := NewChannelServiceForTest(client)
	svc := NewUsageLogService(client, systemService, channelService)

	created, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:             req.ID,
		ProjectID:             p.ID,
		ChannelID:             0,
		ActualModelID:         "test-model",
		CredentialID:          123,
		CredentialFingerprint: "cred:v1:test",
		SecretFingerprint:     "secret:v1:test",
		ResourceScopeKey:      "openai:secret:v1:test",
		CredentialName:        "OpenAI key",
		CredentialKeyHint:     "sk-...-test",
		CredentialSource:      ChannelCredentialSourceRef,
		CredentialQuotaStatus: "available",
		QuotaScopeID:          quotaScope.ID,
		QuotaScopeName:        quotaScope.Name,
		QuotaScopeStatus:      quotaScope.Status.String(),
		Usage: &llm.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
		Source:   usagelog.SourceAPI,
		Format:   "openai/chat_completions",
		APIKeyID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)
	require.Equal(t, "cred:v1:test", created.CredentialFingerprint)
	require.Equal(t, "secret:v1:test", created.SecretFingerprint)
	require.Equal(t, "openai:secret:v1:test", created.ResourceScopeKey)
	require.Equal(t, "OpenAI key", created.CredentialNameSnapshot)
	require.Equal(t, "sk-...-test", created.CredentialKeyHint)
	require.Equal(t, ChannelCredentialSourceRef, created.CredentialSource)
	require.Equal(t, "available", created.CredentialQuotaStatusSnapshot)
	require.Equal(t, quotaScope.ID, created.QuotaScopeID)
	require.Equal(t, quotaScope.Name, created.QuotaScopeNameSnapshot)
	require.Equal(t, quotaScope.Status.String(), created.QuotaScopeStatusSnapshot)

	updatedScope, err := client.CredentialQuotaScope.Get(ctx, quotaScope.ID)
	require.NoError(t, err)
	require.Equal(t, "40", updatedScope.UsedAmount)
	require.Equal(t, credentialquotascope.StatusWarning, updatedScope.Status)
}

func TestUsageLogService_CreateUsageLogRefreshesCachedCredentialQuotaUsage(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	previousAsyncReloadDisabled := asyncReloadDisabled
	asyncReloadDisabled = false
	defer func() { asyncReloadDisabled = previousAsyncReloadDisabled }()

	p, req, quotaScope, ch, channelService := setupUsageLogQuotaCachedChannel(t, ctx, client, "10")
	defer channelService.Stop()

	cached := channelService.GetEnabledChannel(ch.ID)
	require.NotNil(t, cached)
	require.Equal(t, "10", cached.CredentialViews()[0].QuotaScopeUsedAmount)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	svc := NewUsageLogService(client, systemService, channelService)

	_, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     p.ID,
		ChannelID:     ch.ID,
		ActualModelID: "test-model",
		QuotaScopeID:  quotaScope.ID,
		Usage: &llm.Usage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
		Source:   usagelog.SourceAPI,
		Format:   "openai/chat_completions",
		APIKeyID: nil,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		cached := channelService.GetEnabledChannel(ch.ID)
		if cached == nil {
			return false
		}
		views := cached.CredentialViews()
		return len(views) == 1 && views[0].QuotaScopeUsedAmount == "15"
	}, 2*time.Second, 25*time.Millisecond)
}

func TestChannelService_ReloadEnabledChannelsCacheDetectsQuotaScopeUsageUpdate(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	previousAsyncReloadDisabled := asyncReloadDisabled
	asyncReloadDisabled = true
	defer func() { asyncReloadDisabled = previousAsyncReloadDisabled }()

	p, req, quotaScope, ch, channelService := setupUsageLogQuotaCachedChannel(t, ctx, client, "10")
	defer channelService.Stop()

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	svc := NewUsageLogService(client, systemService, channelService)

	_, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     p.ID,
		ChannelID:     ch.ID,
		ActualModelID: "test-model",
		QuotaScopeID:  quotaScope.ID,
		Usage: &llm.Usage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
		Source:   usagelog.SourceAPI,
		Format:   "openai/chat_completions",
		APIKeyID: nil,
	})
	require.NoError(t, err)

	cached := channelService.GetEnabledChannel(ch.ID)
	require.NotNil(t, cached)
	require.Equal(t, "10", cached.CredentialViews()[0].QuotaScopeUsedAmount)

	require.NoError(t, channelService.enabledChannelsCache.Load(ctx, false))

	cached = channelService.GetEnabledChannel(ch.ID)
	require.NotNil(t, cached)
	require.Equal(t, "15", cached.CredentialViews()[0].QuotaScopeUsedAmount)
}

func TestUsageLogService_CreateUsageLogResetsExpiredQuotaWindowBeforeAccounting(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetModelID("test-model").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	startedAt := time.Now().Add(-25 * time.Hour).UTC().Truncate(time.Second)
	expiredResetAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	pauseUntil := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("daily token budget").
		SetStatus(credentialquotascope.StatusPaused).
		SetUnit(credentialquotascope.UnitToken).
		SetLimitAmount("100").
		SetUsedAmount("90").
		SetResetPolicy(credentialquotascope.ResetPolicyDaily).
		SetResetAt(expiredResetAt).
		SetWindowStartedAt(startedAt).
		SetOverLimitAction(credentialquotascope.OverLimitActionPause).
		SetPauseUntil(pauseUntil).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	require.NoError(t, systemService.SetGeneralSettings(ctx, SystemGeneralSettings{
		CurrencyCode:                  "USD",
		Timezone:                      "Asia/Shanghai",
		CredentialQuotaDailyResetTime: "09:30",
	}))
	channelService := NewChannelServiceForTest(client)
	svc := NewUsageLogService(client, systemService, channelService)

	created, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     p.ID,
		ChannelID:     0,
		ActualModelID: "test-model",
		QuotaScopeID:  quotaScope.ID,
		Usage: &llm.Usage{
			PromptTokens: 5,
			TotalTokens:  5,
		},
		Source:   usagelog.SourceAPI,
		Format:   "openai/chat_completions",
		APIKeyID: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	updatedScope, err := client.CredentialQuotaScope.Get(ctx, quotaScope.ID)
	require.NoError(t, err)
	require.Equal(t, "5", updatedScope.UsedAmount)
	require.Equal(t, credentialquotascope.StatusAvailable, updatedScope.Status)
	require.NotNil(t, updatedScope.ResetAt)
	require.True(t, updatedScope.ResetAt.After(time.Now()))
	localReset := updatedScope.ResetAt.In(time.FixedZone("UTC+8", 8*60*60))
	require.Equal(t, 9, localReset.Hour())
	require.Equal(t, 30, localReset.Minute())
	require.NotNil(t, updatedScope.WindowStartedAt)
	require.True(t, updatedScope.WindowStartedAt.After(startedAt))
	require.Nil(t, updatedScope.PauseUntil)
}

func setupUsageLogQuotaCachedChannel(
	t *testing.T,
	ctx context.Context,
	client *ent.Client,
	usedAmount string,
) (*ent.Project, *ent.Request, *ent.CredentialQuotaScope, *ent.Channel, *ChannelService) {
	t.Helper()

	baseTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)

	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		SetUpdatedAt(baseTime).
		Save(ctx)
	require.NoError(t, err)

	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("token budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		SetLimitAmount("100").
		SetUsedAmount(usedAmount).
		SetResetPolicy(credentialquotascope.ResetPolicyDaily).
		SetResetAt(time.Now().Add(24 * time.Hour)).
		SetSource(credentialquotascope.SourceLocalBudget).
		SetUpdatedAt(baseTime).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-quota-cache")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	fingerprint := CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("quota cache key").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(fingerprint).
		SetSecretFingerprint(secretFingerprint).
		SetQuotaScopeID(quotaScope.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		SetUpdatedAt(baseTime).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetName("quota-cache-channel").
		SetType(channel.TypeOpenai).
		SetStatus(channel.StatusEnabled).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"test-model"}).
		SetDefaultTestModel("test-model").
		SetUpdatedAt(baseTime).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		SetUpdatedAt(baseTime).
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetChannelID(ch.ID).
		SetModelID("test-model").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	channelService := NewChannelService(ChannelServiceParams{
		CacheConfig: xcache.Config{Mode: xcache.ModeMemory},
		Ent:         client,
		SystemService: &SystemService{
			AbstractService: &AbstractService{db: client},
			Cache:           xcache.NewFromConfig[ent.System](xcache.Config{Mode: xcache.ModeMemory}),
		},
		HttpClient: httpclient.NewHttpClient(),
	})

	return p, req, quotaScope, ch, channelService
}

func TestUsageLogService_CreateUsageLog_WithPriceReferenceID(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	// Create project
	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	// Create channel
	ch, err := client.Channel.Create().
		SetName("test-channel").
		SetType("openai").
		SetBaseURL("https://api.openai.com/v1").
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetCredentials(objects.ChannelCredentials{}).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("test-key")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("test-credential").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "test-key")).
		SetSecretFingerprint(secretFingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	// Create model price with reference ID
	_, err = client.ChannelModelPrice.Create().
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetReferenceID("test-ref-123").
		SetPrice(objects.ModelPrice{
			Items: []objects.ModelPriceItem{
				{
					ItemCode: objects.PriceItemCodeUsage,
					Pricing: objects.Pricing{
						Mode:         objects.PricingModeUsagePerUnit,
						UsagePerUnit: toDecimalPtr("0.03"),
					},
				},
				{
					ItemCode: objects.PriceItemCodeCompletion,
					Pricing: objects.Pricing{
						Mode:         objects.PricingModeUsagePerUnit,
						UsagePerUnit: toDecimalPtr("0.06"),
					},
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	// Create request
	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	channelService := NewChannelServiceForTest(client)

	reloaded, err := client.Channel.Query().
		Where(channel.IDEQ(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	// Preload the channel with model prices
	enabledCh, err := channelService.buildChannelWithTransformer(reloaded)
	require.NoError(t, err)
	channelService.preloadModelPrices(ctx, enabledCh)

	// Add to enabled channels list so it can be found by GetEnabledChannel
	channelService.SetEnabledChannelsForTest([]*Channel{enabledCh})

	// Verify cache contains the model price
	require.NotNil(t, enabledCh.cachedModelPrices["gpt-4"])
	require.Equal(t, "test-ref-123", enabledCh.cachedModelPrices["gpt-4"].ReferenceID)

	svc := NewUsageLogService(client, systemService, channelService)

	// Create usage log with price calculation
	usage := &llm.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
	}

	channelID := ch.ID
	created, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     p.ID,
		ChannelID:     channelID,
		ActualModelID: "gpt-4",
		Usage:         usage,
		Source:        usagelog.SourceAPI,
		Format:        "openai/chat_completions",
		APIKeyID:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	// Verify price_reference_id is set
	require.Equal(t, "test-ref-123", created.CostPriceReferenceID)
	require.NotNil(t, created.TotalCost)
	require.NotEmpty(t, created.CostItems)

	// Verify cost calculation is correct
	// (1000 / 1_000_000) * 0.03 + (500 / 1_000_000) * 0.06 = 0.00003 + 0.00003 = 0.00006
	require.InDelta(t, 0.00006, *created.TotalCost, 0.0000001)
}

func TestUsageLogService_CreateUsageLog_WithCachedTokens(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	// Create project
	p, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	// Create channel
	ch, err := client.Channel.Create().
		SetName("test-channel").
		SetType("openai").
		SetBaseURL("https://api.openai.com/v1").
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetCredentials(objects.ChannelCredentials{}).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("test-key")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("test-credential").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "test-key")).
		SetSecretFingerprint(secretFingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	// Create model price with reference ID
	// Input tokens: $0.03 per 1M tokens
	// Completion tokens: $0.06 per 1M tokens
	// Cached tokens: $0.015 per 1M tokens (50% discount)
	_, err = client.ChannelModelPrice.Create().
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetReferenceID("test-ref-cached").
		SetPrice(objects.ModelPrice{
			Items: []objects.ModelPriceItem{
				{
					ItemCode: objects.PriceItemCodeUsage,
					Pricing: objects.Pricing{
						Mode:         objects.PricingModeUsagePerUnit,
						UsagePerUnit: toDecimalPtr("0.03"),
					},
				},
				{
					ItemCode: objects.PriceItemCodeCompletion,
					Pricing: objects.Pricing{
						Mode:         objects.PricingModeUsagePerUnit,
						UsagePerUnit: toDecimalPtr("0.06"),
					},
				},
				{
					ItemCode: objects.PriceItemCodePromptCachedToken,
					Pricing: objects.Pricing{
						Mode:         objects.PricingModeUsagePerUnit,
						UsagePerUnit: toDecimalPtr("0.015"),
					},
				},
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	// Create request
	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{
		CacheConfig: xcache.Config{},
		Ent:         client,
	})
	channelService := NewChannelServiceForTest(client)

	reloaded, err := client.Channel.Query().
		Where(channel.IDEQ(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	// Preload the channel with model prices
	enabledCh, err := channelService.buildChannelWithTransformer(reloaded)
	require.NoError(t, err)
	channelService.preloadModelPrices(ctx, enabledCh)

	// Add to enabled channels list so it can be found by GetEnabledChannel
	channelService.SetEnabledChannelsForTest([]*Channel{enabledCh})

	// Verify cache contains the model price
	require.NotNil(t, enabledCh.cachedModelPrices["gpt-4"])
	require.Equal(t, "test-ref-cached", enabledCh.cachedModelPrices["gpt-4"].ReferenceID)

	svc := NewUsageLogService(client, systemService, channelService)

	// Create usage log with cached tokens
	// Total prompt tokens: 1000 (includes 300 cached tokens)
	// Billable prompt tokens: 700 (1000 - 300)
	// Cached tokens: 300 (read from cache, charged at discounted rate)
	// Completion tokens: 500
	usage := &llm.Usage{
		PromptTokens:     1000,
		CompletionTokens: 500,
		TotalTokens:      1500,
		PromptTokensDetails: &llm.PromptTokensDetails{
			CachedTokens: 300,
		},
	}

	channelID := ch.ID
	created, err := svc.CreateUsageLog(ctx, CreateUsageLogParams{
		RequestID:     req.ID,
		ProjectID:     p.ID,
		ChannelID:     channelID,
		ActualModelID: "gpt-4",
		Usage:         usage,
		Source:        usagelog.SourceAPI,
		Format:        "openai/chat_completions",
		APIKeyID:      nil,
	})
	require.NoError(t, err)
	require.NotNil(t, created)

	// Verify price_reference_id is set
	require.Equal(t, "test-ref-cached", created.CostPriceReferenceID)
	require.NotNil(t, created.TotalCost)
	require.NotEmpty(t, created.CostItems)

	// Verify cost calculation excludes cached tokens from input cost
	// Expected cost:
	// - Input tokens (billable): (700 / 1_000_000) * 0.03 = 0.000021
	// - Cached tokens: (300 / 1_000_000) * 0.015 = 0.0000045
	// - Completion tokens: (500 / 1_000_000) * 0.06 = 0.00003
	// Total: 0.000021 + 0.0000045 + 0.00003 = 0.0000555
	expectedCost := 0.0000555
	require.InDelta(t, expectedCost, *created.TotalCost, 0.0000001)

	// Verify cost items breakdown
	require.Len(t, created.CostItems, 3)

	// Find each cost item and verify
	var inputItem, cachedItem, completionItem *objects.CostItem

	for i := range created.CostItems {
		switch created.CostItems[i].ItemCode {
		case objects.PriceItemCodeUsage:
			inputItem = &created.CostItems[i]
		case objects.PriceItemCodePromptCachedToken:
			cachedItem = &created.CostItems[i]
		case objects.PriceItemCodeCompletion:
			completionItem = &created.CostItems[i]
		}
	}

	require.NotNil(t, inputItem, "input cost item should exist")
	require.NotNil(t, cachedItem, "cached cost item should exist")
	require.NotNil(t, completionItem, "completion cost item should exist")

	// Verify input tokens quantity excludes cached tokens
	require.Equal(t, int64(700), inputItem.Quantity, "input quantity should be 700 (1000 - 300 cached)")
	require.InDelta(t, 0.000021, inputItem.Subtotal.InexactFloat64(), 0.0000001)

	// Verify cached tokens quantity
	require.Equal(t, int64(300), cachedItem.Quantity, "cached quantity should be 300")
	require.InDelta(t, 0.0000045, cachedItem.Subtotal.InexactFloat64(), 0.0000001)

	// Verify completion tokens quantity
	require.Equal(t, int64(500), completionItem.Quantity, "completion quantity should be 500")
	require.InDelta(t, 0.00003, completionItem.Subtotal.InexactFloat64(), 0.0000001)
}

func toDecimalPtr(s string) *decimal.Decimal {
	d, _ := decimal.NewFromString(s)
	return &d
}
