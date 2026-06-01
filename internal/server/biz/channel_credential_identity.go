package biz

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
)

const (
	channelCredentialFingerprintVersion = "cred:v1"
	credentialSecretFingerprintVersion  = "secret:v1"
	channelCredentialAuthKindAPIKey     = "api_key"
	channelCredentialAuthKindOAuth      = "oauth"
	channelCredentialAuthKindAzure      = "azure"
	channelCredentialAuthKindGCP        = "gcp"
	channelCredentialAuthKindOther      = "other"
)

const (
	ChannelCredentialSourceRef    = "ref"
	ChannelCredentialSourceLegacy = "legacy"
)

// ChannelCredentialView is the runtime execution identity resolved for a
// channel. It decouples channel policy from the upstream account/cache/quota
// resource while preserving legacy transformer inputs.
type ChannelCredentialView struct {
	CredentialID              int
	Name                      string
	Fingerprint               string
	SecretFingerprint         string
	ResourceScopeKey          string
	AuthKind                  string
	SecretKind                string
	IssuerScope               string
	KeyHint                   string
	QuotaStatus               string
	QuotaScopeID              int
	QuotaScopeName            string
	QuotaScopeStatus          string
	QuotaScopeOverLimitAction string
	QuotaScopePauseUntil      *time.Time
	QuotaScopeResetPolicy     string
	QuotaScopeResetAt         *time.Time
	Secret                    objects.UpstreamCredentialSecret
	Enabled                   bool
	Weight                    int
	Source                    string
}

// ChannelCredentialFingerprintForAPIKey returns a stable, non-secret identity
// for an upstream API key within a coarse issuer scope. The baseURL parameter is
// kept for legacy callers, but channel endpoint routing must not split
// credential identity by default.
func ChannelCredentialFingerprintForAPIKey(provider string, baseURL string, apiKey string) string {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return ""
	}

	return channelCredentialFingerprint(CredentialIssuerScope(provider, baseURL), channelCredentialAuthKindAPIKey, apiKey)
}

func ChannelCredentialFingerprintForSecret(provider string, baseURL string, authKind string, secret objects.UpstreamCredentialSecret) string {
	return CredentialFingerprintForSecret(CredentialIssuerScope(provider, baseURL), CredentialSecretKindForAuthKind(authKind, secret), secret)
}

func CredentialFingerprintForSecret(issuerScope string, secretKind string, secret objects.UpstreamCredentialSecret) string {
	switch normalizeCredentialFingerprintPart(secretKind) {
	case channelCredentialAuthKindAPIKey:
		return channelCredentialFingerprint(issuerScope, channelCredentialAuthKindAPIKey, secret.APIKey)
	case channelCredentialAuthKindOAuth:
		return channelCredentialFingerprint(issuerScope, channelCredentialAuthKindOAuth, stableOAuthCredentialMaterial(secret))
	case channelCredentialAuthKindAzure:
		return channelCredentialFingerprint(issuerScope, channelCredentialAuthKindAzure, credentialSecretMaterialForAuthKind(channelCredentialAuthKindAzure, secret))
	case channelCredentialAuthKindGCP:
		return channelCredentialFingerprint(issuerScope, channelCredentialAuthKindGCP, credentialSecretMaterialForAuthKind(channelCredentialAuthKindGCP, secret))
	default:
		return channelCredentialFingerprint(issuerScope, channelCredentialAuthKindOther, credentialSecretMaterialForAuthKind(secretKind, secret))
	}
}

func CredentialSecretFingerprintForSecret(secretKind string, secret objects.UpstreamCredentialSecret) string {
	normalizedKind := normalizeCredentialFingerprintPart(secretKind)
	if normalizedKind == "" {
		normalizedKind = CredentialSecretKindForSecret(secret)
	}

	return credentialSecretFingerprint(normalizedKind, credentialSecretMaterialForAuthKind(normalizedKind, secret))
}

func CredentialSecretKindForAuthKind(authKind string, secret objects.UpstreamCredentialSecret) string {
	normalized := normalizeCredentialFingerprintPart(authKind)
	if normalized != "" && normalized != channelCredentialAuthKindOther {
		return normalized
	}

	return CredentialSecretKindForSecret(secret)
}

func ChannelResourceScope(ch *ent.Channel) string {
	if ch == nil {
		return "unknown"
	}

	provider := normalizeCredentialFingerprintPart(ch.Type.String())
	switch provider {
	case "openai", "openai_responses", "codex":
		return providerResourceScope("openai", ch.BaseURL, "api.openai.com", "chatgpt.com")
	case "anthropic", "anthropic_fake":
		return providerResourceScope("anthropic", ch.BaseURL, "api.anthropic.com")
	case "anthropic_aws":
		return "anthropic_aws"
	case "anthropic_gcp", "claudecode":
		return providerResourceScope("anthropic_gcp", ch.BaseURL, "api.anthropic.com", "console.anthropic.com")
	case "gemini", "gemini_openai":
		return providerResourceScope("google_gemini", ch.BaseURL, "generativelanguage.googleapis.com")
	case "gemini_vertex":
		return providerResourceScope("google_vertex", ch.BaseURL, "aiplatform.googleapis.com")
	case "deepseek", "deepseek_anthropic":
		return "deepseek"
	case "moonshot", "moonshot_anthropic", "moonshot_coding":
		return "moonshot"
	case "zhipu", "zhipu_anthropic", "zai", "zai_anthropic":
		return "zai"
	case "xiaomi", "xiaomi_anthropic":
		return "xiaomi"
	case "volcengine", "volcengine_anthropic":
		return "volcengine"
	case "longcat", "longcat_anthropic":
		return "longcat"
	case "minimax", "minimax_anthropic":
		return "minimax"
	case "aihubmix", "aihubmix_anthropic":
		return "aihubmix"
	case "bailian", "bailian_anthropic":
		return "bailian"
	case "nanogpt", "nanogpt_responses":
		return "nanogpt"
	}

	if host := normalizedCredentialHost(ch.BaseURL); host != "" {
		return host
	}
	if provider != "" {
		return provider
	}

	return "unknown"
}

func providerResourceScope(provider string, baseURL string, officialHosts ...string) string {
	host := normalizedCredentialHost(baseURL)
	if host == "" {
		return provider
	}

	for _, officialHost := range officialHosts {
		officialHost = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(officialHost)), ".")
		if officialHost == "" {
			continue
		}
		if host == officialHost || strings.HasSuffix(host, "."+officialHost) {
			return provider
		}
	}

	return host
}

func ChannelCredentialResourceScopeKey(ch *ent.Channel, secretFingerprint string) string {
	secretFingerprint = strings.TrimSpace(secretFingerprint)
	if secretFingerprint == "" {
		return ""
	}

	return ChannelResourceScope(ch) + ":" + secretFingerprint
}

func CredentialSecretKindForSecret(secret objects.UpstreamCredentialSecret) string {
	switch {
	case secret.OAuth != nil:
		return channelCredentialAuthKindOAuth
	case secret.GCP != nil:
		return channelCredentialAuthKindGCP
	case secret.Azure != nil:
		return channelCredentialAuthKindAzure
	case strings.TrimSpace(secret.APIKey) != "":
		return channelCredentialAuthKindAPIKey
	default:
		return channelCredentialAuthKindOther
	}
}

func CredentialKeyHintForSecret(secretKind string, secret objects.UpstreamCredentialSecret) string {
	switch normalizeCredentialFingerprintPart(secretKind) {
	case channelCredentialAuthKindOAuth:
		if identity := stableOAuthCredentialMaterial(secret); identity != "" && !strings.Contains(identity, "token:") {
			return safeHint(identity)
		}
		return safeHint(secret.APIKey)
	case channelCredentialAuthKindGCP:
		if secret.GCP != nil && strings.TrimSpace(secret.GCP.ProjectID) != "" {
			return "gcp:" + strings.TrimSpace(secret.GCP.ProjectID)
		}
	case channelCredentialAuthKindAzure:
		if secret.Azure != nil && strings.TrimSpace(secret.Azure.APIVersion) != "" {
			return "azure:" + strings.TrimSpace(secret.Azure.APIVersion)
		}
	}

	return safeHint(secret.APIKey)
}

func CredentialIssuerScope(provider string, baseURL string) string {
	provider = normalizeCredentialFingerprintPart(provider)
	switch provider {
	case "openai", "openai_responses", "codex":
		return "openai"
	case "anthropic", "anthropic_fake":
		return "anthropic"
	case "anthropic_aws":
		return "anthropic_aws"
	case "anthropic_gcp", "claudecode":
		return "anthropic_gcp"
	case "gemini", "gemini_openai":
		return "google_gemini"
	case "gemini_vertex":
		return "google_vertex"
	case "deepseek", "deepseek_anthropic":
		return "deepseek"
	case "moonshot", "moonshot_anthropic", "moonshot_coding":
		return "moonshot"
	case "zhipu", "zhipu_anthropic", "zai", "zai_anthropic":
		return "zai"
	case "xiaomi", "xiaomi_anthropic":
		return "xiaomi"
	case "volcengine", "volcengine_anthropic":
		return "volcengine"
	case "longcat", "longcat_anthropic":
		return "longcat"
	case "minimax", "minimax_anthropic":
		return "minimax"
	case "aihubmix", "aihubmix_anthropic":
		return "aihubmix"
	case "bailian", "bailian_anthropic":
		return "bailian"
	case "nanogpt", "nanogpt_responses":
		return "nanogpt"
	}
	if provider != "" {
		return provider
	}

	if host := normalizedCredentialHost(baseURL); host != "" {
		return host
	}

	return "unknown"
}

// CredentialFingerprintForAPIKey returns a stable, non-secret identity for one
// of this channel's API keys.
func (c *Channel) CredentialFingerprintForAPIKey(apiKey string) string {
	if c == nil || c.Channel == nil {
		return ""
	}

	return ChannelCredentialFingerprintForAPIKey(c.Type.String(), c.BaseURL, apiKey)
}

func (c *Channel) CredentialViews() []ChannelCredentialView {
	if c == nil {
		return nil
	}

	if len(c.cachedCredentialViews) > 0 {
		return runtimeCredentialViews(c.cachedCredentialViews)
	}

	return runtimeCredentialViews(legacyCredentialViews(c.Channel))
}

// WithCredentialViewsForSelection returns a shallow channel snapshot whose
// credential selection is constrained to the supplied runtime views. It keeps
// the existing outbound transformers, but narrows the channel credential
// metadata and legacy credentials used by routing/selection fallback paths.
func (c *Channel) WithCredentialViewsForSelection(views []ChannelCredentialView) *Channel {
	if c == nil {
		return nil
	}

	views = dedupeCredentialViews(slices.Clone(views))

	clone := *c
	clone.cachedCredentialViews = views
	clone.cachedEnabledAPIKeys = enabledAPIKeysFromCredentialViews(views)
	if c.Channel != nil {
		entClone := *c.Channel
		entClone.Credentials = credentialsFromViews(views, c.Channel.Credentials)
		clone.Channel = &entClone
	}

	return &clone
}

func (c *Channel) enabledCredentialViews() []ChannelCredentialView {
	if c == nil {
		return nil
	}

	views := c.cachedCredentialViews
	if len(views) == 0 {
		views = legacyCredentialViews(c.Channel)
	}

	enabled := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if view.Enabled && credentialViewQuotaSelectable(view) {
			enabled = append(enabled, view)
		}
	}

	return enabled
}

func enabledAPIKeyCredentialViews(views []ChannelCredentialView) []ChannelCredentialView {
	result := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if !view.Enabled || !credentialViewQuotaSelectable(view) || strings.TrimSpace(view.Secret.APIKey) == "" {
			continue
		}

		if normalizeCredentialFingerprintPart(view.AuthKind) != channelCredentialAuthKindAPIKey {
			continue
		}

		result = append(result, view)
	}

	return result
}

func authCapableAPIKeyCredentialViews(views []ChannelCredentialView) []ChannelCredentialView {
	result := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		if !view.Enabled || strings.TrimSpace(view.Secret.APIKey) == "" {
			continue
		}

		if normalizeCredentialFingerprintPart(view.AuthKind) != channelCredentialAuthKindAPIKey {
			continue
		}

		result = append(result, view)
	}

	return result
}

func enabledAPIKeysFromCredentialViews(views []ChannelCredentialView) []string {
	apiKeyViews := enabledAPIKeyCredentialViews(views)
	result := make([]string, 0, len(apiKeyViews))
	for _, view := range apiKeyViews {
		if key := strings.TrimSpace(view.Secret.APIKey); key != "" {
			result = append(result, key)
		}
	}

	return result
}

func authCapableAPIKeysFromCredentialViews(views []ChannelCredentialView) []string {
	apiKeyViews := authCapableAPIKeyCredentialViews(views)
	result := make([]string, 0, len(apiKeyViews))
	for _, view := range apiKeyViews {
		if key := strings.TrimSpace(view.Secret.APIKey); key != "" {
			result = append(result, key)
		}
	}

	return result
}

func legacyCredentialViewsForAPIKeys(c *Channel, keys []string) []ChannelCredentialView {
	if c == nil {
		return nil
	}

	views := make([]ChannelCredentialView, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		fingerprint := c.CredentialFingerprintForAPIKey(key)
		if fingerprint == "" {
			continue
		}
		secret := objects.UpstreamCredentialSecretFromAPIKey(key)
		secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)

		views = append(views, ChannelCredentialView{
			Fingerprint:       fingerprint,
			SecretFingerprint: secretFingerprint,
			ResourceScopeKey:  ChannelCredentialResourceScopeKey(c.Channel, secretFingerprint),
			AuthKind:          channelCredentialAuthKindAPIKey,
			SecretKind:        channelCredentialAuthKindAPIKey,
			IssuerScope:       CredentialIssuerScope(c.Type.String(), c.BaseURL),
			KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret),
			Secret:            secret,
			Enabled:           true,
			Weight:            1,
			Source:            ChannelCredentialSourceLegacy,
		})
	}

	return views
}

// EnabledCredentialFingerprints returns the safe identities for this channel's
// currently enabled API-key credentials.
func (c *Channel) EnabledCredentialFingerprints() []string {
	if c == nil {
		return nil
	}

	views := c.enabledCredentialViews()
	result := make([]string, 0, len(views))
	for _, view := range views {
		if view.Fingerprint != "" && !slices.Contains(result, view.Fingerprint) {
			result = append(result, view.Fingerprint)
		}
	}

	return result
}

// HasEnabledCredentialID reports whether this channel can use the given
// first-class upstream credential row ID.
func (c *Channel) HasEnabledCredentialID(credentialID int) bool {
	if credentialID <= 0 || c == nil {
		return false
	}

	return slices.ContainsFunc(c.enabledCredentialViews(), func(view ChannelCredentialView) bool {
		return view.CredentialID == credentialID
	})
}

// HasEnabledCredentialFingerprint reports whether this channel can use the
// given upstream credential fingerprint.
func (c *Channel) HasEnabledCredentialFingerprint(fingerprint string) bool {
	if fingerprint == "" || c == nil {
		return false
	}

	return slices.Contains(c.EnabledCredentialFingerprints(), fingerprint)
}

func (c *Channel) legacyCredentialViews() []ChannelCredentialView {
	if c == nil {
		return nil
	}

	return legacyCredentialViews(c.Channel)
}

func credentialViewsFromRefs(c *ent.Channel) []ChannelCredentialView {
	if c == nil {
		return nil
	}

	refs, err := c.Edges.CredentialRefsOrErr()
	if err != nil || len(refs) == 0 {
		return nil
	}

	views := make([]ChannelCredentialView, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}

		credential, err := ref.Edges.CredentialOrErr()
		if err != nil || credential == nil {
			continue
		}

		enabled := ref.Enabled && credential.Status == upstreamcredential.StatusEnabled

		secretFingerprint := credential.SecretFingerprint
		if secretFingerprint == nil || strings.TrimSpace(*secretFingerprint) == "" {
			computed := CredentialSecretFingerprintForSecret(credential.SecretKind.String(), credential.SecretPayload)
			secretFingerprint = &computed
		}

		view := ChannelCredentialView{
			CredentialID:      credential.ID,
			Name:              credential.Name,
			Fingerprint:       credential.Fingerprint,
			SecretFingerprint: strings.TrimSpace(*secretFingerprint),
			AuthKind:          credential.AuthKind.String(),
			SecretKind:        credential.SecretKind.String(),
			IssuerScope:       credential.IssuerScope,
			KeyHint:           credential.KeyHint,
			QuotaStatus:       credential.QuotaStatus,
			Secret:            credential.SecretPayload,
			Enabled:           enabled,
			Weight:            1,
			Source:            ChannelCredentialSourceRef,
		}
		view.ResourceScopeKey = ChannelCredentialResourceScopeKey(c, view.SecretFingerprint)
		if credential.QuotaScopeID != nil {
			view.QuotaScopeID = *credential.QuotaScopeID
			if quotaScope, err := credential.Edges.QuotaScopeOrErr(); err == nil && quotaScope != nil {
				view.QuotaScopeName = quotaScope.Name
				view.QuotaScopeStatus = quotaScope.Status.String()
				view.QuotaScopeOverLimitAction = quotaScope.OverLimitAction.String()
				view.QuotaScopePauseUntil = quotaScope.PauseUntil
				view.QuotaScopeResetPolicy = quotaScope.ResetPolicy.String()
				view.QuotaScopeResetAt = quotaScope.ResetAt
			}
		}
		views = append(views, view)
	}

	return views
}

func runtimeCredentialViews(views []ChannelCredentialView) []ChannelCredentialView {
	result := slices.Clone(views)
	for i := range result {
		if result[i].Enabled && !credentialViewQuotaSelectable(result[i]) {
			result[i].Enabled = false
		}
	}

	return result
}

func primaryCredentialViewForAuthKind(ch *Channel, authKind string) ChannelCredentialView {
	if ch == nil {
		return ChannelCredentialView{}
	}

	normalizedAuthKind := normalizeCredentialFingerprintPart(authKind)
	for _, view := range ch.cachedCredentialViews {
		if !view.Enabled || !credentialViewQuotaSelectable(view) {
			continue
		}
		if normalizeCredentialFingerprintPart(view.AuthKind) == normalizedAuthKind {
			return view
		}
	}

	return ChannelCredentialView{}
}

func legacyCredentialViews(c *ent.Channel) []ChannelCredentialView {
	if c == nil {
		return nil
	}

	views := make([]ChannelCredentialView, 0, len(c.Credentials.GetAllAPIKeys())+3)

	if c.Credentials.IsOAuth() {
		secret := objects.UpstreamCredentialSecret{
			APIKey: strings.TrimSpace(c.Credentials.APIKey),
			OAuth:  c.Credentials.OAuth,
		}
		fingerprint := ChannelCredentialFingerprintForSecret(c.Type.String(), c.BaseURL, channelCredentialAuthKindOAuth, secret)
		if fingerprint != "" {
			secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindOAuth, secret)
			views = append(views, ChannelCredentialView{
				Fingerprint:       fingerprint,
				SecretFingerprint: secretFingerprint,
				ResourceScopeKey:  ChannelCredentialResourceScopeKey(c, secretFingerprint),
				AuthKind:          channelCredentialAuthKindOAuth,
				SecretKind:        channelCredentialAuthKindOAuth,
				IssuerScope:       CredentialIssuerScope(c.Type.String(), c.BaseURL),
				KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindOAuth, secret),
				Secret:            secret,
				Enabled:           true,
				Weight:            1,
				Source:            ChannelCredentialSourceLegacy,
			})
		}

		return views
	}

	for _, key := range c.Credentials.GetAllAPIKeys() {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fingerprint := ChannelCredentialFingerprintForAPIKey(c.Type.String(), c.BaseURL, key)
		if fingerprint == "" {
			continue
		}
		secret := objects.UpstreamCredentialSecretFromAPIKey(key)
		secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAPIKey, secret)

		views = append(views, ChannelCredentialView{
			Fingerprint:       fingerprint,
			SecretFingerprint: secretFingerprint,
			ResourceScopeKey:  ChannelCredentialResourceScopeKey(c, secretFingerprint),
			AuthKind:          channelCredentialAuthKindAPIKey,
			SecretKind:        channelCredentialAuthKindAPIKey,
			IssuerScope:       CredentialIssuerScope(c.Type.String(), c.BaseURL),
			KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindAPIKey, secret),
			Secret:            secret,
			Enabled:           true,
			Weight:            1,
			Source:            ChannelCredentialSourceLegacy,
		})
	}

	if c.Credentials.GCP != nil {
		secret := objects.UpstreamCredentialSecret{GCP: c.Credentials.GCP}
		fingerprint := ChannelCredentialFingerprintForSecret(c.Type.String(), c.BaseURL, channelCredentialAuthKindGCP, secret)
		if fingerprint != "" {
			secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindGCP, secret)
			views = append(views, ChannelCredentialView{
				Fingerprint:       fingerprint,
				SecretFingerprint: secretFingerprint,
				ResourceScopeKey:  ChannelCredentialResourceScopeKey(c, secretFingerprint),
				AuthKind:          channelCredentialAuthKindGCP,
				SecretKind:        channelCredentialAuthKindGCP,
				IssuerScope:       CredentialIssuerScope(c.Type.String(), c.BaseURL),
				KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindGCP, secret),
				Secret:            secret,
				Enabled:           true,
				Weight:            1,
				Source:            ChannelCredentialSourceLegacy,
			})
		}
	}

	if c.Credentials.Azure != nil {
		secret := objects.UpstreamCredentialSecret{Azure: c.Credentials.Azure}
		fingerprint := ChannelCredentialFingerprintForSecret(c.Type.String(), c.BaseURL, channelCredentialAuthKindAzure, secret)
		if fingerprint != "" {
			secretFingerprint := CredentialSecretFingerprintForSecret(channelCredentialAuthKindAzure, secret)
			views = append(views, ChannelCredentialView{
				Fingerprint:       fingerprint,
				SecretFingerprint: secretFingerprint,
				ResourceScopeKey:  ChannelCredentialResourceScopeKey(c, secretFingerprint),
				AuthKind:          channelCredentialAuthKindAzure,
				SecretKind:        channelCredentialAuthKindAzure,
				IssuerScope:       CredentialIssuerScope(c.Type.String(), c.BaseURL),
				KeyHint:           CredentialKeyHintForSecret(channelCredentialAuthKindAzure, secret),
				Secret:            secret,
				Enabled:           true,
				Weight:            1,
				Source:            ChannelCredentialSourceLegacy,
			})
		}
	}

	return dedupeCredentialViews(views)
}

func credentialsFromViews(views []ChannelCredentialView, fallback objects.ChannelCredentials) objects.ChannelCredentials {
	if len(views) == 0 {
		return fallback
	}

	var creds objects.ChannelCredentials
	for _, view := range views {
		if !view.Enabled || !credentialViewQuotaSelectable(view) {
			continue
		}

		switch normalizeCredentialFingerprintPart(view.AuthKind) {
		case channelCredentialAuthKindAPIKey:
			if view.Secret.APIKey != "" {
				creds.APIKeys = append(creds.APIKeys, view.Secret.APIKey)
				if creds.APIKey == "" {
					creds.APIKey = view.Secret.APIKey
				}
			}
		case channelCredentialAuthKindOAuth:
			if creds.APIKey == "" && creds.OAuth == nil {
				creds.APIKey = view.Secret.APIKey
				creds.OAuth = view.Secret.OAuth
			}
		case channelCredentialAuthKindGCP:
			if creds.GCP == nil {
				creds.GCP = view.Secret.GCP
			}
		case channelCredentialAuthKindAzure:
			if creds.Azure == nil {
				creds.Azure = view.Secret.Azure
			}
		}
	}

	if creds.APIKey == "" && len(creds.APIKeys) == 1 {
		creds.APIKey = creds.APIKeys[0]
	}

	return creds
}

func dedupeCredentialViews(views []ChannelCredentialView) []ChannelCredentialView {
	if len(views) < 2 {
		return views
	}

	seen := make(map[string]struct{}, len(views))
	result := make([]ChannelCredentialView, 0, len(views))
	for _, view := range views {
		key := view.SecretFingerprint
		if key == "" {
			key = view.Fingerprint
		}
		if key == "" && view.Secret.APIKey != "" {
			key = view.Secret.APIKey
		}
		if key == "" {
			result = append(result, view)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, view)
	}

	return result
}

func credentialViewQuotaSelectable(view ChannelCredentialView) bool {
	switch normalizeCredentialFingerprintPart(view.QuotaStatus) {
	case "exhausted", "paused", "disabled":
		return false
	}

	return true
}

func stableCredentialSecretMaterial(value any) string {
	if value == nil {
		return ""
	}

	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}

	material := string(data)
	if material == "null" || material == "{}" {
		return ""
	}

	return material
}

func credentialSecretMaterialForAuthKind(authKind string, secret objects.UpstreamCredentialSecret) string {
	switch normalizeCredentialFingerprintPart(authKind) {
	case channelCredentialAuthKindAPIKey:
		return strings.TrimSpace(secret.APIKey)
	case channelCredentialAuthKindOAuth:
		return stableOAuthCredentialMaterial(secret)
	case channelCredentialAuthKindAzure:
		if material := stableCredentialSecretMaterial(secret.Azure); material != "" {
			return material
		}
		return strings.TrimSpace(secret.APIKey)
	case channelCredentialAuthKindGCP:
		return stableCredentialSecretMaterial(secret.GCP)
	default:
		return stableCredentialSecretMaterial(secret)
	}
}

func stableOAuthCredentialMaterial(secret objects.UpstreamCredentialSecret) string {
	if secret.OAuth != nil {
		if identity := stableOAuthTokenIdentity(secret.OAuth.AccessToken); identity != "" {
			return identity
		}
		if identity := stableOAuthTokenIdentity(secret.OAuth.IDToken); identity != "" {
			return identity
		}
		if refreshToken := strings.TrimSpace(secret.OAuth.RefreshToken); refreshToken != "" {
			return "refresh_token:" + refreshToken
		}
		if material := stableCredentialSecretMaterial(secret.OAuth); material != "" {
			return material
		}
	}

	return stableOAuthRawCredentialMaterial(secret.APIKey)
}

func stableOAuthRawCredentialMaterial(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return raw
	}

	for _, path := range [][]string{
		{"access_token"},
		{"tokens", "access_token"},
		{"id_token"},
		{"tokens", "id_token"},
	} {
		if identity := stableOAuthTokenIdentity(jsonPathString(decoded, path...)); identity != "" {
			return identity
		}
	}

	for _, path := range [][]string{
		{"refresh_token"},
		{"tokens", "refresh_token"},
	} {
		if refreshToken := jsonPathString(decoded, path...); refreshToken != "" {
			return "refresh_token:" + refreshToken
		}
	}

	if material := stableCredentialSecretMaterial(decoded); material != "" {
		return material
	}

	return raw
}

func stableOAuthTokenIdentity(token string) string {
	claims := jwtClaimsFromToken(token)
	if len(claims) == 0 {
		return ""
	}

	for _, path := range [][]string{
		{"https://api.openai.com/auth", "chatgpt_account_id"},
		{"chatgpt_account_id"},
		{"account_id"},
		{"account_uuid"},
		{"sub"},
		{"email"},
	} {
		if value := jsonPathString(claims, path...); value != "" {
			return strings.Join(path, ".") + ":" + value
		}
	}

	return ""
}

func jwtClaimsFromToken(token string) map[string]any {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return nil
		}
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}

	return claims
}

func jsonPathString(value any, path ...string) string {
	current := value
	for _, part := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current, ok = object[part]
		if !ok {
			return ""
		}
	}

	result, ok := current.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(result)
}

func channelCredentialFingerprint(issuerScope string, secretKind string, secret string) string {
	if strings.TrimSpace(secret) == "" {
		return ""
	}

	parts := []string{
		channelCredentialFingerprintVersion,
		normalizeCredentialFingerprintPart(issuerScope),
		normalizeCredentialFingerprintPart(secretKind),
		secret,
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return channelCredentialFingerprintVersion + ":" + hex.EncodeToString(sum[:16])
}

func credentialSecretFingerprint(secretKind string, secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return ""
	}

	mac := hmac.New(sha256.New, []byte(credentialSecretFingerprintVersion))
	_, _ = mac.Write([]byte(normalizeCredentialFingerprintPart(secretKind)))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(secret))
	sum := mac.Sum(nil)

	return credentialSecretFingerprintVersion + ":" + hex.EncodeToString(sum[:16])
}

func normalizeCredentialFingerprintPart(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeCredentialBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(strings.ToLower(value), "/")
	}

	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""

	return parsed.String()
}

func normalizedCredentialHost(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return strings.TrimRight(strings.ToLower(value), "/")
	}

	return strings.ToLower(parsed.Hostname())
}

func safeHint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 4 {
		return fmt.Sprintf("len:%d", len(value))
	}
	if len(value) <= 12 {
		return "..." + value[len(value)-4:]
	}

	return value[:4] + "..." + value[len(value)-6:]
}
