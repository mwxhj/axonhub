package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/channelcredentialref"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

func setupBackupTest(t *testing.T) (*ent.Client, *BackupService, context.Context) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=1")

	service := NewBackupService(BackupServiceParams{
		Ent: client,
	})

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)

	ctx = authz.WithTestBypass(ctx)

	user, err := client.User.Create().
		SetEmail("test@example.com").
		SetPassword("password").
		SetIsOwner(true).
		Save(ctx)
	require.NoError(t, err)

	ctx = contexts.WithUser(ctx, user)

	return client, service, ctx
}

func createBackupTestChannel(t *testing.T, client *ent.Client, ctx context.Context, name string, chType channel.Type) *ent.Channel {
	credentials := objects.ChannelCredentials{
		APIKey: "test-api-key",
	}

	settings := &objects.ChannelSettings{
		ExtraModelPrefix: "test",
	}

	ch, err := client.Channel.Create().
		SetType(chType).
		SetName(name).
		SetBaseURL("https://api.example.com").
		SetStatus(channel.StatusEnabled).
		SetCredentials(credentials).
		SetSupportedModels([]string{"model-1", "model-2"}).
		SetAutoSyncSupportedModels(true).
		SetTags([]string{"test"}).
		SetDefaultTestModel("model-1").
		SetSettings(settings).
		SetOrderingWeight(1).
		Save(ctx)
	require.NoError(t, err)

	return ch
}

func createBackupTestModel(t *testing.T, client *ent.Client, ctx context.Context, developer, modelID string) *ent.Model {
	modelCard := &objects.ModelCard{
		Reasoning: objects.ModelCardReasoning{
			Supported: true,
			Default:   false,
		},
		ToolCall:    true,
		Temperature: true,
		Vision:      false,
		Cost: objects.ModelCardCost{
			Input:  0.001,
			Output: 0.002,
		},
		Limit: objects.ModelCardLimit{
			Context: 8192,
			Output:  4096,
		},
	}

	settings := &objects.ModelSettings{
		Associations: []*objects.ModelAssociation{},
	}

	m, err := client.Model.Create().
		SetDeveloper(developer).
		SetModelID(modelID).
		SetType(model.TypeChat).
		SetName(fmt.Sprintf("Test Model %s", modelID)).
		SetIcon("test-icon").
		SetGroup("test-group").
		SetModelCard(modelCard).
		SetSettings(settings).
		SetStatus(model.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	return m
}

func createBackupTestProject(t *testing.T, client *ent.Client, ctx context.Context, name, description string) *ent.Project {
	proj, err := client.Project.Create().
		SetName(name).
		SetDescription(description).
		Save(ctx)
	require.NoError(t, err)

	return proj
}

func createBackupTestChannelModelPrice(t *testing.T, client *ent.Client, ctx context.Context, channelID int, modelID string) *ent.ChannelModelPrice {
	pricePerUnit := decimal.NewFromFloat(0.01)
	price := objects.ModelPrice{
		Items: []objects.ModelPriceItem{
			{
				ItemCode: objects.PriceItemCodeUsage,
				Pricing: objects.Pricing{
					Mode:         objects.PricingModeUsagePerUnit,
					UsagePerUnit: &pricePerUnit,
				},
			},
		},
	}

	cmp, err := client.ChannelModelPrice.Create().
		SetChannelID(channelID).
		SetModelID(modelID).
		SetPrice(price).
		SetReferenceID("ref-" + modelID).
		Save(ctx)
	require.NoError(t, err)

	return cmp
}

func createBackupTestAPIKey(t *testing.T, client *ent.Client, ctx context.Context, user *ent.User, project *ent.Project, name, key string) *ent.APIKey {
	profiles := &objects.APIKeyProfiles{
		ActiveProfile: "default",
		Profiles: []objects.APIKeyProfile{
			{
				Name:     "default",
				ModelIDs: []string{"gpt-4"},
			},
		},
	}

	ak, err := client.APIKey.Create().
		SetKey(key).
		SetName(name).
		SetType("user").
		SetStatus("enabled").
		SetScopes([]string{"chat"}).
		SetProfiles(profiles).
		SetUserID(user.ID).
		SetProjectID(project.ID).
		Save(ctx)
	require.NoError(t, err)

	return ak
}

func createBackupTestUsage(t *testing.T, client *ent.Client, ctx context.Context, project *ent.Project, ch *ent.Channel, ak *ent.APIKey) (*ent.Request, *ent.RequestExecution, *ent.UsageLog) {
	req, err := client.Request.Create().
		SetProjectID(project.ID).
		SetAPIKeyID(ak.ID).
		SetChannelID(ch.ID).
		SetSource(request.SourceAPI).
		SetModelID("gpt-4").
		SetFormat("openai/chat_completions").
		SetRequestBody(objects.JSONRawMessage(`{"model":"gpt-4"}`)).
		SetStatus(request.StatusCompleted).
		SetStream(false).
		SetClientIP("127.0.0.1").
		Save(ctx)
	require.NoError(t, err)

	exec, err := client.RequestExecution.Create().
		SetRequestID(req.ID).
		SetProjectID(project.ID).
		SetChannelID(ch.ID).
		SetModelID("gpt-4").
		SetFormat("openai/chat_completions").
		SetRequestBody(objects.JSONRawMessage(`{"model":"gpt-4"}`)).
		SetResponseBody(objects.JSONRawMessage(`{"id":"resp_backup"}`)).
		SetStatus(requestexecution.StatusCompleted).
		SetStream(false).
		SetCredentialFingerprint("cred:v1:backup").
		SetSecretFingerprint("secret:v1:backup").
		SetResourceScopeKey("openai:secret:v1:backup").
		SetCredentialNameSnapshot("backup credential").
		SetCredentialKeyHint("sk-...-backup").
		SetCredentialSource(biz.ChannelCredentialSourceRef).
		SetCredentialQuotaStatusSnapshot("available").
		Save(ctx)
	require.NoError(t, err)

	cost := 0.42
	usage, err := client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(ak.ID).
		SetProjectID(project.ID).
		SetChannelID(ch.ID).
		SetCredentialFingerprint("cred:v1:backup").
		SetSecretFingerprint("secret:v1:backup").
		SetResourceScopeKey("openai:secret:v1:backup").
		SetCredentialNameSnapshot("backup credential").
		SetCredentialKeyHint("sk-...-backup").
		SetCredentialSource(biz.ChannelCredentialSourceRef).
		SetCredentialQuotaStatusSnapshot("available").
		SetModelID("gpt-4").
		SetPromptTokens(100).
		SetCompletionTokens(50).
		SetTotalTokens(150).
		SetPromptCachedTokens(20).
		SetSource(usagelog.SourceAPI).
		SetFormat("openai/chat_completions").
		SetTotalCost(cost).
		SetCostPriceReferenceID("price-ref").
		Save(ctx)
	require.NoError(t, err)

	return req, exec, usage
}

func TestBackupService_Backup(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	ch1 := createBackupTestChannel(t, client, ctx, "Channel 1", channel.TypeOpenai)
	ch2 := createBackupTestChannel(t, client, ctx, "Channel 2", channel.TypeAnthropic)

	_ = createBackupTestChannelModelPrice(t, client, ctx, ch1.ID, "gpt-4")

	m1 := createBackupTestModel(t, client, ctx, "openai", "gpt-4")
	m2 := createBackupTestModel(t, client, ctx, "anthropic", "claude-3")

	data, err := service.Backup(ctx, BackupOptions{
		IncludeChannels:    true,
		IncludeModels:      true,
		IncludeModelPrices: true,
	})
	require.NoError(t, err)
	require.NotNil(t, data)
	require.NotEmpty(t, data)

	var backupData BackupData

	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)

	require.Equal(t, BackupVersion, backupData.Version)
	require.Len(t, backupData.Channels, 2)
	require.Len(t, backupData.Models, 2)
	require.Len(t, backupData.ChannelModelPrices, 1)

	require.Equal(t, ch1.Name, backupData.Channels[0].Name)
	require.Equal(t, ch2.Name, backupData.Channels[1].Name)
	require.Equal(t, m1.Name, backupData.Models[0].Name)
	require.Equal(t, m2.Name, backupData.Models[1].Name)

	require.Equal(t, ch1.Name, backupData.ChannelModelPrices[0].ChannelName)
	require.Equal(t, "gpt-4", backupData.ChannelModelPrices[0].ModelID)
}

func TestBackupService_Backup_ExcludeModelPrices(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	ch1 := createBackupTestChannel(t, client, ctx, "Channel 1", channel.TypeOpenai)
	_ = createBackupTestChannelModelPrice(t, client, ctx, ch1.ID, "gpt-4")

	data, err := service.Backup(ctx, BackupOptions{
		IncludeChannels:    true,
		IncludeModels:      false,
		IncludeModelPrices: false,
	})
	require.NoError(t, err)
	require.NotNil(t, data)

	var backupData BackupData

	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)

	require.Len(t, backupData.Channels, 1)
	require.Len(t, backupData.ChannelModelPrices, 0)
}

func TestBackupService_Backup_ModelPricesOnly(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	ch1 := createBackupTestChannel(t, client, ctx, "Channel 1", channel.TypeOpenai)
	_ = createBackupTestChannelModelPrice(t, client, ctx, ch1.ID, "gpt-4")

	data, err := service.Backup(ctx, BackupOptions{
		IncludeChannels:    false,
		IncludeModels:      false,
		IncludeModelPrices: true,
	})
	require.NoError(t, err)
	require.NotNil(t, data)

	var backupData BackupData

	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)

	require.Len(t, backupData.Channels, 0)
	require.Len(t, backupData.ChannelModelPrices, 1)
	require.Equal(t, "Channel 1", backupData.ChannelModelPrices[0].ChannelName)
	require.Equal(t, "gpt-4", backupData.ChannelModelPrices[0].ModelID)
}

func TestBackupService_Backup_Empty(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	data, err := service.Backup(ctx, BackupOptions{
		IncludeChannels:    true,
		IncludeModels:      true,
		IncludeModelPrices: true,
	})
	require.NoError(t, err)
	require.NotNil(t, data)

	var backupData BackupData

	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)

	require.Equal(t, BackupVersion, backupData.Version)
	require.Len(t, backupData.Channels, 0)
	require.Len(t, backupData.Models, 0)
}

func TestBackupService_Backup_WithUsageStats(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	user, _ := client.User.Query().First(ctx)
	proj := createBackupTestProject(t, client, ctx, "Project1", "Test Project")
	ch := createBackupTestChannel(t, client, ctx, "Channel 1", channel.TypeOpenai)
	ak := createBackupTestAPIKey(t, client, ctx, user, proj, "API Key 1", "sk-test-key-1")
	req, exec, usage := createBackupTestUsage(t, client, ctx, proj, ch, ak)

	data, err := service.Backup(ctx, BackupOptions{
		IncludeUsageStats: true,
	})
	require.NoError(t, err)
	require.NotContains(t, string(data), "sk-test-key-1")
	require.NotContains(t, string(data), `"edges"`)

	var backupData BackupData
	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)

	require.Equal(t, BackupVersion, backupData.Version)
	require.Len(t, backupData.UsageRequests, 1)
	require.Len(t, backupData.RequestExecutions, 1)
	require.Len(t, backupData.UsageLogs, 1)
	require.Equal(t, req.ID, backupData.UsageRequests[0].ID)
	require.Equal(t, "Project1", backupData.UsageRequests[0].ProjectName)
	require.Equal(t, "Channel 1", backupData.UsageRequests[0].ChannelName)
	require.Empty(t, backupData.UsageRequests[0].APIKeyKey)
	require.Equal(t, exec.RequestID, backupData.RequestExecutions[0].RequestID)
	require.Equal(t, "cred:v1:backup", backupData.RequestExecutions[0].CredentialFingerprint)
	require.Equal(t, "secret:v1:backup", backupData.RequestExecutions[0].SecretFingerprint)
	require.Equal(t, "openai:secret:v1:backup", backupData.RequestExecutions[0].ResourceScopeKey)
	require.Equal(t, "backup credential", backupData.RequestExecutions[0].CredentialNameSnapshot)
	require.Equal(t, "sk-...-backup", backupData.RequestExecutions[0].CredentialKeyHint)
	require.Equal(t, biz.ChannelCredentialSourceRef, backupData.RequestExecutions[0].CredentialSource)
	require.Equal(t, usage.RequestID, backupData.UsageLogs[0].RequestID)
	require.Equal(t, int64(150), backupData.UsageLogs[0].TotalTokens)
	require.Equal(t, "price-ref", backupData.UsageLogs[0].CostPriceReferenceID)
	require.Equal(t, "cred:v1:backup", backupData.UsageLogs[0].CredentialFingerprint)
	require.Equal(t, "secret:v1:backup", backupData.UsageLogs[0].SecretFingerprint)
	require.Equal(t, "openai:secret:v1:backup", backupData.UsageLogs[0].ResourceScopeKey)
	require.Equal(t, "backup credential", backupData.UsageLogs[0].CredentialNameSnapshot)
	require.Equal(t, "sk-...-backup", backupData.UsageLogs[0].CredentialKeyHint)
	require.Equal(t, biz.ChannelCredentialSourceRef, backupData.UsageLogs[0].CredentialSource)
	require.Equal(t, "available", backupData.UsageLogs[0].CredentialQuotaStatusSnapshot)

	data, err = service.Backup(ctx, BackupOptions{
		IncludeAPIKeys:    true,
		IncludeUsageStats: true,
	})
	require.NoError(t, err)

	err = json.Unmarshal(data, &backupData)
	require.NoError(t, err)
	require.Equal(t, "sk-test-key-1", backupData.UsageRequests[0].APIKeyKey)
}

func TestBackupService_Restore_PreservesRequestExecutionCredentialSnapshots(t *testing.T) {
	sourceClient, sourceService, sourceCtx := setupBackupTest(t)
	defer sourceClient.Close()

	user, _ := sourceClient.User.Query().First(sourceCtx)
	proj := createBackupTestProject(t, sourceClient, sourceCtx, "Project1", "Test Project")
	ch := createBackupTestChannel(t, sourceClient, sourceCtx, "Channel 1", channel.TypeOpenai)
	ak := createBackupTestAPIKey(t, sourceClient, sourceCtx, user, proj, "API Key 1", "sk-test-key-1")
	_, _, _ = createBackupTestUsage(t, sourceClient, sourceCtx, proj, ch, ak)

	data, err := sourceService.Backup(sourceCtx, BackupOptions{
		IncludeProjects:   true,
		IncludeChannels:   true,
		IncludeUsageStats: true,
	})
	require.NoError(t, err)

	targetClient, targetService, targetCtx := setupBackupTest(t)
	defer targetClient.Close()

	err = targetService.Restore(targetCtx, data, RestoreOptions{
		IncludeProjects:         true,
		IncludeChannels:         true,
		IncludeUsageStats:       true,
		ProjectConflictStrategy: ConflictStrategyOverwrite,
		ChannelConflictStrategy: ConflictStrategyOverwrite,
	})
	require.NoError(t, err)

	execs, err := targetClient.RequestExecution.Query().All(targetCtx)
	require.NoError(t, err)
	require.Len(t, execs, 1)
	require.Equal(t, "cred:v1:backup", execs[0].CredentialFingerprint)
	require.Equal(t, "secret:v1:backup", execs[0].SecretFingerprint)
	require.Equal(t, "openai:secret:v1:backup", execs[0].ResourceScopeKey)
	require.Equal(t, "backup credential", execs[0].CredentialNameSnapshot)
	require.Equal(t, "sk-...-backup", execs[0].CredentialKeyHint)
	require.Equal(t, biz.ChannelCredentialSourceRef, execs[0].CredentialSource)
	require.Equal(t, "available", execs[0].CredentialQuotaStatusSnapshot)
}

func TestBackupService_Backup_IncludesUpstreamCredentialsAndRefs(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	ch := createBackupTestChannel(t, client, ctx, "Credential Channel", channel.TypeOpenai)
	fingerprint := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), ch.BaseURL, "credential-key")
	secretFingerprint := biz.CredentialSecretFingerprintForSecret("api_key", objects.UpstreamCredentialSecretFromAPIKey("credential-key"))
	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("credential budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitUsd).
		SetLimitAmount("10").
		Save(ctx)
	require.NoError(t, err)
	credential, err := client.UpstreamCredential.Create().
		SetName("credential one").
		SetProviderType(channel.TypeOpenai.String()).
		SetBaseURL(ch.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretPayload(objects.UpstreamCredentialSecretFromAPIKey("credential-key")).
		SetFingerprint(fingerprint).
		SetSecretFingerprint(secretFingerprint).
		SetQuotaScopeID(quotaScope.ID).
		Save(ctx)
	require.NoError(t, err)
	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(ch.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	data, err := service.Backup(ctx, BackupOptions{IncludeChannels: true})
	require.NoError(t, err)

	var backupData BackupData
	require.NoError(t, json.Unmarshal(data, &backupData))
	require.Len(t, backupData.UpstreamCredentials, 1)
	require.Len(t, backupData.CredentialQuotaScopes, 1)
	require.Len(t, backupData.ChannelCredentialRefs, 1)
	require.Equal(t, quotaScope.Name, backupData.CredentialQuotaScopes[0].Name)
	require.Equal(t, fingerprint, backupData.UpstreamCredentials[0].Fingerprint)
	require.NotNil(t, backupData.UpstreamCredentials[0].SecretFingerprint)
	require.Equal(t, secretFingerprint, *backupData.UpstreamCredentials[0].SecretFingerprint)
	require.NotNil(t, backupData.UpstreamCredentials[0].QuotaScopeID)
	require.Equal(t, quotaScope.ID, *backupData.UpstreamCredentials[0].QuotaScopeID)
	require.Equal(t, "credential-key", backupData.UpstreamCredentials[0].SecretPayload.APIKey)
	require.Equal(t, ch.Name, backupData.ChannelCredentialRefs[0].ChannelName)
	require.Equal(t, fingerprint, backupData.ChannelCredentialRefs[0].CredentialFingerprint)
}

func TestBackupService_Restore_LegacyChannelCredentialsAreIgnored(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	data := []byte(`{
		"version":"` + BackupVersion + `",
		"channels":[
			{
				"id":10,
				"name":"Legacy Credential Channel",
				"type":"openai",
				"base_url":"https://api.openai.com/v1",
				"status":"enabled",
				"supported_models":["gpt-4"],
				"default_test_model":"gpt-4",
				"credentials":{"apiKey":"legacy-key"}
			}
		]
	}`)

	err := service.Restore(ctx, data, RestoreOptions{
		IncludeChannels:         true,
		ChannelConflictStrategy: ConflictStrategyOverwrite,
	})
	require.NoError(t, err)

	restoredChannel, err := client.Channel.Query().Where(channel.Name("Legacy Credential Channel")).Only(ctx)
	require.NoError(t, err)
	require.Empty(t, restoredChannel.Credentials.GetAllAPIKeys())

	credentialCount, err := client.UpstreamCredential.Query().Count(ctx)
	require.NoError(t, err)
	refCount, err := client.ChannelCredentialRef.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, credentialCount)
	require.Zero(t, refCount)
}

func TestBackupService_Restore_FirstClassCredentialRefs(t *testing.T) {
	client, service, ctx := setupBackupTest(t)
	defer client.Close()

	fingerprint := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "restored-key")
	secretFingerprint := biz.CredentialSecretFingerprintForSecret("api_key", objects.UpstreamCredentialSecretFromAPIKey("restored-key"))
	backupData := BackupData{
		Version: BackupVersion,
		Channels: []*BackupChannel{
			{
				Channel: ent.Channel{
					ID:               10,
					Name:             "Credential Ref Channel",
					Type:             channel.TypeOpenai,
					BaseURL:          "https://api.openai.com/v1",
					Status:           channel.StatusEnabled,
					Credentials:      objects.ChannelCredentials{},
					SupportedModels:  []string{"gpt-4"},
					DefaultTestModel: "gpt-4",
				},
			},
		},
		CredentialQuotaScopes: []*BackupCredentialQuotaScope{
			{
				CredentialQuotaScope: ent.CredentialQuotaScope{
					ID:          99,
					Name:        "restored budget",
					Status:      credentialquotascope.StatusAvailable,
					Unit:        credentialquotascope.UnitToken,
					LimitAmount: "1000",
				},
			},
		},
		UpstreamCredentials: []*BackupUpstreamCredential{
			{
				UpstreamCredential: ent.UpstreamCredential{
					ID:                7,
					Name:              "restored credential",
					ProviderType:      channel.TypeOpenai.String(),
					BaseURL:           "https://api.openai.com/v1",
					AuthKind:          upstreamcredential.AuthKindAPIKey,
					Fingerprint:       fingerprint,
					SecretFingerprint: &secretFingerprint,
					QuotaScopeID:      lo.ToPtr(99),
					Status:            upstreamcredential.StatusEnabled,
					Weight:            100,
				},
				SecretPayload: objects.UpstreamCredentialSecretFromAPIKey("restored-key"),
			},
		},
		ChannelCredentialRefs: []*BackupChannelCredentialRef{
			{
				ChannelCredentialRef: ent.ChannelCredentialRef{
					ChannelID:    10,
					CredentialID: 7,
					Enabled:      true,
				},
				ChannelName:           "Credential Ref Channel",
				CredentialFingerprint: fingerprint,
			},
		},
	}
	data, err := json.Marshal(backupData)
	require.NoError(t, err)

	err = service.Restore(ctx, data, RestoreOptions{
		IncludeChannels:         true,
		ChannelConflictStrategy: ConflictStrategyOverwrite,
	})
	require.NoError(t, err)

	refExists, err := client.ChannelCredentialRef.Query().
		Where(channelcredentialref.HasCredentialWith(upstreamcredential.Fingerprint(fingerprint))).
		Exist(ctx)
	require.NoError(t, err)
	require.True(t, refExists)

	restoredCredential, err := client.UpstreamCredential.Query().
		Where(upstreamcredential.Fingerprint(fingerprint)).
		Only(ctx)
	require.NoError(t, err)
	require.NotNil(t, restoredCredential.SecretFingerprint)
	require.Equal(t, secretFingerprint, *restoredCredential.SecretFingerprint)
	require.NotNil(t, restoredCredential.QuotaScopeID)

	restoredScope, err := client.CredentialQuotaScope.Get(ctx, *restoredCredential.QuotaScopeID)
	require.NoError(t, err)
	require.Equal(t, "restored budget", restoredScope.Name)
}
