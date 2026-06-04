package biz

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
)

func attachAPIKeyCredentialForTest(
	t *testing.T,
	ctx context.Context,
	client *ent.Client,
	entChannel *ent.Channel,
	apiKey string,
) *ent.Channel {
	t.Helper()

	secret := objects.UpstreamCredentialSecretFromAPIKey(apiKey)
	secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)
	credential, err := client.UpstreamCredential.Create().
		SetName(entChannel.Name + " credential").
		SetProviderType(entChannel.Type.String()).
		SetBaseURL(entChannel.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetIssuerScope(CredentialIssuerScope(entChannel.Type.String(), entChannel.BaseURL)).
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForAPIKey(entChannel.Type.String(), entChannel.BaseURL, apiKey)).
		SetSecretFingerprint(secretFingerprint).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(entChannel.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	reloaded, err := client.Channel.Query().
		Where(channel.IDEQ(entChannel.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	return reloaded
}

func attachOAuthCredentialForTest(
	t *testing.T,
	ctx context.Context,
	client *ent.Client,
	entChannel *ent.Channel,
	secret objects.UpstreamCredentialSecret,
) *ent.Channel {
	t.Helper()

	credential, err := client.UpstreamCredential.Create().
		SetName(entChannel.Name + " credential").
		SetProviderType(entChannel.Type.String()).
		SetBaseURL(entChannel.BaseURL).
		SetAuthKind(upstreamcredential.AuthKindOauth).
		SetSecretKind(upstreamcredential.SecretKindOauth).
		SetIssuerScope(CredentialIssuerScope(entChannel.Type.String(), entChannel.BaseURL)).
		SetKeyHint(CredentialKeyHintForSecret(channelCredentialAuthKindOAuth, secret)).
		SetSecretPayload(secret).
		SetFingerprint(ChannelCredentialFingerprintForSecret(entChannel.Type.String(), entChannel.BaseURL, upstreamcredential.AuthKindOauth.String(), secret)).
		SetSecretFingerprint(CredentialSecretFingerprintForSecret(channelCredentialAuthKindOAuth, secret)).
		SetStatus(upstreamcredential.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.ChannelCredentialRef.Create().
		SetChannelID(entChannel.ID).
		SetCredentialID(credential.ID).
		SetEnabled(true).
		Save(ctx)
	require.NoError(t, err)

	reloaded, err := client.Channel.Query().
		Where(channel.IDEQ(entChannel.ID)).
		WithCredentialRefs(func(q *ent.ChannelCredentialRefQuery) {
			q.WithCredential()
		}).
		Only(ctx)
	require.NoError(t, err)

	return reloaded
}
