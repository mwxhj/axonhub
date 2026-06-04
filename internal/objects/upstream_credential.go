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

func UpstreamCredentialSecretFromAPIKey(apiKey string) UpstreamCredentialSecret {
	return UpstreamCredentialSecret{
		APIKey: apiKey,
	}
}
