package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

// PersistenceState holds shared state with channel management and retry capabilities.
// TODO: move the dependencies out of the state to make it a real state.
type PersistenceState struct {
	APIKey *ent.APIKey

	RequestService      *biz.RequestService
	UsageLogService     *biz.UsageLogService
	ChannelService      *biz.ChannelService
	PromptProvider      PromptProvider
	PromptProtecter     PromptProtecter
	RetryPolicyProvider RetryPolicyProvider
	CandidateSelector   CandidateSelector

	// Request state
	ModelMapper *ModelMapper
	// Proxy config, will be used to override channel's default proxy config.
	Proxy *httpclient.ProxyConfig

	// OriginalModel is the model after API key profile mapping, used for channel selection
	OriginalModel string
	RawRequest    *httpclient.Request
	LlmRequest    *llm.Request

	// OriginalRequestStream stores the client's original stream intent before any
	// candidate-specific forcing to provider-side streaming happens.
	OriginalRequestStream *bool

	// Persistence state
	Request     *ent.Request
	RequestExec *ent.RequestExecution

	// ChannelModelsCandidates is the primary state for channel selection
	ChannelModelsCandidates []*ChannelModelsCandidate
	// StickyKey stores the extracted sticky-session key for successful binding refresh.
	StickyKey string
	// StickyKeyOK is true when StickyKey is suitable for sticky-session binding.
	StickyKeyOK bool
	// StickyKeyReason explains why sticky extraction did or did not produce a key.
	StickyKeyReason string
	// StickyLookups stores ordered sticky-session lookup aliases for routing.
	StickyLookups []StickyLookup
	// StickyBindings stores aliases to refresh when the request completes successfully.
	StickyBindings []StickyLookup
	// StickyBasePayload stores the routing-time sticky payload before outbound mutates request fields.
	StickyBasePayload stickyKeyPayload
	// StickyBasePayloadOK is true when StickyBasePayload contains routing-time identity data.
	StickyBasePayloadOK bool
	// StickyResponseID stores the structured upstream response ID captured after response conversion.
	StickyResponseID string
	// StickyPreviousResponseID stores the structured upstream previous response ID captured after response conversion.
	StickyPreviousResponseID string
	// StickyResponseMessage stores the completed assistant message when available for transcript-prefix binding.
	StickyResponseMessage *llm.Message
	// StickyRoutingSource describes which sticky routing semantic path selected the current primary target.
	StickyRoutingSource string
	// StickyRoutingDegradeReason explains why sticky routing left its primary rebind policy when known.
	StickyRoutingDegradeReason string
	// CurrentCredentialID stores the upstream credential row selected for the current attempt when known.
	CurrentCredentialID int
	// CurrentCredentialFingerprint stores the safe upstream credential identity selected for the current attempt.
	CurrentCredentialFingerprint string
	// CurrentSecretFingerprint stores the safe secret-only identity selected for the current attempt.
	CurrentSecretFingerprint string
	// CurrentResourceScopeKey stores the channel/resource scoped credential identity selected for the current attempt.
	CurrentResourceScopeKey string
	// CurrentCredentialName stores a safe display name snapshot for the selected credential.
	CurrentCredentialName string
	// CurrentCredentialKeyHint stores a non-secret key/account hint for the selected credential.
	CurrentCredentialKeyHint string
	// CurrentCredentialSource stores whether the credential came from refs or legacy channel config.
	CurrentCredentialSource string
	// CurrentCredentialQuotaStatus stores the latest credential quota status known at selection time.
	CurrentCredentialQuotaStatus string
	// CurrentQuotaScopeID stores the quota scope row selected for the current attempt when known.
	CurrentQuotaScopeID int
	// CurrentQuotaScopeName stores a safe quota scope display snapshot for the current attempt.
	CurrentQuotaScopeName string
	// CurrentQuotaScopeStatus stores the selected quota scope status snapshot for the current attempt.
	CurrentQuotaScopeStatus string
	// PreferredCredentialID stores the sticky credential row selected by routing when known.
	PreferredCredentialID int
	// PreferredCredentialFingerprint stores the sticky credential identity selected by routing.
	PreferredCredentialFingerprint string
	// ExcludedCredentialIDs stores request-scoped credentials that failed and
	// should not be selected again by the current fallback chain.
	ExcludedCredentialIDs []int
	// ExcludedCredentialFingerprints stores request-scoped credential
	// fingerprints that failed and should not be selected again by the current
	// fallback chain.
	ExcludedCredentialFingerprints []string
	// FallbackTargetSwitches counts execution-target changes after failures.
	// Target switches include same-channel credential fallback and cross-channel
	// fallback. Same-target retry is tracked separately by the pipeline.
	FallbackTargetSwitches int
	// CurrentCredentialAPIKey stores the raw upstream API key selected for the current attempt.
	// It is used only for existing auto-disable logic and must not be logged in full.
	CurrentCredentialAPIKey string
	// Candidate state - current candidate index of ChannelModelsCandidates
	CurrentCandidateIndex int
	// CurrentCandidate is the currently selected candidate of ChannelModelsCandidates
	CurrentCandidate *ChannelModelsCandidate
	// CurrentModelIndex is the current model index in CurrentCandidate.Models
	CurrentModelIndex int

	// Perf is the performance record for the current request.
	Perf *biz.PerformanceRecord

	// StreamCompleted tracks whether the stream has response successfully completed.
	// This is used to distinguish between a stream that was canceled mid-way
	// versus a stream that completed successfully but the client disconnected
	// immediately after receiving the last chunk.
	StreamCompleted bool

	// RawProviderResponse stores the raw provider response for non-stream response pass-through.
	RawProviderResponse *httpclient.Response

	// RawProviderRequest stores the actual outbound provider request for pass-through checks.
	RawProviderRequest *httpclient.Request

	// RawStreamCh receives raw provider stream events for stream response pass-through.
	RawStreamCh chan *httpclient.StreamEvent

	// RawStreamErrRef points to the current attempt's local error variable used by the
	// captureRawProviderStream fan-out goroutine. Using a per-attempt pointer (instead of
	// a single shared field) prevents data races when retries spawn a new goroutine before
	// the previous one has exited.
	RawStreamErrRef *error

	// RawStreamCancel cancels the current attempt's fan-out goroutine started by
	// captureRawProviderStream. Must be called in PrepareForRetry and NextChannel so the
	// abandoned goroutine exits promptly and releases its upstream HTTP connection.
	RawStreamCancel context.CancelFunc

	// PassThroughApplied records whether the inbound request body was reused for
	// the current upstream execution attempt.
	PassThroughApplied bool
}
