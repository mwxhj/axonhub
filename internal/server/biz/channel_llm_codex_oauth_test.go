package biz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/oauth"
	"github.com/looplj/axonhub/llm/transformer/openai/codex"
)

func TestCodexRefreshPersistsChannelCredentials(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":3600,"token_type":"bearer"}`))
	}))
	t.Cleanup(tokenServer.Close)

	prevURLs := codex.DefaultTokenURLs
	codex.DefaultTokenURLs = oauth.OAuthUrls{AuthorizeUrl: prevURLs.AuthorizeUrl, TokenUrl: tokenServer.URL}

	t.Cleanup(func() { codex.DefaultTokenURLs = prevURLs })

	db := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { _ = db.Close() })

	ctx := ent.NewContext(context.Background(), db)
	ctx = authz.WithTestBypass(ctx)

	created, err := db.Channel.Create().
		SetType(channel.TypeCodex).
		SetName("codex").
		SetStatus(channel.StatusEnabled).
		SetSupportedModels([]string{"gpt-4o-mini"}).
		SetDefaultTestModel("gpt-4o-mini").
		SetCredentials(objects.ChannelCredentials{
			OAuth: &objects.OAuthCredentials{
				AccessToken:  "old-access",
				RefreshToken: "old-refresh",
				ClientID:     codex.ClientID,
				ExpiresAt:    time.Now().Add(-1 * time.Hour),
			},
		}).
		Save(ctx)
	require.NoError(t, err)

	svc := NewChannelServiceForTest(db)

	ch, err := svc.buildChannelWithTransformer(created)
	require.NoError(t, err)

	req := &llm.Request{
		Model: "gpt-4o-mini",
		Messages: []llm.Message{
			{Role: "user", Content: llm.MessageContent{Content: new("hi")}},
		},
	}

	_, err = ch.Outbound.TransformRequest(ctx, req)
	require.NoError(t, err)

	reloaded, err := db.Channel.Get(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, reloaded.Credentials)
	require.NotNil(t, reloaded.Credentials.OAuth)
	require.Equal(t, "new-access", reloaded.Credentials.OAuth.AccessToken)
	require.Equal(t, "new-refresh", reloaded.Credentials.OAuth.RefreshToken)
	require.False(t, reloaded.Credentials.OAuth.ExpiresAt.IsZero())
	require.Equal(t, "new-access", extractAccessTokenFromAPIKeyJSON(t, reloaded.Credentials.APIKey))
}

func TestCodexRefreshPersistsFirstClassCredentialSecret(t *testing.T) {
	newAccess := unsignedTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-refresh",
		},
	})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + newAccess + `","refresh_token":"new-refresh","expires_in":3600,"token_type":"bearer"}`))
	}))
	t.Cleanup(tokenServer.Close)

	prevURLs := codex.DefaultTokenURLs
	codex.DefaultTokenURLs = oauth.OAuthUrls{AuthorizeUrl: prevURLs.AuthorizeUrl, TokenUrl: tokenServer.URL}

	t.Cleanup(func() { codex.DefaultTokenURLs = prevURLs })

	db := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { _ = db.Close() })

	ctx := ent.NewContext(context.Background(), db)
	ctx = authz.WithTestBypass(ctx)

	oldAccess := unsignedTestJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id": "acct-refresh",
		},
	})
	secret := objects.UpstreamCredentialSecret{
		OAuth: &objects.OAuthCredentials{
			AccessToken:  oldAccess,
			RefreshToken: "old-refresh",
			ClientID:     codex.ClientID,
			ExpiresAt:    time.Now().Add(-1 * time.Hour),
		},
	}

	credential, err := db.UpstreamCredential.Create().
		SetName("codex oauth").
		SetProviderType(channel.TypeCodex.String()).
		SetBaseURL("https://chatgpt.com/backend-api/codex").
		SetAuthKind(upstreamcredential.AuthKindOauth).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForSecret(channel.TypeCodex.String(), "https://chatgpt.com/backend-api/codex", upstreamcredential.AuthKindOauth.String(), secret)).
		Save(ctx)
	require.NoError(t, err)

	created, err := db.Channel.Create().
		SetType(channel.TypeCodex).
		SetName("codex").
		SetStatus(channel.StatusEnabled).
		SetSupportedModels([]string{"gpt-4o-mini"}).
		SetDefaultTestModel("gpt-4o-mini").
		SetCredentials(objects.ChannelCredentials{}).
		Save(ctx)
	require.NoError(t, err)

	_, err = db.ChannelCredentialRef.Create().
		SetChannelID(created.ID).
		SetCredentialID(credential.ID).
		Save(ctx)
	require.NoError(t, err)

	loaded, err := db.Channel.Query().
		Where(channel.ID(created.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	svc := NewChannelServiceForTest(db)

	ch, err := svc.buildChannelWithTransformer(loaded)
	require.NoError(t, err)

	req := &llm.Request{
		Model: "gpt-4o-mini",
		Messages: []llm.Message{
			{Role: "user", Content: llm.MessageContent{Content: new("hi")}},
		},
	}

	_, err = ch.Outbound.TransformRequest(ctx, req)
	require.NoError(t, err)

	reloadedCredential, err := db.UpstreamCredential.Get(ctx, credential.ID)
	require.NoError(t, err)
	require.NotNil(t, reloadedCredential.SecretPayload.OAuth)
	require.Equal(t, newAccess, reloadedCredential.SecretPayload.OAuth.AccessToken)
	require.Equal(t, "new-refresh", reloadedCredential.SecretPayload.OAuth.RefreshToken)
	require.Equal(t, newAccess, extractAccessTokenFromAPIKeyJSON(t, reloadedCredential.SecretPayload.APIKey))

	reloadedChannel, err := db.Channel.Get(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, reloadedChannel.Credentials.OAuth)
	require.Empty(t, reloadedChannel.Credentials.APIKey)
}

func extractAccessTokenFromAPIKeyJSON(t *testing.T, raw string) string {
	t.Helper()

	creds, err := oauth.ParseCredentialsJSON(raw)
	require.NoError(t, err)

	return creds.AccessToken
}
