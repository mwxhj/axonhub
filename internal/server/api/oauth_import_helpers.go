package api

import (
	"strings"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/oauth"
)

func loPtr(value string) *string {
	return &value
}

func loSecretKind(value upstreamcredential.SecretKind) *upstreamcredential.SecretKind {
	return &value
}

func loAuthKind(value upstreamcredential.AuthKind) *upstreamcredential.AuthKind {
	return &value
}

func loStatus(value upstreamcredential.Status) *upstreamcredential.Status {
	return &value
}

func bizOAuthSecret(creds *oauth.OAuthCredentials, apiKey string) objects.UpstreamCredentialSecret {
	if creds == nil {
		return objects.UpstreamCredentialSecret{APIKey: strings.TrimSpace(apiKey)}
	}
	if strings.TrimSpace(apiKey) == "" {
		encoded, err := creds.ToJSON()
		if err == nil {
			apiKey = encoded
		}
	}
	return objects.UpstreamCredentialSecret{
		APIKey: strings.TrimSpace(apiKey),
		OAuth:  creds,
	}
}

func credentialImportResponseFromEntity(credential *ent.UpstreamCredential) *CredentialImportResponse {
	if credential == nil {
		return nil
	}
	return &CredentialImportResponse{
		ID:           credential.ID,
		Name:         credential.Name,
		Status:       credential.Status.String(),
		ProviderType: credential.ProviderType,
		BaseURL:      credential.BaseURL,
	}
}
