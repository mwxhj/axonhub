package contexts

import (
	"context"
	"sync"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
)

// contextContainer contains all values in the context.
type contextContainer struct {
	ProjectID     *int
	TraceID       *string
	RequestID     *string
	OperationName *string
	APIKey        *ent.APIKey
	User          *ent.User
	Source        *request.Source
	Thread        *ent.Thread
	Trace         *ent.Trace
	Errors        []error
	mu            sync.RWMutex

	// ChannelAPIKey stores the API key used for the channel request (not the user's API key)
	ChannelAPIKey *string

	// ChannelCredentialFingerprint stores the safe upstream credential identity
	// used for the channel request. It must never contain the raw secret.
	ChannelCredentialFingerprint *string

	// ChannelCredentialID stores the first-class upstream credential row ID
	// selected for the channel request when the channel uses credential refs.
	ChannelCredentialID *int

	// ChannelCredentialName stores a safe display name snapshot for the
	// selected upstream credential.
	ChannelCredentialName *string

	// ChannelCredentialKeyHint stores a non-secret key/account hint for the
	// selected upstream credential.
	ChannelCredentialKeyHint *string

	// ChannelCredentialSource stores whether the selected credential came from
	// first-class refs, legacy inline channel credentials, or another source.
	ChannelCredentialSource *string

	// ChannelCredentialQuotaStatus stores the latest credential quota/budget
	// status known at selection time.
	ChannelCredentialQuotaStatus *string

	// CredentialSelectionSeed stores the sticky-session identity used by
	// channel credential providers to keep related requests on the same
	// upstream credential/cache pool.
	CredentialSelectionSeed *string

	// PreferredCredentialID stores the credential row selected by routing.
	// Channel credential providers should prefer it when the current channel
	// references the credential.
	PreferredCredentialID *int

	// PreferredCredentialFingerprint stores the credential identity selected by
	// the outer router. Channel credential providers should prefer it when the
	// current channel references the credential.
	PreferredCredentialFingerprint *string
}

// getContainer retrieves the existing container from context, or creates a new one and stores it in the context if it doesn't exist.
func getContainer(ctx context.Context) *contextContainer {
	if container, ok := ctx.Value(containerContextKey).(*contextContainer); ok {
		return container
	}

	// If container doesn't exist, create a new one and store it in the context
	container := &contextContainer{}

	return container
}

// withContainer stores the container in the context (if not already stored).
func withContainer(ctx context.Context, container *contextContainer) context.Context {
	if ctx.Value(containerContextKey) == nil {
		return context.WithValue(ctx, containerContextKey, container)
	}

	return ctx
}
