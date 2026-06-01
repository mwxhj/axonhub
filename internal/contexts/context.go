package contexts

import (
	"context"
	"slices"

	"github.com/looplj/axonhub/internal/ent"
)

// ContextKey defines the context key type.
type ContextKey string

const (
	// containerContextKey is used to store the context container in the context.
	containerContextKey ContextKey = "context_container"
)

// WithAPIKey stores the API key entity in the context.
func WithAPIKey(ctx context.Context, apiKey *ent.APIKey) context.Context {
	container := getContainer(ctx)
	container.APIKey = apiKey

	return withContainer(ctx, container)
}

// GetAPIKey retrieves the API key entity from the context.
func GetAPIKey(ctx context.Context) (*ent.APIKey, bool) {
	container := getContainer(ctx)
	return container.APIKey, container.APIKey != nil
}

// GetAPIKeyString retrieves the API key string from the context (for backward compatibility).
func GetAPIKeyString(ctx context.Context) (string, bool) {
	apiKey, ok := GetAPIKey(ctx)
	if !ok || apiKey == nil {
		return "", false
	}

	return apiKey.Key, true
}

// WithUser stores the user entity in the context.
func WithUser(ctx context.Context, user *ent.User) context.Context {
	container := getContainer(ctx)
	container.User = user

	return withContainer(ctx, container)
}

// GetUser retrieves the user entity from the context.
func GetUser(ctx context.Context) (*ent.User, bool) {
	container := getContainer(ctx)
	return container.User, container.User != nil
}

// WithTraceID stores the trace id in the context.
func WithTraceID(ctx context.Context, traceID string) context.Context {
	container := getContainer(ctx)
	container.TraceID = &traceID

	return withContainer(ctx, container)
}

// GetTraceID retrieves the trace id from the context.
func GetTraceID(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.TraceID != nil {
		return *container.TraceID, true
	}

	return "", false
}

// WithOperationName stores the operation name in the context.
func WithOperationName(ctx context.Context, name string) context.Context {
	container := getContainer(ctx)
	container.OperationName = &name

	return withContainer(ctx, container)
}

// GetOperationName retrieves the operation name from the context.
func GetOperationName(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.OperationName != nil {
		return *container.OperationName, true
	}

	return "", false
}

// WithRequestID stores the request id in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	container := getContainer(ctx)
	container.RequestID = &requestID

	return withContainer(ctx, container)
}

// GetRequestID retrieves the request id from the context.
func GetRequestID(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.RequestID != nil {
		return *container.RequestID, true
	}

	return "", false
}

// WithChannelAPIKey stores the channel API key in the context.
func WithChannelAPIKey(ctx context.Context, apiKey string) context.Context {
	container := getContainer(ctx)
	container.ChannelAPIKey = &apiKey

	return withContainer(ctx, container)
}

// GetChannelAPIKey retrieves the channel API key from the context.
func GetChannelAPIKey(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelAPIKey != nil {
		return *container.ChannelAPIKey, true
	}

	return "", false
}

// WithChannelCredential stores the selected channel credential metadata in the context.
func WithChannelCredential(ctx context.Context, credentialID int, apiKey string, fingerprint string) context.Context {
	container := getContainer(ctx)
	container.ChannelAPIKey = &apiKey
	container.ChannelCredentialFingerprint = &fingerprint
	if credentialID > 0 {
		container.ChannelCredentialID = &credentialID
	} else {
		container.ChannelCredentialID = nil
	}

	return withContainer(ctx, container)
}

// WithChannelCredentialIdentity stores safe selected credential identity values.
func WithChannelCredentialIdentity(ctx context.Context, secretFingerprint string, resourceScopeKey string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialSecretFingerprint = &secretFingerprint
	container.ChannelCredentialResourceScopeKey = &resourceScopeKey

	return withContainer(ctx, container)
}

// WithChannelCredentialMetadata stores safe selected credential display metadata.
func WithChannelCredentialMetadata(ctx context.Context, name string, keyHint string, source string, quotaStatus string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialName = &name
	container.ChannelCredentialKeyHint = &keyHint
	container.ChannelCredentialSource = &source
	container.ChannelCredentialQuotaStatus = &quotaStatus

	return withContainer(ctx, container)
}

// WithChannelCredentialQuotaScope stores selected quota scope snapshot metadata.
func WithChannelCredentialQuotaScope(ctx context.Context, quotaScopeID int, name string, status string) context.Context {
	container := getContainer(ctx)
	if quotaScopeID > 0 {
		container.ChannelCredentialQuotaScopeID = &quotaScopeID
	} else {
		container.ChannelCredentialQuotaScopeID = nil
	}
	container.ChannelCredentialQuotaScopeName = &name
	container.ChannelCredentialQuotaScopeStatus = &status

	return withContainer(ctx, container)
}

// WithChannelCredentialID stores the selected channel credential row ID in the context.
func WithChannelCredentialID(ctx context.Context, credentialID int) context.Context {
	container := getContainer(ctx)
	if credentialID > 0 {
		container.ChannelCredentialID = &credentialID
	} else {
		container.ChannelCredentialID = nil
	}

	return withContainer(ctx, container)
}

// GetChannelCredentialID retrieves the selected channel credential row ID from the context.
func GetChannelCredentialID(ctx context.Context) (int, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialID != nil {
		return *container.ChannelCredentialID, true
	}

	return 0, false
}

// WithChannelCredentialName stores the selected credential display name.
func WithChannelCredentialName(ctx context.Context, name string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialName = &name

	return withContainer(ctx, container)
}

// GetChannelCredentialName retrieves the selected credential display name.
func GetChannelCredentialName(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialName != nil {
		return *container.ChannelCredentialName, true
	}

	return "", false
}

// WithChannelCredentialKeyHint stores the selected credential safe key hint.
func WithChannelCredentialKeyHint(ctx context.Context, keyHint string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialKeyHint = &keyHint

	return withContainer(ctx, container)
}

// GetChannelCredentialKeyHint retrieves the selected credential safe key hint.
func GetChannelCredentialKeyHint(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialKeyHint != nil {
		return *container.ChannelCredentialKeyHint, true
	}

	return "", false
}

// WithChannelCredentialSource stores the selected credential source.
func WithChannelCredentialSource(ctx context.Context, source string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialSource = &source

	return withContainer(ctx, container)
}

// GetChannelCredentialSource retrieves the selected credential source.
func GetChannelCredentialSource(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialSource != nil {
		return *container.ChannelCredentialSource, true
	}

	return "", false
}

// WithChannelCredentialQuotaStatus stores the selected credential quota status.
func WithChannelCredentialQuotaStatus(ctx context.Context, status string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialQuotaStatus = &status

	return withContainer(ctx, container)
}

// GetChannelCredentialQuotaStatus retrieves the selected credential quota status.
func GetChannelCredentialQuotaStatus(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialQuotaStatus != nil {
		return *container.ChannelCredentialQuotaStatus, true
	}

	return "", false
}

// WithChannelCredentialSecretFingerprint stores the selected secret-only fingerprint in the context.
func WithChannelCredentialSecretFingerprint(ctx context.Context, fingerprint string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialSecretFingerprint = &fingerprint

	return withContainer(ctx, container)
}

// GetChannelCredentialSecretFingerprint retrieves the selected secret-only fingerprint from the context.
func GetChannelCredentialSecretFingerprint(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialSecretFingerprint != nil {
		return *container.ChannelCredentialSecretFingerprint, true
	}

	return "", false
}

// WithChannelCredentialResourceScopeKey stores the selected resource scope in the context.
func WithChannelCredentialResourceScopeKey(ctx context.Context, resourceScopeKey string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialResourceScopeKey = &resourceScopeKey

	return withContainer(ctx, container)
}

// GetChannelCredentialResourceScopeKey retrieves the selected resource scope from the context.
func GetChannelCredentialResourceScopeKey(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialResourceScopeKey != nil {
		return *container.ChannelCredentialResourceScopeKey, true
	}

	return "", false
}

// WithChannelCredentialQuotaScopeID stores the selected quota scope row ID in the context.
func WithChannelCredentialQuotaScopeID(ctx context.Context, quotaScopeID int) context.Context {
	container := getContainer(ctx)
	if quotaScopeID > 0 {
		container.ChannelCredentialQuotaScopeID = &quotaScopeID
	} else {
		container.ChannelCredentialQuotaScopeID = nil
	}

	return withContainer(ctx, container)
}

// GetChannelCredentialQuotaScopeID retrieves the selected quota scope row ID from the context.
func GetChannelCredentialQuotaScopeID(ctx context.Context) (int, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialQuotaScopeID != nil {
		return *container.ChannelCredentialQuotaScopeID, true
	}

	return 0, false
}

// GetChannelCredentialQuotaScopeName retrieves the selected quota scope display name from the context.
func GetChannelCredentialQuotaScopeName(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialQuotaScopeName != nil {
		return *container.ChannelCredentialQuotaScopeName, true
	}

	return "", false
}

// GetChannelCredentialQuotaScopeStatus retrieves the selected quota scope status from the context.
func GetChannelCredentialQuotaScopeStatus(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialQuotaScopeStatus != nil {
		return *container.ChannelCredentialQuotaScopeStatus, true
	}

	return "", false
}

// WithChannelCredentialFingerprint stores the selected channel credential fingerprint in the context.
func WithChannelCredentialFingerprint(ctx context.Context, fingerprint string) context.Context {
	container := getContainer(ctx)
	container.ChannelCredentialFingerprint = &fingerprint

	return withContainer(ctx, container)
}

// GetChannelCredentialFingerprint retrieves the selected channel credential fingerprint from the context.
func GetChannelCredentialFingerprint(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.ChannelCredentialFingerprint != nil {
		return *container.ChannelCredentialFingerprint, true
	}

	return "", false
}

// WithCredentialSelectionSeed stores the stable sticky-session seed for upstream credential selection.
func WithCredentialSelectionSeed(ctx context.Context, seed string) context.Context {
	container := getContainer(ctx)
	container.CredentialSelectionSeed = &seed

	return withContainer(ctx, container)
}

// GetCredentialSelectionSeed retrieves the stable sticky-session seed for upstream credential selection.
func GetCredentialSelectionSeed(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.CredentialSelectionSeed != nil {
		return *container.CredentialSelectionSeed, true
	}

	return "", false
}

// WithPreferredCredential stores the credential row and fingerprint preferred by routing.
func WithPreferredCredential(ctx context.Context, credentialID int, fingerprint string) context.Context {
	container := getContainer(ctx)
	if credentialID > 0 {
		container.PreferredCredentialID = &credentialID
	} else {
		container.PreferredCredentialID = nil
	}
	container.PreferredCredentialFingerprint = &fingerprint

	return withContainer(ctx, container)
}

// WithPreferredCredentialID stores the credential row preferred by routing.
func WithPreferredCredentialID(ctx context.Context, credentialID int) context.Context {
	container := getContainer(ctx)
	if credentialID > 0 {
		container.PreferredCredentialID = &credentialID
	} else {
		container.PreferredCredentialID = nil
	}

	return withContainer(ctx, container)
}

// GetPreferredCredentialID retrieves the credential row preferred by routing.
func GetPreferredCredentialID(ctx context.Context) (int, bool) {
	container := getContainer(ctx)
	if container.PreferredCredentialID != nil {
		return *container.PreferredCredentialID, true
	}

	return 0, false
}

// WithPreferredCredentialFingerprint stores the credential identity preferred by routing.
func WithPreferredCredentialFingerprint(ctx context.Context, fingerprint string) context.Context {
	container := getContainer(ctx)
	container.PreferredCredentialFingerprint = &fingerprint

	return withContainer(ctx, container)
}

// GetPreferredCredentialFingerprint retrieves the credential identity preferred by routing.
func GetPreferredCredentialFingerprint(ctx context.Context) (string, bool) {
	container := getContainer(ctx)
	if container.PreferredCredentialFingerprint != nil {
		return *container.PreferredCredentialFingerprint, true
	}

	return "", false
}

// WithAllowedCredentials stores the credential identities routing kept for the
// current candidate. Channel credential providers must not select credentials
// outside this set when it is non-empty.
func WithAllowedCredentials(ctx context.Context, credentialIDs []int, fingerprints []string) context.Context {
	container := getContainer(ctx)
	container.AllowedCredentialIDs = slices.Clone(credentialIDs)
	container.AllowedCredentialFingerprints = slices.Clone(fingerprints)

	return withContainer(ctx, container)
}

// GetAllowedCredentials retrieves the candidate-scoped credential allow-list.
func GetAllowedCredentials(ctx context.Context) ([]int, []string, bool) {
	container := getContainer(ctx)
	if len(container.AllowedCredentialIDs) == 0 && len(container.AllowedCredentialFingerprints) == 0 {
		return nil, nil, false
	}

	return slices.Clone(container.AllowedCredentialIDs), slices.Clone(container.AllowedCredentialFingerprints), true
}

// WithProjectID stores the project ID in the context.
func WithProjectID(ctx context.Context, projectID int) context.Context {
	container := getContainer(ctx)
	container.ProjectID = &projectID

	return withContainer(ctx, container)
}

// GetProjectID retrieves the project ID from the context.
func GetProjectID(ctx context.Context) (int, bool) {
	container := getContainer(ctx)
	if container.ProjectID != nil {
		return *container.ProjectID, true
	}

	return 0, false
}

// AddError appends an error to the context's error list.
// Will do nothing if the context is not initialized.
// But in real world, it should be initialized.
func AddError(ctx context.Context, err error) {
	if err == nil {
		return
	}

	container := getContainer(ctx)

	container.mu.Lock()
	defer container.mu.Unlock()

	container.Errors = append(container.Errors, err)
}

// GetErrors retrieves all errors from the context.
// Will return nil if the context is not initialized.
// But in real world, it should be initialized.
func GetErrors(ctx context.Context) []error {
	container := getContainer(ctx)

	container.mu.RLock()
	defer container.mu.RUnlock()

	return slices.Clone(container.Errors)
}
