package objects

// UpstreamCredentialSecret stores provider-specific upstream authentication
// material. It is persisted only in sensitive fields and must never be exposed
// by read APIs.
type UpstreamCredentialSecret struct {
	APIKey string            `json:"apiKey,omitempty"`
	OAuth  *OAuthCredentials `json:"oauth,omitempty"`
	Azure  *AzureCredential  `json:"azure,omitempty"`
	GCP    *GCPCredential    `json:"gcp,omitempty"`
	Extra  map[string]any    `json:"extra,omitempty"`
}

// ToChannelCredentials converts a credential secret into the legacy channel
// credential shape consumed by existing outbound transformers.
func (s UpstreamCredentialSecret) ToChannelCredentials() ChannelCredentials {
	return ChannelCredentials{
		APIKey: s.APIKey,
		OAuth:  s.OAuth,
		Azure:  s.Azure,
		GCP:    s.GCP,
	}
}

func UpstreamCredentialSecretFromAPIKey(apiKey string) UpstreamCredentialSecret {
	return UpstreamCredentialSecret{
		APIKey: apiKey,
	}
}
