package gql

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/scopes"
	"github.com/looplj/axonhub/internal/server/biz"
)

func setupTestSystemMutationResolver(t *testing.T) (*mutationResolver, context.Context, *ent.Client) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	systemService := &biz.SystemService{
		Cache: xcache.NewFromConfig[ent.System](xcache.Config{Mode: xcache.ModeMemory}),
	}

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	resolver := &mutationResolver{&Resolver{systemService: systemService}}
	return resolver, ctx, client
}

func setupTestSystemQueryResolver(t *testing.T) (*queryResolver, context.Context, *ent.Client) {
	t.Helper()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")
	systemService := &biz.SystemService{
		Cache: xcache.NewFromConfig[ent.System](xcache.Config{Mode: xcache.ModeMemory}),
	}

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)
	ctx = contexts.WithUser(ctx, &ent.User{
		ID:      1,
		IsOwner: true,
		Scopes:  []string{string(scopes.ScopeReadSettings)},
	})

	resolver := &queryResolver{&Resolver{
		client:        client,
		systemService: systemService,
	}}
	return resolver, ctx, client
}

func TestMutationResolver_UpdateSystemChannelSettings_MergesAutoSyncWithoutOverwritingProbe(t *testing.T) {
	resolver, ctx, client := setupTestSystemMutationResolver(t)
	defer client.Close()

	err := resolver.systemService.SetChannelSetting(ctx, biz.SystemChannelSettings{
		Probe: biz.ChannelProbeSetting{
			Enabled:   true,
			Frequency: biz.ProbeFrequency5Min,
		},
		AutoSync: biz.ChannelModelAutoSyncSetting{
			Frequency: biz.AutoSyncFrequencyOneHour,
		},
	})
	require.NoError(t, err)

	ok, err := resolver.UpdateSystemChannelSettings(ctx, biz.SystemChannelSettings{
		AutoSync: biz.ChannelModelAutoSyncSetting{
			Frequency: biz.AutoSyncFrequencySixHours,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	setting, err := resolver.systemService.ChannelSetting(ctx)
	require.NoError(t, err)
	require.True(t, setting.Probe.Enabled)
	require.Equal(t, biz.ProbeFrequency5Min, setting.Probe.Frequency)
	require.Equal(t, biz.AutoSyncFrequencySixHours, setting.AutoSync.Frequency)
}

func TestMutationResolver_UpdateSystemChannelSettings_MergesProbeWithoutOverwritingAutoSync(t *testing.T) {
	resolver, ctx, client := setupTestSystemMutationResolver(t)
	defer client.Close()

	err := resolver.systemService.SetChannelSetting(ctx, biz.SystemChannelSettings{
		Probe: biz.ChannelProbeSetting{
			Enabled:   true,
			Frequency: biz.ProbeFrequency5Min,
		},
		AutoSync: biz.ChannelModelAutoSyncSetting{
			Frequency: biz.AutoSyncFrequencySixHours,
		},
	})
	require.NoError(t, err)

	ok, err := resolver.UpdateSystemChannelSettings(ctx, biz.SystemChannelSettings{
		Probe: biz.ChannelProbeSetting{
			Enabled:   false,
			Frequency: biz.ProbeFrequency1Hour,
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	setting, err := resolver.systemService.ChannelSetting(ctx)
	require.NoError(t, err)
	require.False(t, setting.Probe.Enabled)
	require.Equal(t, biz.ProbeFrequency1Hour, setting.Probe.Frequency)
	require.Equal(t, biz.AutoSyncFrequencySixHours, setting.AutoSync.Frequency)
}

func TestMutationResolver_UpdateResponseQualityGuardSettings_MergesWithoutOverwritingRetryPolicy(t *testing.T) {
	resolver, ctx, client := setupTestSystemMutationResolver(t)
	defer client.Close()

	err := resolver.systemService.SetRetryPolicy(ctx, &biz.RetryPolicy{
		Enabled:                 true,
		MaxChannelRetries:       5,
		MaxSingleChannelRetries: 3,
		RetryDelayMs:            1200,
		EmptyResponseDetection:  true,
		UpstreamErrorPolicy: biz.UpstreamErrorPolicy{
			Mode:          biz.UpstreamErrorModeCustom,
			CustomMessage: "custom upstream error",
		},
		ResponseQualityGuard: biz.ResponseQualityGuard{
			Enabled: false,
			Mode:    "observe_only",
			Rules:   []biz.ResponseQualityGuardRule{},
		},
	})
	require.NoError(t, err)

	ok, err := resolver.UpdateResponseQualityGuardSettings(ctx, biz.ResponseQualityGuard{
		Enabled: true,
		Mode:    "retry_on_match",
		Rules: []biz.ResponseQualityGuardRule{
			{
				ModelMatch:                []string{"gpt-5.4", "gpt-5-codex"},
				ReasoningTokensLTE:        516,
				ApplyToStream:             true,
				ApplyToNonStream:          true,
				BufferStreamUntilDecision: true,
			},
		},
	})
	require.NoError(t, err)
	require.True(t, ok)

	policy, err := resolver.systemService.RetryPolicy(ctx)
	require.NoError(t, err)
	require.True(t, policy.Enabled)
	require.Equal(t, 5, policy.MaxChannelRetries)
	require.Equal(t, 3, policy.MaxSingleChannelRetries)
	require.Equal(t, 1200, policy.RetryDelayMs)
	require.True(t, policy.EmptyResponseDetection)
	require.Equal(t, biz.UpstreamErrorModeCustom, policy.UpstreamErrorPolicy.Mode)
	require.Equal(t, "custom upstream error", policy.UpstreamErrorPolicy.CustomMessage)
	require.True(t, policy.ResponseQualityGuard.Enabled)
	require.Equal(t, "retry_on_match", policy.ResponseQualityGuard.Mode)
	require.Len(t, policy.ResponseQualityGuard.Rules, 1)
	require.Equal(t, []string{"gpt-5.4", "gpt-5-codex"}, policy.ResponseQualityGuard.Rules[0].ModelMatch)
	require.Equal(t, int64(516), policy.ResponseQualityGuard.Rules[0].ReasoningTokensLTE)
	require.True(t, policy.ResponseQualityGuard.Rules[0].ApplyToStream)
	require.True(t, policy.ResponseQualityGuard.Rules[0].ApplyToNonStream)
	require.True(t, policy.ResponseQualityGuard.Rules[0].BufferStreamUntilDecision)
}

func TestQueryResolver_ResponseQualityGuardStats_CountsMatchedExecutions(t *testing.T) {
	resolver, ctx, client := setupTestSystemQueryResolver(t)
	defer client.Close()

	req, err := client.Request.Create().
		SetProjectID(1).
		SetSource(request.SourceAPI).
		SetModelID("gpt-5").
		SetFormat("openai/chat_completions").
		SetRequestBody([]byte(`{}`)).
		SetStatus(request.StatusProcessing).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.RequestExecution.Create().
		SetProjectID(1).
		SetRequestID(req.ID).
		SetChannelID(1).
		SetModelID("gpt-5.4").
		SetFormat("openai/chat_completions").
		SetRequestBody([]byte(`{}`)).
		SetStatus(requestexecution.StatusFailed).
		SetResponseQualityGuardMatched(true).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.RequestExecution.Create().
		SetProjectID(1).
		SetRequestID(req.ID).
		SetChannelID(2).
		SetModelID("gpt-5.4").
		SetFormat("openai/chat_completions").
		SetRequestBody([]byte(`{}`)).
		SetStatus(requestexecution.StatusCompleted).
		SetResponseQualityGuardMatched(false).
		Save(ctx)
	require.NoError(t, err)

	stats, err := resolver.ResponseQualityGuardStats(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats.MatchedCount)
}
