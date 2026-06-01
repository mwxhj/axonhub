package biz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestRequestService_CreateRequestExecutionStoresCredentialFingerprint(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetName("test-channel").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetModelID("gpt-4").
		SetStatus(request.StatusProcessing).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	fingerprint := ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", "test-key")
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, objects.UpstreamCredentialSecretFromAPIKey("test-key"))
	resourceScopeKey := ChannelCredentialResourceScopeKey(ch, secretFingerprint)
	quotaScope, err := client.CredentialQuotaScope.Create().
		SetName("openai budget").
		SetStatus(credentialquotascope.StatusAvailable).
		SetUnit(credentialquotascope.UnitToken).
		Save(ctx)
	require.NoError(t, err)
	ctx = contexts.WithChannelCredentialFingerprint(ctx, fingerprint)
	ctx = contexts.WithChannelCredentialIdentity(ctx, secretFingerprint, resourceScopeKey)
	ctx = contexts.WithChannelCredentialMetadata(ctx, "OpenAI key", "sk-...-key", ChannelCredentialSourceRef, "available")
	ctx = contexts.WithChannelCredentialQuotaScope(ctx, quotaScope.ID, quotaScope.Name, quotaScope.Status.String())

	exec, err := svc.CreateRequestExecution(
		ctx,
		&Channel{Channel: ch},
		"gpt-4",
		req,
		httpclient.Request{JSONBody: []byte(`{}`)},
		llm.APIFormatOpenAIChatCompletion,
	)
	require.NoError(t, err)
	require.Equal(t, fingerprint, exec.CredentialFingerprint)
	require.Equal(t, secretFingerprint, exec.SecretFingerprint)
	require.Equal(t, resourceScopeKey, exec.ResourceScopeKey)
	require.Equal(t, "OpenAI key", exec.CredentialNameSnapshot)
	require.Equal(t, "sk-...-key", exec.CredentialKeyHint)
	require.Equal(t, ChannelCredentialSourceRef, exec.CredentialSource)
	require.Equal(t, "available", exec.CredentialQuotaStatusSnapshot)
	require.Equal(t, quotaScope.ID, exec.QuotaScopeID)
	require.Equal(t, quotaScope.Name, exec.QuotaScopeNameSnapshot)
	require.Equal(t, quotaScope.Status.String(), exec.QuotaScopeStatusSnapshot)

	fetched, err := client.RequestExecution.Get(context.Background(), exec.ID)
	require.NoError(t, err)
	require.Equal(t, fingerprint, fetched.CredentialFingerprint)
	require.Equal(t, secretFingerprint, fetched.SecretFingerprint)
	require.Equal(t, resourceScopeKey, fetched.ResourceScopeKey)
	require.Equal(t, "OpenAI key", fetched.CredentialNameSnapshot)
	require.Equal(t, "sk-...-key", fetched.CredentialKeyHint)
	require.Equal(t, ChannelCredentialSourceRef, fetched.CredentialSource)
	require.Equal(t, "available", fetched.CredentialQuotaStatusSnapshot)
	require.Equal(t, quotaScope.ID, fetched.QuotaScopeID)
	require.Equal(t, quotaScope.Name, fetched.QuotaScopeNameSnapshot)
	require.Equal(t, quotaScope.Status.String(), fetched.QuotaScopeStatusSnapshot)
}

func TestRequestService_CreateRequestExecutionRedactsOutboundBodySecrets(t *testing.T) {
	svc, client, ctx := setupTestRequestService(t)
	defer client.Close()

	proj, err := client.Project.Create().
		SetName("test-project").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetName("test-channel").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(proj.ID).
		SetModelID("gpt-4").
		SetStatus(request.StatusProcessing).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		Save(ctx)
	require.NoError(t, err)

	exec, err := svc.CreateRequestExecution(
		ctx,
		&Channel{Channel: ch},
		"gpt-4",
		req,
		httpclient.Request{JSONBody: []byte(`{"model":"gpt-4","api_key":"sk-secret","access_token":"token-secret","message":"keep me"}`)},
		llm.APIFormatOpenAIChatCompletion,
	)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"gpt-4","api_key":"[REDACTED]","access_token":"[REDACTED]","message":"keep me"}`, string(exec.RequestBody))
	require.NotContains(t, string(exec.RequestBody), "sk-secret")
	require.NotContains(t, string(exec.RequestBody), "token-secret")
}
