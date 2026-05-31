package biz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent/channel"
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
	ctx = contexts.WithChannelCredentialFingerprint(ctx, fingerprint)

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

	fetched, err := client.RequestExecution.Get(context.Background(), exec.ID)
	require.NoError(t, err)
	require.Equal(t, fingerprint, fetched.CredentialFingerprint)
}
