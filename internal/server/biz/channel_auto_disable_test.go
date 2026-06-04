package biz

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/pkg/xcache/live"
	"github.com/looplj/axonhub/llm/httpclient"
)

func newTestChannelService(client *ent.Client) *ChannelService {
	mockSysSvc := &SystemService{
		AbstractService: &AbstractService{db: client},
		Cache:           xcache.NewFromConfig[ent.System](xcache.Config{Mode: xcache.ModeMemory}),
	}

	svc := &ChannelService{
		AbstractService:       &AbstractService{db: client},
		SystemService:         mockSysSvc,
		WebhookNotifier:       NewWebhookNotifier(mockSysSvc, httpclient.NewHttpClient()),
		channelPerfMetrics:    make(map[int]*channelMetrics),
		channelErrorCounts:    make(map[int]map[int]int),
		credentialErrorCounts: make(map[string]map[int]int),
		perfWindowSeconds:     600,
	}

	svc.enabledChannelsCache = live.NewCache(live.Options[[]*Channel]{
		Name:            "test_enabled_channels",
		InitialValue:    []*Channel{},
		RefreshInterval: time.Hour,
		RefreshFunc:     svc.reloadEnabledChannels,
		OnSwap:          svc.onEnabledChannelsSwap,
	})

	return svc
}

func createAutoDisableTestChannel(t *testing.T, client *ent.Client, ctx context.Context, name string) *ent.Channel {
	t.Helper()

	ch, err := client.Channel.Create().
		SetName(name).
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com").
		SetCredentials(objects.ChannelCredentials{}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	return ch
}

func attachTestCredential(
	t *testing.T,
	client *ent.Client,
	ctx context.Context,
	ch *ent.Channel,
	key string,
) *ent.UpstreamCredential {
	t.Helper()

	secret := objects.UpstreamCredentialSecretFromAPIKey(key)
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName("credential-" + key).
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL(ch.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForAPIKey(ch.Type.String(), ch.BaseURL, key)).
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

	return credential
}

func attachExistingCredentialToChannel(
	t *testing.T,
	client *ent.Client,
	ctx context.Context,
	ch *ent.Channel,
	credential *ent.UpstreamCredential,
) {
	t.Helper()

	_, err := client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)
}

func TestChannelService_checkAndHandleCredentialError(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "test-channel")
	credential := attachTestCredential(t, client, ctx, ch, "key1")

	tests := []struct {
		name             string
		policy           *RetryPolicy
		perf             *PerformanceRecord
		expectedDisabled bool
		setupFunc        func()
	}{
		{
			name: "first error - should not disable",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 3}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:             ch.ID,
				CredentialID:          credential.ID,
				CredentialFingerprint: credential.Fingerprint,
				ResponseStatusCode:    401,
				Success:               false,
			},
			expectedDisabled: false,
			setupFunc: func() {
				svc.credentialErrorCounts = make(map[string]map[int]int)
			},
		},
		{
			name: "second error - should not disable",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 3}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:             ch.ID,
				CredentialID:          credential.ID,
				CredentialFingerprint: credential.Fingerprint,
				ResponseStatusCode:    401,
				Success:               false,
			},
			expectedDisabled: false,
			setupFunc: func() {
				svc.credentialErrorCounts = map[string]map[int]int{
					credentialErrorIdentity(&PerformanceRecord{CredentialID: credential.ID}): {401: 1},
				}
			},
		},
		{
			name: "third error - should disable credential",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 3}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:             ch.ID,
				CredentialID:          credential.ID,
				CredentialFingerprint: credential.Fingerprint,
				ResponseStatusCode:    401,
				Success:               false,
			},
			expectedDisabled: true,
			setupFunc: func() {
				_, err := client.UpstreamCredential.UpdateOneID(credential.ID).
					SetStatus(upstreamcredential.StatusEnabled).
					Save(ctx)
				require.NoError(t, err)

				svc.credentialErrorCounts = map[string]map[int]int{
					credentialErrorIdentity(&PerformanceRecord{CredentialID: credential.ID}): {401: 2},
				}
			},
		},
		{
			name: "different status code - should not disable",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 3}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:             ch.ID,
				CredentialID:          credential.ID,
				CredentialFingerprint: credential.Fingerprint,
				ResponseStatusCode:    500,
				Success:               false,
			},
			expectedDisabled: false,
			setupFunc: func() {
				svc.credentialErrorCounts = map[string]map[int]int{
					credentialErrorIdentity(&PerformanceRecord{CredentialID: credential.ID}): {401: 2},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UpstreamCredential.UpdateOneID(credential.ID).
				SetStatus(upstreamcredential.StatusEnabled).
				Save(ctx)
			require.NoError(t, err)

			tt.setupFunc()
			result := svc.checkAndHandleCredentialError(ctx, tt.perf, tt.policy)
			require.Equal(t, tt.expectedDisabled, result)

			updatedCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
			require.NoError(t, err)
			if tt.expectedDisabled {
				require.Equal(t, upstreamcredential.StatusDisabled, updatedCredential.Status)
				svc.credentialErrorCountsLock.Lock()
				_, exists := svc.credentialErrorCounts[credentialErrorIdentity(tt.perf)]
				svc.credentialErrorCountsLock.Unlock()
				require.False(t, exists)
			} else {
				require.Equal(t, upstreamcredential.StatusEnabled, updatedCredential.Status)
			}
		})
	}
}

func TestChannelService_checkAndHandleChannelError(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "test-channel-no-credentials")

	tests := []struct {
		name             string
		policy           *RetryPolicy
		perf             *PerformanceRecord
		expectedDisabled bool
		setupFunc        func()
	}{
		{
			name: "first error - should not disable",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 2}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:          ch.ID,
				ResponseStatusCode: 401,
				Success:            false,
			},
			expectedDisabled: false,
			setupFunc: func() {
				svc.channelErrorCounts = make(map[int]map[int]int)
				_, err := client.Channel.UpdateOneID(ch.ID).
					SetStatus(channel.StatusEnabled).
					ClearErrorMessage().
					Save(ctx)
				require.NoError(t, err)
			},
		},
		{
			name: "second error - should disable channel",
			policy: &RetryPolicy{
				AutoDisableChannel: AutoDisableChannel{
					Enabled:  true,
					Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 2}},
				},
			},
			perf: &PerformanceRecord{
				ChannelID:          ch.ID,
				ResponseStatusCode: 401,
				Success:            false,
			},
			expectedDisabled: true,
			setupFunc: func() {
				_, err := client.Channel.UpdateOneID(ch.ID).
					SetStatus(channel.StatusEnabled).
					ClearErrorMessage().
					Save(ctx)
				require.NoError(t, err)

				svc.channelErrorCounts = map[int]map[int]int{
					ch.ID: {401: 1},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupFunc()
			result := svc.checkAndHandleChannelError(ctx, tt.perf, tt.policy)
			require.Equal(t, tt.expectedDisabled, result)

			if tt.expectedDisabled {
				time.Sleep(100 * time.Millisecond)
				updatedCh, err := client.Channel.Get(ctx, ch.ID)
				require.NoError(t, err)
				require.Equal(t, channel.StatusDisabled, updatedCh.Status)
				require.NotNil(t, updatedCh.ErrorMessage)
			}
		})
	}
}

func TestChannelService_markChannelUnavailable_RefreshesStaleLocalCacheWhenAlreadyDisabled(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	defer svc.enabledChannelsCache.Stop()

	ch := createAutoDisableTestChannel(t, client, ctx, "stale-cache-channel")
	attachTestCredential(t, client, ctx, ch, "cache-key")

	require.NoError(t, svc.enabledChannelsCache.Load(ctx, true))
	require.NotNil(t, svc.GetEnabledChannel(ch.ID))

	_, err := client.Channel.UpdateOneID(ch.ID).
		SetStatus(channel.StatusDisabled).
		SetErrorMessage("disabled elsewhere").
		Save(ctx)
	require.NoError(t, err)

	svc.markChannelUnavailable(ctx, ch.ID, 401, 2, 2)

	require.Nil(t, svc.GetEnabledChannel(ch.ID))
}

func TestChannelService_SuccessClearsErrorCounts(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "test-channel")
	credential := attachTestCredential(t, client, ctx, ch, "key1")

	svc.channelErrorCounts = map[int]map[int]int{
		ch.ID: {401: 2, 500: 1},
	}
	svc.credentialErrorCounts = map[string]map[int]int{
		credentialErrorIdentity(&PerformanceRecord{CredentialID: credential.ID}): {401: 2},
	}

	perf := &PerformanceRecord{
		ChannelID:             ch.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		Success:               true,
		RequestCompleted:      true,
		EndTime:               time.Now(),
	}

	svc.IncrementChannelSelection(ch.ID)
	svc.RecordPerformance(ctx, perf)

	svc.channelErrorCountsLock.Lock()
	_, channelExists := svc.channelErrorCounts[ch.ID]
	svc.channelErrorCountsLock.Unlock()
	require.False(t, channelExists)

	svc.credentialErrorCountsLock.Lock()
	_, credentialExists := svc.credentialErrorCounts[credentialErrorIdentity(perf)]
	svc.credentialErrorCountsLock.Unlock()
	require.False(t, credentialExists)
}

func TestChannelService_MultipleStatusCodes(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "test-channel")
	credential := attachTestCredential(t, client, ctx, ch, "key1")

	policy := &RetryPolicy{
		AutoDisableChannel: AutoDisableChannel{
			Enabled: true,
			Statuses: []AutoDisableChannelStatus{
				{Status: 401, Times: 2},
				{Status: 403, Times: 1},
			},
		},
	}

	svc.credentialErrorCounts = map[string]map[int]int{
		credentialErrorIdentity(&PerformanceRecord{CredentialID: credential.ID}): {401: 1},
	}

	perf401 := &PerformanceRecord{
		ChannelID:             ch.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		ResponseStatusCode:    401,
		Success:               false,
	}
	require.True(t, svc.checkAndHandleCredentialError(ctx, perf401, policy))

	_, err := client.UpstreamCredential.UpdateOneID(credential.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	svc.credentialErrorCounts = make(map[string]map[int]int)

	perf403 := &PerformanceRecord{
		ChannelID:             ch.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		ResponseStatusCode:    403,
		Success:               false,
	}
	require.True(t, svc.checkAndHandleCredentialError(ctx, perf403, policy))

	updatedCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusDisabled, updatedCredential.Status)
}

func TestChannelService_DisableCredentialIDIdempotent(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "test-channel")
	credential := attachTestCredential(t, client, ctx, ch, "key1")

	affected, err := svc.DisableCredentialID(ctx, credential.ID, 401, "Reason 1")
	require.NoError(t, err)
	require.Equal(t, 1, affected)

	affected, err = svc.DisableCredentialID(ctx, credential.ID, 401, "Reason 2")
	require.NoError(t, err)
	require.Equal(t, 1, affected)

	updatedCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusDisabled, updatedCredential.Status)
}

func TestChannelService_DisableCredentialIDNotFound(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)

	affected, err := svc.DisableCredentialID(ctx, 99999, 401, "Reason")
	require.NoError(t, err)
	require.Equal(t, 0, affected)
}

func TestChannelService_DisableCredentialIDEmptyID(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)

	affected, err := svc.DisableCredentialID(ctx, 0, 401, "Reason")
	require.Error(t, err)
	require.Equal(t, 0, affected)
}

func TestChannelService_CheckAndHandleCredentialErrorOnlyForCredentialScopedStatuses(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch1 := createAutoDisableTestChannel(t, client, ctx, "channel-one")
	ch2 := createAutoDisableTestChannel(t, client, ctx, "channel-two")
	credential := attachTestCredential(t, client, ctx, ch1, "shared-upstream-key")
	attachExistingCredentialToChannel(t, client, ctx, ch2, credential)

	policy := &RetryPolicy{
		AutoDisableChannel: AutoDisableChannel{
			Enabled: true,
			Statuses: []AutoDisableChannelStatus{
				{Status: 500, Times: 1},
				{Status: 401, Times: 1},
			},
		},
	}

	transient := &PerformanceRecord{
		ChannelID:             ch1.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		ResponseStatusCode:    500,
		Success:               false,
	}
	require.False(t, svc.checkAndHandleCredentialError(ctx, transient, policy))

	updated1, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusEnabled, updated1.Status)

	authFailure := &PerformanceRecord{
		ChannelID:             ch1.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		ResponseStatusCode:    401,
		Success:               false,
	}
	require.True(t, svc.checkAndHandleCredentialError(ctx, authFailure, policy))

	updated1, err = client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusDisabled, updated1.Status)
}

func TestChannelService_CheckAndHandleCredentialErrorDisablesUpstreamCredentialID(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	ch := createAutoDisableTestChannel(t, client, ctx, "channel-one")
	credential := attachTestCredential(t, client, ctx, ch, "first-class-key")

	policy := &RetryPolicy{
		AutoDisableChannel: AutoDisableChannel{
			Enabled:  true,
			Statuses: []AutoDisableChannelStatus{{Status: 401, Times: 1}},
		},
	}
	perf := &PerformanceRecord{
		ChannelID:             ch.ID,
		CredentialID:          credential.ID,
		CredentialFingerprint: credential.Fingerprint,
		ResponseStatusCode:    401,
		Success:               false,
	}

	require.True(t, svc.checkAndHandleCredentialError(ctx, perf, policy))

	updatedCredential, err := client.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.Equal(t, upstreamcredential.StatusDisabled, updatedCredential.Status)

	refs, err := client.ChannelCredentialRef.Query().
		Where(channelcredentialref.ChannelIDEQ(ch.ID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.True(t, refs[0].Enabled)
}

func TestChannelService_ReloadedChannelExcludesDisabledCredential(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	svc := newTestChannelService(client)
	defer svc.enabledChannelsCache.Stop()

	ch := createAutoDisableTestChannel(t, client, ctx, "quota-channel")
	secret := objects.UpstreamCredentialSecretFromAPIKey("quota-key")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		SetLimitAmount("100").
		SetUsedAmount("0").
		Save(ctx)
	require.NoError(t, err)

	credential, err := client.UpstreamCredential.Create().
		SetName("quota credential").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL(ch.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope("openai").
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForAPIKey(ch.Type.String(), ch.BaseURL, "quota-key")).
		SetSecretFingerprint(secretFingerprint).
		SetQuotaScopeID(quotaScope.ID).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	require.NoError(t, svc.enabledChannelsCache.Load(ctx, true))
	cached := svc.GetEnabledChannel(ch.ID)
	require.NotNil(t, cached)
	require.Len(t, cached.CredentialViews(), 1)

	_, err = svc.DisableCredentialID(ctx, credential.ID, 401, "disable for test")
	require.NoError(t, err)

	require.NoError(t, svc.enabledChannelsCache.Load(ctx, true))
	require.Nil(t, svc.GetEnabledChannel(ch.ID))
}
