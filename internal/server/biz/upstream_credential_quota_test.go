package biz

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
)

func TestUpstreamCredentialService_CreateDedupesBySecretFingerprint(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-same-secret")

	first, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:         lo.ToPtr("first"),
		ProviderType: lo.ToPtr("openai"),
		BaseURL:      lo.ToPtr("https://api.openai.com/v1"),
		Secret:       secret,
	})
	require.NoError(t, err)
	require.NotNil(t, first.SecretFingerprint)

	second, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:         lo.ToPtr("second"),
		ProviderType: lo.ToPtr("openai_compatible"),
		BaseURL:      lo.ToPtr("https://gateway.example.com/v1"),
		Secret:       secret,
	})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)

	count, err := client.UpstreamCredential.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestChannelCredentialResourceScopeSeparatesOpenAICompatibleHosts(t *testing.T) {
	secretFingerprint := CredentialSecretFingerprintForSecret(
		channelCredentialAuthKindAPIKey,
		objects.UpstreamCredentialSecretFromAPIKey("sk-shared-resource-scope"),
	)

	official := &ent.Channel{
		Type:    channel.TypeOpenai,
		BaseURL: "https://api.openai.com/v1",
	}
	gatewayA := &ent.Channel{
		Type:    channel.TypeOpenai,
		BaseURL: "https://gateway-a.example.com/v1",
	}
	gatewayB := &ent.Channel{
		Type:    channel.TypeOpenaiResponses,
		BaseURL: "https://gateway-b.example.com/v1",
	}

	require.Equal(t, "openai:"+secretFingerprint, ChannelCredentialResourceScopeKey(official, secretFingerprint))
	require.Equal(t, "gateway-a.example.com:"+secretFingerprint, ChannelCredentialResourceScopeKey(gatewayA, secretFingerprint))
	require.Equal(t, "gateway-b.example.com:"+secretFingerprint, ChannelCredentialResourceScopeKey(gatewayB, secretFingerprint))
	require.NotEqual(t, ChannelCredentialResourceScopeKey(gatewayA, secretFingerprint), ChannelCredentialResourceScopeKey(gatewayB, secretFingerprint))
}

func TestUpstreamCredentialService_CreateAndUpdateInlineQuotaScope(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	unit := credentialquotascope.UnitToken
	resetPolicy := credentialquotascope.ResetPolicyMonthly
	threshold := 80
	credential, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("budgeted"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-budgeted"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
		Quota: &CreateCredentialQuotaScopeInput{
			Name:                    lo.ToPtr("shared budget"),
			Unit:                    &unit,
			LimitAmount:             lo.ToPtr("1000"),
			WarningThresholdPercent: &threshold,
			ResetPolicy:             &resetPolicy,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, credential.QuotaScopeID)

	scope, err := client.CredentialQuotaScope.Get(ctx, *credential.QuotaScopeID)
	require.NoError(t, err)
	require.Equal(t, "shared budget", scope.Name)
	require.Equal(t, credentialquotascope.StatusAvailable, scope.Status)
	require.Equal(t, credentialquotascope.UnitToken, scope.Unit)
	require.Equal(t, "1000", scope.LimitAmount)
	require.Equal(t, 80, *scope.WarningThresholdPercent)

	newThreshold := 90
	updated, err := svc.UpdateUpstreamCredential(ctx, credential.ID, UpdateUpstreamCredentialInput{
		Quota: &UpdateCredentialQuotaScopeInput{
			LimitAmount:             lo.ToPtr("2000"),
			WarningThresholdPercent: &newThreshold,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, updated.QuotaScopeID)
	require.Equal(t, *credential.QuotaScopeID, *updated.QuotaScopeID)

	scope, err = client.CredentialQuotaScope.Get(ctx, *updated.QuotaScopeID)
	require.NoError(t, err)
	require.Equal(t, "2000", scope.LimitAmount)
	require.Equal(t, 90, *scope.WarningThresholdPercent)
}

func TestNormalizeCreateCredentialQuotaScopeInputDefaultsDailyAndMonthlyResetAt(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*60*60)
	now := time.Date(2026, 6, 1, 15, 30, 0, 0, loc)

	daily := credentialquotascope.ResetPolicyDaily
	dailyInput, err := normalizeCreateCredentialQuotaScopeInput(CreateCredentialQuotaScopeInput{
		ResetPolicy: &daily,
	}, now)
	require.NoError(t, err)
	require.NotNil(t, dailyInput.ResetAt)
	require.Equal(t, time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC), *dailyInput.ResetAt)
	require.NotNil(t, dailyInput.WindowStartedAt)
	require.Equal(t, now.UTC(), *dailyInput.WindowStartedAt)

	monthly := credentialquotascope.ResetPolicyMonthly
	monthlyInput, err := normalizeCreateCredentialQuotaScopeInput(CreateCredentialQuotaScopeInput{
		ResetPolicy: &monthly,
	}, now)
	require.NoError(t, err)
	require.NotNil(t, monthlyInput.ResetAt)
	require.Equal(t, time.Date(2026, 6, 30, 16, 0, 0, 0, time.UTC), *monthlyInput.ResetAt)
	require.NotNil(t, monthlyInput.WindowStartedAt)
	require.Equal(t, now.UTC(), *monthlyInput.WindowStartedAt)
}

func TestUpstreamCredentialService_CreateCredentialQuotaScopeValidatesCustomResetWindow(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	custom := credentialquotascope.ResetPolicyCustom

	_, err := svc.CreateCredentialQuotaScope(ctx, CreateCredentialQuotaScopeInput{
		ResetPolicy: &custom,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires windowStartedAt and resetAt")

	startedAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	resetAt := startedAt
	_, err = svc.CreateCredentialQuotaScope(ctx, CreateCredentialQuotaScopeInput{
		ResetPolicy:     &custom,
		WindowStartedAt: &startedAt,
		ResetAt:         &resetAt,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "resetAt must be after windowStartedAt")

	resetAt = startedAt.Add(24 * time.Hour)
	scope, err := svc.CreateCredentialQuotaScope(ctx, CreateCredentialQuotaScopeInput{
		ResetPolicy:     &custom,
		WindowStartedAt: &startedAt,
		ResetAt:         &resetAt,
	})
	require.NoError(t, err)
	require.Equal(t, credentialquotascope.ResetPolicyCustom, scope.ResetPolicy)
	require.NotNil(t, scope.WindowStartedAt)
	require.NotNil(t, scope.ResetAt)
	require.Equal(t, resetAt, *scope.ResetAt)
}

func TestUpstreamCredentialService_UpdateCredentialQuotaScopeDefaultsResetAtWhenPolicyBecomesAutomatic(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	scope, err := svc.CreateCredentialQuotaScope(ctx, CreateCredentialQuotaScopeInput{
		Name: lo.ToPtr("manual scope"),
	})
	require.NoError(t, err)
	require.Nil(t, scope.ResetAt)

	daily := credentialquotascope.ResetPolicyDaily
	updated, err := svc.UpdateCredentialQuotaScope(ctx, scope.ID, UpdateCredentialQuotaScopeInput{
		ResetPolicy: &daily,
	})
	require.NoError(t, err)
	require.Equal(t, credentialquotascope.ResetPolicyDaily, updated.ResetPolicy)
	require.NotNil(t, updated.ResetAt)
	require.NotNil(t, updated.WindowStartedAt)
}

func TestUpstreamCredentialService_ArchiveDisablesRefsAndRuntimeSelection(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	credential, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("archive me"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-archive-me"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch := createCredentialTestChannel(t, ctx, client)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	archived, err := svc.ArchiveUpstreamCredential(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusArchived, archived.Status)

	ref, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(credential.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.False(t, ref.Enabled)

	reloaded, err := client.Channel.Query().
		Where(channel.ID(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	views := credentialViewsFromRefs(reloaded)
	require.Len(t, views, 1)
	require.False(t, runtimeCredentialViews(views)[0].Enabled)
	require.Empty(t, enabledAPIKeyCredentialViews(views))
}

func TestCredentialViewsFromRefsMarksExhaustedQuotaScopeUnavailable(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("exhausted").
		SetStatus(credentialquotascope.StatusExhausted).
		SetOverLimitAction(credentialquotascope.OverLimitActionPause).
		SetUnit(credentialquotascope.UnitToken).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-exhausted")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	fingerprint := CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("exhausted key").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(fingerprint).
		SetSecretFingerprint(secretFingerprint).
		SetQuotaScopeID(quotaScope.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetName("openai").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	reloaded, err := client.Channel.Query().
		Where(channel.ID(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential(func(q *ent.UpstreamCredentialQuery) {
				q.WithQuotaScope()
			})
		}).
		Only(ctx)
	require.NoError(t, err)

	views := credentialViewsFromRefs(reloaded)
	require.Len(t, views, 1)
	require.False(t, runtimeCredentialViews(views)[0].Enabled)
	require.Empty(t, enabledAPIKeyCredentialViews(views))
}

func TestCredentialViewsFromRefsSkipsOnlyExhaustedQuotaScope(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	exhaustedScope, err := client.CredentialQuotaScope.Create().
		SetName("exhausted").
		SetStatus(credentialquotascope.StatusExhausted).
		SetOverLimitAction(credentialquotascope.OverLimitActionPause).
		SetUnit(credentialquotascope.UnitToken).
		Save(ctx)
	require.NoError(t, err)
	availableScope, err := client.CredentialQuotaScope.Create().
		SetName("available").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		Save(ctx)
	require.NoError(t, err)

	createCredential := func(name string, key string, quotaScopeID int) *ent.UpstreamCredential {
		t.Helper()
		secret := objects.UpstreamCredentialSecretFromAPIKey(key)
		credential, err := client.UpstreamCredential.Create().
			SetName(name).
			SetAuthKind(upstreamcredential.AuthKindAPIKey).
			SetSecretKind(upstreamcredential.SecretKindAPIKey).
			SetIssuerScope("openai").
			SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
			SetSecretPayload(secret).
			SetFingerprint(CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)).
			SetSecretFingerprint(CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)).
			SetQuotaScopeID(quotaScopeID).
			SetStatus(upstreamcredential.StatusEnabled).
			Save(ctx)
		require.NoError(t, err)
		return credential
	}

	exhaustedCredential := createCredential("exhausted key", "sk-exhausted", exhaustedScope.ID)
	availableCredential := createCredential("available key", "sk-available", availableScope.ID)

	ch, err := client.Channel.Create().
		SetName("openai").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	for _, credential := range []*ent.UpstreamCredential{exhaustedCredential, availableCredential} {
		_, err = client.ChannelCredentialRef.Create().
			SetChannelID(ch.ID).
			SetCredentialID(credential.ID).
			SetEnabled(true).
			Save(ctx)
		require.NoError(t, err)
	}

	reloaded, err := client.Channel.Query().
		Where(channel.ID(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential(func(q *ent.UpstreamCredentialQuery) {
				q.WithQuotaScope()
			})
		}).
		Only(ctx)
	require.NoError(t, err)

	views := credentialViewsFromRefs(reloaded)
	require.Len(t, views, 2)
	enabledViews := enabledAPIKeyCredentialViews(views)
	require.Len(t, enabledViews, 1)
	require.Equal(t, availableCredential.ID, enabledViews[0].CredentialID)
	require.Equal(t, "sk-available", enabledViews[0].Secret.APIKey)
}

func TestCredentialViewQuotaSelectableHonorsScopeActionAndPauseUntil(t *testing.T) {
	now := time.Now()

	require.True(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus:          credentialquotascope.StatusExhausted.String(),
		QuotaScopeOverLimitAction: credentialquotascope.OverLimitActionWarn.String(),
	}, now))

	require.False(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus:          credentialquotascope.StatusExhausted.String(),
		QuotaScopeOverLimitAction: credentialquotascope.OverLimitActionPause.String(),
	}, now))

	require.False(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus:     credentialquotascope.StatusPaused.String(),
		QuotaScopePauseUntil: lo.ToPtr(now.Add(time.Minute)),
	}, now))

	require.True(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus:     credentialquotascope.StatusPaused.String(),
		QuotaScopePauseUntil: lo.ToPtr(now.Add(-time.Minute)),
	}, now))

	require.False(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus: credentialquotascope.StatusDisabled.String(),
	}, now))

	require.True(t, quotaScopeViewSelectable(ChannelCredentialView{
		QuotaScopeStatus:          credentialquotascope.StatusPaused.String(),
		QuotaScopePauseUntil:      lo.ToPtr(now.Add(time.Hour)),
		QuotaScopeResetPolicy:     credentialquotascope.ResetPolicyDaily.String(),
		QuotaScopeResetAt:         lo.ToPtr(now.Add(-time.Minute)),
		QuotaScopeOverLimitAction: credentialquotascope.OverLimitActionPause.String(),
	}, now))
}

func TestChannelCredentialViewsReevaluateExpiredQuotaReset(t *testing.T) {
	now := time.Now()
	channel := &Channel{
		cachedCredentialViews: []ChannelCredentialView{
			{
				Fingerprint:               "cred:v1:test",
				Secret:                    objects.UpstreamCredentialSecretFromAPIKey("sk-expired-reset"),
				Enabled:                   true,
				AuthKind:                  channelCredentialAuthKindAPIKey,
				QuotaScopeStatus:          credentialquotascope.StatusPaused.String(),
				QuotaScopePauseUntil:      lo.ToPtr(now.Add(time.Hour)),
				QuotaScopeOverLimitAction: credentialquotascope.OverLimitActionPause.String(),
				QuotaScopeResetPolicy:     credentialquotascope.ResetPolicyDaily.String(),
				QuotaScopeResetAt:         lo.ToPtr(now.Add(-time.Minute)),
			},
		},
	}

	views := channel.CredentialViews()
	require.Len(t, views, 1)
	require.True(t, views[0].Enabled)
	require.Len(t, enabledAPIKeyCredentialViews(views), 1)
}

func TestBuildChannelKeepsQuotaPausedCredentialLoadable(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("paused").
		SetStatus(credentialquotascope.StatusPaused).
		SetUnit(credentialquotascope.UnitToken).
		SetOverLimitAction(credentialquotascope.OverLimitActionPause).
		SetPauseUntil(time.Now().Add(time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-paused-loadable")
	credential, err := client.UpstreamCredential.Create().
		SetName("paused key").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)).
		SetSecretFingerprint(CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetQuotaScopeID(quotaScope.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetName("openai paused").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	reloaded, err := client.Channel.Query().
		Where(channel.ID(ch.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential(func(q *ent.UpstreamCredentialQuery) {
				q.WithQuotaScope()
			})
		}).
		Only(ctx)
	require.NoError(t, err)

	built, err := NewChannelServiceForTest(client).buildChannelWithTransformer(reloaded)
	require.NoError(t, err)
	require.NotNil(t, built)
	require.Empty(t, built.cachedEnabledAPIKeys)
	require.Len(t, authCapableAPIKeyCredentialViews(built.cachedCredentialViews), 1)
}

func TestUpstreamCredentialService_RotateSameSecretKeepsCredentialIdentity(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-same-rotate")
	credential, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("same secret"),
		Secret: secret,
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch := createCredentialTestChannel(t, ctx, client)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	rotated, err := svc.RotateUpstreamCredentialSecret(ctx, credential.ID, RotateUpstreamCredentialSecretInput{
		Secret: secret,
	})
	require.NoError(t, err)
	require.Equal(t, credential.ID, rotated.ID)

	count, err := client.UpstreamCredential.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	ref, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(credential.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, ref.Enabled)
}

func TestUpstreamCredentialService_RotateDifferentSecretCreatesReplacementAndMigratesRefs(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("rotate budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitUsd).
		Save(ctx)
	require.NoError(t, err)

	oldSecret := objects.UpstreamCredentialSecretFromAPIKey("sk-rotate-old")
	credential, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:         lo.ToPtr("rotating"),
		Secret:       oldSecret,
		Status:       lo.ToPtr(upstreamcredential.StatusEnabled),
		QuotaScopeID: &objects.GUID{Type: ent.TypeCredentialQuotaScope, ID: quotaScope.ID},
		Remark:       lo.ToPtr("keep metadata"),
	})
	require.NoError(t, err)

	ch := createCredentialTestChannel(t, ctx, client)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	rotated, err := svc.RotateUpstreamCredentialSecret(ctx, credential.ID, RotateUpstreamCredentialSecretInput{
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-rotate-new"),
	})
	require.NoError(t, err)
	require.NotEqual(t, credential.ID, rotated.ID)
	require.Equal(t, credential.Name, rotated.Name)
	require.NotNil(t, rotated.QuotaScopeID)
	require.Equal(t, quotaScope.ID, *rotated.QuotaScopeID)
	require.NotEqual(t, credential.SecretFingerprint, rotated.SecretFingerprint)

	oldCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusArchived, oldCredential.Status)

	oldRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(credential.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.False(t, oldRef.Enabled)

	newRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(rotated.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, newRef.Enabled)
}

func TestUpstreamCredentialService_RotateToExistingSecretReusesReplacementCredential(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	source, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("source"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-source"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)
	target, err := svc.CreateUpstreamCredential(ctx, CreateUpstreamCredentialInput{
		Name:   lo.ToPtr("target"),
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-target"),
		Status: lo.ToPtr(upstreamcredential.StatusEnabled),
	})
	require.NoError(t, err)

	ch := createCredentialTestChannel(t, ctx, client)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(source.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	rotated, err := svc.RotateUpstreamCredentialSecret(ctx, source.ID, RotateUpstreamCredentialSecretInput{
		Secret: objects.UpstreamCredentialSecretFromAPIKey("sk-target"),
	})
	require.NoError(t, err)
	require.Equal(t, target.ID, rotated.ID)

	count, err := client.UpstreamCredential.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	sourceAfter, err := client.UpstreamCredential.Get(ctx, source.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusArchived, sourceAfter.Status)

	targetRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(target.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, targetRef.Enabled)
}

func TestUpstreamCredentialService_BackfillSecretFingerprintUpdatesLegacyCredential(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-backfill")
	fingerprint := CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("legacy").
		SetProviderType("openai").
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(fingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)
	require.Nil(t, credential.SecretFingerprint)

	payload, err := svc.BackfillCredentialSecretFingerprints(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, payload.ScannedCredentials)
	require.Equal(t, 1, payload.UpdatedCredentials)
	require.Zero(t, payload.MergedCredentials)

	updated, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.SecretFingerprint)
	require.Equal(t, CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret), *updated.SecretFingerprint)
}

func TestUpstreamCredentialService_BackfillSecretFingerprintMergesDuplicateLegacyCredentials(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-backfill-duplicate")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	first, err := client.UpstreamCredential.Create().
		SetName("first").
		SetProviderType("openai").
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)
	second, err := client.UpstreamCredential.Create().
		SetName("second").
		SetProviderType("gateway").
		SetBaseURL("https://gateway.example.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("gateway.example.com").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("gateway.example.com", channelCredentialAuthKindAPIKey, secret)).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	firstChannel := createCredentialTestChannel(t, ctx, client)
	secondChannel, err := client.Channel.Create().
		SetName("second credential test").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://gateway.example.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(firstChannel.ID).
		SetCredentialID(first.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(secondChannel.ID).
		SetCredentialID(second.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	payload, err := svc.BackfillCredentialSecretFingerprints(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, payload.ScannedCredentials)
	require.Equal(t, 1, payload.UpdatedCredentials)
	require.Equal(t, 1, payload.MergedCredentials)
	require.Equal(t, 1, payload.MigratedRefs)
	require.Equal(t, 1, payload.DisabledSourceRefs)
	require.Equal(t, 1, payload.ArchivedCredentials)

	firstAfter, err := client.UpstreamCredential.Get(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, firstAfter.SecretFingerprint)
	require.Equal(t, secretFingerprint, *firstAfter.SecretFingerprint)

	secondAfter, err := client.UpstreamCredential.Get(ctx, second.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusArchived, secondAfter.Status)
	require.Nil(t, secondAfter.SecretFingerprint)

	sourceRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(secondChannel.ID),
			channelcredentialref.CredentialID(second.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.False(t, sourceRef.Enabled)

	targetRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(secondChannel.ID),
			channelcredentialref.CredentialID(first.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, targetRef.Enabled)
}

func TestUpstreamCredentialService_BackfillSecretFingerprintKeepsExistingTargetRefEnabled(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := NewUpstreamCredentialService(UpstreamCredentialServiceParams{Ent: client})
	secret := objects.UpstreamCredentialSecretFromAPIKey("sk-backfill-ref-enabled")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	target, err := client.UpstreamCredential.Create().
		SetName("target").
		SetProviderType("openai").
		SetBaseURL("https://api.openai.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("openai", channelCredentialAuthKindAPIKey, secret)).
		SetSecretFingerprint(secretFingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)
	source, err := client.UpstreamCredential.Create().
		SetName("source").
		SetProviderType("gateway").
		SetBaseURL("https://gateway.example.com/v1").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("gateway.example.com").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(CredentialFingerprintForSecret("gateway.example.com", channelCredentialAuthKindAPIKey, secret)).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ch := createCredentialTestChannel(t, ctx, client)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(target.ID).
		SetEnabled(true).
		SetWeightOverride(10).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(source.ID).
		SetEnabled(false).
		Save(ctx)
	require.NoError(t, err)

	payload, err := svc.BackfillCredentialSecretFingerprints(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, payload.MergedCredentials)
	require.Zero(t, payload.DisabledSourceRefs)

	targetRef, err := client.ChannelCredentialRef.Query().
		Where(
			channelcredentialref.ChannelID(ch.ID),
			channelcredentialref.CredentialID(target.ID),
		).
		Only(ctx)
	require.NoError(t, err)
	require.True(t, targetRef.Enabled)
	require.NotNil(t, targetRef.WeightOverride)
	require.Equal(t, 10, *targetRef.WeightOverride)
}

func createCredentialTestChannel(t *testing.T, ctx context.Context, client *ent.Client) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetName("credential test").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	return ch
}
