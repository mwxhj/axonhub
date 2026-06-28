package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcontext"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/shared"
)

// OutboundPersistentStream wraps a stream and tracks all responses for final saving to database.
// It implements the streams.Stream interface and handles persistence in the Close method.
//
//nolint:containedctx // Checked.
type OutboundPersistentStream struct {
	ctx context.Context

	RequestService  *biz.RequestService
	UsageLogService *biz.UsageLogService

	stream      streams.Stream[*httpclient.StreamEvent]
	request     *ent.Request
	requestExec *ent.RequestExecution

	transformer    transformer.Outbound
	perf           *biz.PerformanceRecord
	responseChunks []*httpclient.StreamEvent
	closed         bool
	state          *PersistenceState
}

var _ streams.Stream[*httpclient.StreamEvent] = (*OutboundPersistentStream)(nil)

func NewOutboundPersistentStream(
	ctx context.Context,
	stream streams.Stream[*httpclient.StreamEvent],
	request *ent.Request,
	requestExec *ent.RequestExecution,
	requestService *biz.RequestService,
	usageLogService *biz.UsageLogService,
	outboundTransformer transformer.Outbound,
	perf *biz.PerformanceRecord,
	state *PersistenceState,
) *OutboundPersistentStream {
	s := &OutboundPersistentStream{
		ctx:             ctx,
		stream:          stream,
		request:         request,
		requestExec:     requestExec,
		RequestService:  requestService,
		UsageLogService: usageLogService,
		transformer:     outboundTransformer,
		perf:            perf,
		responseChunks:  make([]*httpclient.StreamEvent, 0),
		closed:          false,
		state:           state,
	}

	return s
}

func (ts *OutboundPersistentStream) Next() bool {
	return ts.stream.Next()
}

func (ts *OutboundPersistentStream) Current() *httpclient.StreamEvent {
	event := ts.stream.Current()
	if event != nil {
		ts.responseChunks = append(ts.responseChunks, httpclient.SummarizeBinaryChunk(event))
		// Check if this is a terminal event, which indicates the stream reached a terminal state.
		// For Chat Completions API this is the raw [DONE] event; for Responses API this includes
		// response.completed, response.failed, response.cancelled, and response.incomplete;
		// for Anthropic Messages API this is message_stop.
		if isTerminalStreamEvent(event) {
			ts.state.StreamCompleted = true
		}
	}

	return event
}

func (ts *OutboundPersistentStream) Err() error {
	return ts.stream.Err()
}

func (ts *OutboundPersistentStream) Close() error {
	if ts.closed {
		return nil
	}

	ts.closed = true
	ctx := ts.ctx

	log.Debug(ctx, "Closing persistent stream", log.Int("chunk_count", len(ts.responseChunks)), log.Bool("received_done", ts.state.StreamCompleted))

	streamErr := ts.stream.Err()
	ctxErr := ctx.Err()

	// If we received a terminal event, treat the stream as successfully completed
	// even if there's a context cancellation error. This handles the case where
	// the client disconnects immediately after receiving the last chunk.
	if ts.state.StreamCompleted {
		ts.logFinalizationDecision(ctx, "terminal_event_completed", streamErr, ctxErr, true, nil)
		// Stream completed successfully - perform final persistence
		log.Debug(ctx, "Stream completed successfully (received terminal event), performing final persistence")
		ts.persistResponseChunks(ctx)

		return ts.stream.Close()
	}

	// If there's an explicit stream error (not just context cancellation), treat as failure
	// regardless of what chunks we have. Stream errors indicate the upstream response
	// was incomplete or corrupted.
	if streamErr != nil && !errors.Is(streamErr, context.Canceled) && !errors.Is(streamErr, context.DeadlineExceeded) {
		ts.logFinalizationDecision(ctx, "explicit_stream_error", streamErr, ctxErr, false, nil)
		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
		defer cancel()

		if ts.requestExec != nil {
			if err := ts.RequestService.UpdateRequestExecutionStatusFromError(persistCtx, ts.requestExec.ID, streamErr); err != nil {
				log.Warn(persistCtx, "Failed to update request execution status from error", log.Cause(err))
			}
		}

		return ts.stream.Close()
	}

	var responseBody []byte
	var meta llm.ResponseMeta
	var aggErr error
	aggregatedCompleted := false

	if len(ts.responseChunks) > 0 {
		responseBody, meta, aggErr = ts.transformer.AggregateStreamChunks(context.WithoutCancel(ctx), ts.state.RawProviderRequest, ts.responseChunks)
		aggregatedCompleted = aggErr == nil && isCompletedAggregated(meta)
		ts.logFinalizationDecision(ctx, "aggregated_outbound_chunks", streamErr, ctxErr, aggregatedCompleted, aggErr)
		if aggregatedCompleted {
			log.Debug(ctx, "Stream has valid complete response without terminal event, treating as completed")
			ts.state.StreamCompleted = true
		}
	} else {
		ts.logFinalizationDecision(ctx, "no_outbound_chunks_to_aggregate", streamErr, ctxErr, false, nil)
	}

	// ended without a terminal event / complete aggregated response.
	if (ctxErr != nil || streamErr != nil) && !ts.state.StreamCompleted {
		ts.logFinalizationDecision(ctx, "incomplete_stream_with_error", streamErr, ctxErr, aggregatedCompleted, aggErr)
		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
		defer cancel()

		errToReport := streamErr
		if errToReport == nil {
			errToReport = ctxErr
		}
		if errToReport == nil {
			errToReport = errors.New("stream ended without terminal event or completed response")
		}

		if ts.requestExec != nil {
			if err := ts.RequestService.UpdateRequestExecutionStatusFromError(persistCtx, ts.requestExec.ID, errToReport); err != nil {
				log.Warn(persistCtx, "Failed to update request execution status from error", log.Cause(err))
			}
		}

		return ts.stream.Close()
	}

	if !ts.state.StreamCompleted {
		ts.logFinalizationDecision(ctx, "incomplete_stream_without_terminal_event", streamErr, ctxErr, aggregatedCompleted, aggErr)
		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
		defer cancel()

		errToReport := errors.New("stream ended without terminal event or completed response")
		if ts.requestExec != nil {
			if err := ts.RequestService.UpdateRequestExecutionStatusFromError(persistCtx, ts.requestExec.ID, errToReport); err != nil {
				log.Warn(persistCtx, "Failed to update request execution status from error", log.Cause(err))
			}
		}

		return ts.stream.Close()
	}

	// Stream completed successfully - perform final persistence
	log.Debug(ctx, "Stream completed successfully, performing final persistence")
	decision := "completed_after_aggregation"
	if len(responseBody) == 0 {
		decision = "completed_via_chunk_persistence"
	}
	ts.logFinalizationDecision(ctx, decision, streamErr, ctxErr, aggregatedCompleted, aggErr)

	if len(responseBody) > 0 {
		ts.persistAggregatedResponse(context.WithoutCancel(ctx), responseBody, meta)
	} else {
		ts.persistResponseChunks(ctx)
	}

	return ts.stream.Close()
}

func (ts *OutboundPersistentStream) logFinalizationDecision(ctx context.Context, decision string, streamErr error, ctxErr error, aggregatedCompleted bool, aggregatedErr error) {
	fields := []log.Field{
		log.String("decision", decision),
		log.Bool("terminal_event_seen", ts.state.StreamCompleted),
		log.Int("chunk_count", len(ts.responseChunks)),
		log.String("api_format", string(ts.transformer.APIFormat())),
		log.Bool("aggregated_completed", aggregatedCompleted),
	}

	if streamErr != nil {
		fields = append(fields, log.String("stream_err", streamErr.Error()))
	}
	if ctxErr != nil {
		fields = append(fields, log.String("ctx_err", ctxErr.Error()))
	}
	if aggregatedErr != nil {
		fields = append(fields, log.String("aggregated_err", aggregatedErr.Error()))
	}

	log.Debug(ctx, "Outbound stream finalization decision", fields...)
}

func (ts *OutboundPersistentStream) persistResponseChunks(ctx context.Context) {
	defer func() {
		if cause := recover(); cause != nil {
			log.Warn(ctx, "Failed to persist outbound response chunks", log.Any("cause", cause))
		}
	}()

	// Update request execution with aggregated chunks
	if ts.requestExec != nil {
		// Use context without cancellation to ensure persistence even if client canceled
		persistCtx, cancel := xcontext.DetachWithTimeout(ctx, 10*time.Second)
		defer cancel()

		responseBody, meta, err := ts.transformer.AggregateStreamChunks(persistCtx, ts.state.RawProviderRequest, ts.responseChunks)
		if err != nil {
			log.Warn(persistCtx, "Failed to aggregate chunks using transformer", log.Cause(err))
			return
		}

		ts.persistAggregatedResponse(persistCtx, responseBody, meta)
	}
}

func (ts *OutboundPersistentStream) persistAggregatedResponse(ctx context.Context, responseBody []byte, meta llm.ResponseMeta) {
	if ts.requestExec == nil {
		return
	}

	// Try to create usage log from aggregated response
	if usage := meta.Usage; usage != nil {
		_, err := ts.UsageLogService.CreateUsageLogFromRequest(ctx, ts.request, ts.requestExec, usage)
		if err != nil {
			log.Warn(ctx, "Failed to create usage log from request", log.Cause(err))
		}
	}

	// Build latency metrics from performance record
	var metrics *biz.LatencyMetrics

	if ts.perf != nil {
		firstTokenLatencyMs, requestLatencyMs, _ := ts.perf.Calculate()

		metrics = &biz.LatencyMetrics{
			LatencyMs: &requestLatencyMs,
		}
		if ts.perf.Stream && ts.perf.FirstTokenTime != nil {
			metrics.FirstTokenLatencyMs = &firstTokenLatencyMs
		}
	}

	err := ts.RequestService.UpdateRequestExecutionCompleted(
		ctx,
		ts.requestExec.ID,
		meta.ID,
		responseBody,
		metrics,
		&biz.RequestExecutionUpdateOptions{
			ResponseQualityGuardMatched: ts.requestExec.ResponseQualityGuardMatched,
		},
	)
	if err != nil {
		log.Warn(
			ctx,
			"Failed to update request execution with chunks, trying basic completion",
			log.Cause(err),
		)
	}

	// Save all response chunks at once
	if err := ts.RequestService.SaveRequestExecutionChunks(ctx, ts.requestExec.ID, ts.responseChunks); err != nil {
		log.Warn(ctx, "Failed to save request execution chunks", log.Cause(err))
	}
}

func isCompletedAggregated(meta llm.ResponseMeta) bool {
	return meta.Usage != nil && meta.Usage.CompletionTokens > 0
}

var errSkipCandidateByCircuitBreaker = errors.New("skip candidate by circuit breaker")

// PersistentOutboundTransformer wraps an outbound transformer with shared persistence state.
type PersistentOutboundTransformer struct {
	wrapped transformer.Outbound
	state   *PersistenceState
}

func shouldForceStreamingForCandidate(candidate *ChannelModelsCandidate, req *llm.Request) bool {
	if candidate == nil || candidate.Channel == nil || req == nil {
		return false
	}

	if req.Stream != nil && *req.Stream {
		return false
	}

	if candidate.Channel.Policies.Stream != objects.CapabilityPolicyRequire {
		return false
	}

	return supportsAutoAggregateRequest(req)
}

func selectOutboundForCandidate(candidate *ChannelModelsCandidate) transformer.Outbound {
	if candidate == nil || candidate.Channel == nil {
		return nil
	}

	if candidate.APIFormat != "" && candidate.Channel.Outbounds != nil {
		if out, ok := candidate.Channel.Outbounds[candidate.APIFormat]; ok {
			return out
		}
	}

	return candidate.Channel.Outbound
}

// APIFormat returns the API format of the transformer.
func (p *PersistentOutboundTransformer) APIFormat() llm.APIFormat {
	return p.wrapped.APIFormat()
}

func (p *PersistentOutboundTransformer) TransformError(ctx context.Context, rawErr *httpclient.Error) *llm.ResponseError {
	return p.wrapped.TransformError(ctx, rawErr)
}

func (p *PersistentOutboundTransformer) TransformRequest(ctx context.Context, llmRequest *llm.Request) (*httpclient.Request, error) {
	// Candidates should already be selected by inbound transformer
	if len(p.state.ChannelModelsCandidates) == 0 {
		return nil, errors.New("no candidates available: candidates should be selected by inbound transformer")
	}

	// Select current candidate for this attempt
	if p.state.CurrentCandidateIndex >= len(p.state.ChannelModelsCandidates) {
		return nil, fmt.Errorf("%w: all candidates exhausted", biz.ErrInternal)
	}

	candidate := p.state.ChannelModelsCandidates[p.state.CurrentCandidateIndex]
	entry := candidate.Models[p.state.CurrentModelIndex]

	p.state.CurrentCandidate = candidate
	p.state.StreamCompleted = false
	p.state.CurrentCredentialID = 0
	p.state.CurrentCredentialFingerprint = ""
	p.state.CurrentSecretFingerprint = ""
	p.state.CurrentResourceScopeKey = ""
	p.state.CurrentCredentialName = ""
	p.state.CurrentCredentialKeyHint = ""
	p.state.CurrentCredentialSource = ""
	p.state.CurrentCredentialQuotaStatus = ""
	p.state.CurrentQuotaScopeID = 0
	p.state.CurrentQuotaScopeName = ""
	p.state.CurrentQuotaScopeStatus = ""
	p.state.CurrentCredentialAPIKey = ""

	p.wrapped = selectOutboundForCandidate(candidate)

	log.Debug(ctx, "using candidate",
		log.String("channel", candidate.Channel.Name),
		log.String("request_model", p.state.OriginalModel),
		log.String("actual_model", entry.ActualModel),
		log.String("api_format", candidate.APIFormat),
	)

	llmRequest.Model = entry.ActualModel

	// Apply channel transform options to create a new request
	llmRequest = applyTransformOptions(llmRequest, candidate.Channel.Settings)
	llmRequest = filterResponseCustomToolMessagesForNonResponsesOutbound(llmRequest, p.wrapped.APIFormat())

	if shouldForceStreamingForCandidate(candidate, llmRequest) {
		streamPtr := lo.ToPtr(true)
		llmRequest.Stream = streamPtr
		if llmRequest.StreamOptions == nil {
			llmRequest.StreamOptions = &llm.StreamOptions{}
		}
		llmRequest.StreamOptions.IncludeUsage = true
		if p.state != nil && p.state.LlmRequest != nil {
			p.state.LlmRequest.Stream = streamPtr
			if p.state.LlmRequest.StreamOptions == nil {
				p.state.LlmRequest.StreamOptions = &llm.StreamOptions{}
			}
			p.state.LlmRequest.StreamOptions.IncludeUsage = true
		}
	}

	// Ensure the mutable request context container exists before API key
	// providers store the selected credential metadata in it.
	ctx = contexts.WithCredentialSelectionSeed(ctx, "")
	ctx = contexts.WithPreferredCredential(ctx, 0, "")
	ctx = contexts.WithAllowedCredentials(ctx, nil, nil)
	ctx = contexts.WithExcludedCredentials(ctx, p.state.ExcludedCredentialIDs, p.state.ExcludedCredentialFingerprints)
	contexts.WithChannelCredential(ctx, 0, "", "")
	contexts.WithChannelCredentialIdentity(ctx, "", "")
	contexts.WithChannelCredentialMetadata(ctx, "", "", "", "")
	contexts.WithChannelCredentialQuotaScope(ctx, 0, "", "")
	if p.state.StickyKeyOK && p.state.StickyKey != "" {
		ctx = contexts.WithCredentialSelectionSeed(ctx, p.state.StickyKey)
	}
	ctx = contextWithAllowedCandidateCredentials(ctx, candidate)
	if p.state.PreferredCredentialID > 0 {
		ctx = contexts.WithPreferredCredential(ctx, p.state.PreferredCredentialID, p.state.PreferredCredentialFingerprint)
	} else if p.state.PreferredCredentialFingerprint != "" {
		ctx = contexts.WithPreferredCredentialFingerprint(ctx, p.state.PreferredCredentialFingerprint)
	} else if preferred := singleCandidateCredentialView(candidate); preferred != nil {
		if preferred.CredentialID > 0 {
			ctx = contexts.WithPreferredCredential(ctx, preferred.CredentialID, preferred.Fingerprint)
		} else if preferred.Fingerprint != "" {
			ctx = contexts.WithPreferredCredentialFingerprint(ctx, preferred.Fingerprint)
		}
	}

	rawRequest, err := p.wrapped.TransformRequest(ctx, llmRequest)
	if err != nil {
		return nil, err
	}

	if apiKey, ok := contexts.GetChannelAPIKey(ctx); ok {
		p.state.CurrentCredentialAPIKey = apiKey
	}
	if credentialID, ok := contexts.GetChannelCredentialID(ctx); ok {
		p.state.CurrentCredentialID = credentialID
	}
	if fingerprint, ok := contexts.GetChannelCredentialFingerprint(ctx); ok {
		p.state.CurrentCredentialFingerprint = fingerprint
	}
	if secretFingerprint, ok := contexts.GetChannelCredentialSecretFingerprint(ctx); ok {
		p.state.CurrentSecretFingerprint = secretFingerprint
	}
	if resourceScopeKey, ok := contexts.GetChannelCredentialResourceScopeKey(ctx); ok {
		p.state.CurrentResourceScopeKey = resourceScopeKey
	}
	if name, ok := contexts.GetChannelCredentialName(ctx); ok {
		p.state.CurrentCredentialName = name
	}
	if keyHint, ok := contexts.GetChannelCredentialKeyHint(ctx); ok {
		p.state.CurrentCredentialKeyHint = keyHint
	}
	if source, ok := contexts.GetChannelCredentialSource(ctx); ok {
		p.state.CurrentCredentialSource = source
	}
	if quotaStatus, ok := contexts.GetChannelCredentialQuotaStatus(ctx); ok {
		p.state.CurrentCredentialQuotaStatus = quotaStatus
	}
	if quotaScopeID, ok := contexts.GetChannelCredentialQuotaScopeID(ctx); ok {
		p.state.CurrentQuotaScopeID = quotaScopeID
	}
	if quotaScopeName, ok := contexts.GetChannelCredentialQuotaScopeName(ctx); ok {
		p.state.CurrentQuotaScopeName = quotaScopeName
	}
	if quotaScopeStatus, ok := contexts.GetChannelCredentialQuotaScopeStatus(ctx); ok {
		p.state.CurrentQuotaScopeStatus = quotaScopeStatus
	}
	if p.state.CurrentCredentialFingerprint == "" ||
		p.state.CurrentCredentialID == 0 ||
		p.state.CurrentSecretFingerprint == "" ||
		p.state.CurrentResourceScopeKey == "" ||
		p.state.CurrentQuotaScopeID == 0 ||
		p.state.CurrentQuotaScopeName == "" ||
		p.state.CurrentQuotaScopeStatus == "" {
		var only *biz.ChannelCredentialView
		for _, view := range candidate.Channel.CredentialViews() {
			if !view.Enabled {
				continue
			}
			if only != nil {
				only = nil
				break
			}
			v := view
			only = &v
		}
		if only != nil {
			if p.state.CurrentCredentialID == 0 {
				p.state.CurrentCredentialID = only.CredentialID
			}
			if p.state.CurrentCredentialFingerprint == "" {
				p.state.CurrentCredentialFingerprint = only.Fingerprint
			}
			if p.state.CurrentSecretFingerprint == "" {
				p.state.CurrentSecretFingerprint = only.SecretFingerprint
			}
			if p.state.CurrentResourceScopeKey == "" {
				p.state.CurrentResourceScopeKey = only.ResourceScopeKey
			}
			if p.state.CurrentCredentialName == "" {
				p.state.CurrentCredentialName = only.Name
			}
			if p.state.CurrentCredentialKeyHint == "" {
				p.state.CurrentCredentialKeyHint = only.KeyHint
			}
			if p.state.CurrentCredentialSource == "" {
				p.state.CurrentCredentialSource = only.Source
			}
			if p.state.CurrentCredentialQuotaStatus == "" {
				p.state.CurrentCredentialQuotaStatus = only.QuotaStatus
			}
			if p.state.CurrentQuotaScopeID == 0 {
				p.state.CurrentQuotaScopeID = only.QuotaScopeID
			}
			if p.state.CurrentQuotaScopeName == "" {
				p.state.CurrentQuotaScopeName = only.QuotaScopeName
			}
			if p.state.CurrentQuotaScopeStatus == "" {
				p.state.CurrentQuotaScopeStatus = only.QuotaScopeStatus
			}
		}
	}

	return rawRequest, nil
}

func contextWithAllowedCandidateCredentials(ctx context.Context, candidate *ChannelModelsCandidate) context.Context {
	if candidate == nil || candidate.Channel == nil {
		return ctx
	}

	views := candidate.Channel.CredentialViews()
	if len(views) == 0 {
		return ctx
	}

	credentialIDs := make([]int, 0, len(views))
	fingerprints := make([]string, 0, len(views))
	for _, view := range views {
		if !view.Enabled {
			continue
		}
		if view.CredentialID > 0 {
			credentialIDs = append(credentialIDs, view.CredentialID)
		}
		if view.Fingerprint != "" {
			fingerprints = append(fingerprints, view.Fingerprint)
		}
	}
	if len(credentialIDs) == 0 && len(fingerprints) == 0 {
		return ctx
	}

	return contexts.WithAllowedCredentials(ctx, credentialIDs, fingerprints)
}

func singleCandidateCredentialView(candidate *ChannelModelsCandidate) *biz.ChannelCredentialView {
	if candidate == nil || candidate.Channel == nil {
		return nil
	}

	var only *biz.ChannelCredentialView
	for _, view := range candidate.Channel.CredentialViews() {
		if !view.Enabled {
			continue
		}
		if only != nil {
			return nil
		}
		v := view
		only = &v
	}

	return only
}

func filterResponseCustomToolMessagesForNonResponsesOutbound(
	llmRequest *llm.Request,
	outboundFormat llm.APIFormat,
) *llm.Request {
	if llmRequest == nil {
		return nil
	}

	if !isResponsesFormat(llmRequest.APIFormat) || isResponsesFormat(outboundFormat) || !containsResponseCustomToolMessages(llmRequest.Messages) {
		return llmRequest
	}

	cloned := *llmRequest
	cloned.Messages = shared.FilterOutResponseCustomToolMessages(llmRequest.Messages)

	return &cloned
}

func isResponsesFormat(format llm.APIFormat) bool {
	return format == llm.APIFormatOpenAIResponse || format == llm.APIFormatOpenAIResponseCompact
}

func containsResponseCustomToolMessages(messages []llm.Message) bool {
	for _, msg := range messages {
		for _, toolCall := range msg.ToolCalls {
			if toolCall.Type == llm.ToolTypeResponsesCustomTool || toolCall.ResponseCustomToolCall != nil {
				return true
			}
		}
	}

	return false
}

func (p *PersistentOutboundTransformer) TransformResponse(ctx context.Context, response *httpclient.Response) (*llm.Response, error) {
	return p.wrapped.TransformResponse(ctx, response)
}

func (p *PersistentOutboundTransformer) TransformStream(ctx context.Context, req *httpclient.Request, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	persistentStream := NewOutboundPersistentStream(
		ctx,
		stream,
		p.state.Request,
		p.state.RequestExec,
		p.state.RequestService,
		p.state.UsageLogService,
		p.wrapped, // Pass the wrapped outbound transformer for chunk aggregation
		p.state.Perf,
		p.state,
	)

	return p.wrapped.TransformStream(ctx, req, persistentStream)
}

func (p *PersistentOutboundTransformer) AggregateStreamChunks(
	ctx context.Context, req *httpclient.Request,
	chunks []*httpclient.StreamEvent,
) ([]byte, llm.ResponseMeta, error) {
	return p.wrapped.AggregateStreamChunks(ctx, req, chunks)
}

// GetRequestExecution returns the current request execution.
func (p *PersistentOutboundTransformer) GetRequestExecution() *ent.RequestExecution {
	return p.state.RequestExec
}

// GetRequest returns the current request.
func (p *PersistentOutboundTransformer) GetRequest() *ent.Request {
	return p.state.Request
}

// GetCurrentChannel returns the current channel.
func (p *PersistentOutboundTransformer) GetCurrentChannel() *biz.Channel {
	if p.state == nil || p.state.CurrentCandidate == nil {
		return nil
	}

	return p.state.CurrentCandidate.Channel
}

// GetCurrentModelID returns the current model ID for logging purposes.
func (p *PersistentOutboundTransformer) GetCurrentModelID() string {
	if p.state.CurrentCandidate == nil || len(p.state.CurrentCandidate.Models) == 0 {
		return ""
	}

	return p.state.CurrentCandidate.Models[p.state.CurrentModelIndex].ActualModel
}

// GetRequestedModel returns the originally requested model ID.
func (p *PersistentOutboundTransformer) GetRequestedModel() string {
	return p.state.OriginalModel
}

// HasMoreChannels returns true if there are more candidates available for retry.
// It implements the pipeline.Retryable interface.
func (p *PersistentOutboundTransformer) HasMoreChannels() bool {
	next := p.nextCandidateIndexForFallback(0, "")

	return next >= 0
}

// resetPassThroughStreamState cancels the current attempt's fan-out goroutine (if any)
// and clears pass-through stream state so the next attempt starts with a clean slate.
// Must be called before every retry to prevent goroutine leaks and data races on
// state.RawStreamErrRef.
func (p *PersistentOutboundTransformer) resetPassThroughStreamState() {
	if p.state.RawStreamCancel != nil {
		p.state.RawStreamCancel()
		p.state.RawStreamCancel = nil
	}

	p.state.RawStreamCh = nil
	p.state.RawStreamErrRef = nil
}

// NextChannel moves to the next available candidate for retry.
// It implements the pipeline.Retryable interface.
func (p *PersistentOutboundTransformer) NextChannel(ctx context.Context) error {
	// Cancel any in-flight pass-through stream goroutine from the previous attempt
	// so it exits promptly and releases its upstream HTTP connection.
	p.resetPassThroughStreamState()

	nextIndex := p.nextCandidateIndexForFallback(0, "")
	if nextIndex < 0 {
		return errors.New("no more candidates available for retry")
	}

	p.state.CurrentModelIndex = 0
	p.state.CurrentCandidateIndex = nextIndex

	// Reset request execution for the new candidate
	p.state.RequestExec = nil
	p.state.PassThroughApplied = false

	candidate := p.state.ChannelModelsCandidates[p.state.CurrentCandidateIndex]
	p.state.CurrentCandidate = candidate
	p.wrapped = selectOutboundForCandidate(candidate)

	if log.DebugEnabled(ctx) {
		model := candidate.Models[0].ActualModel
		log.Debug(ctx, "switching to next channel for retry",
			log.String("channel", candidate.Channel.Name),
			log.String("model", model),
			log.Int("index", p.state.CurrentCandidateIndex),
			log.String("api_format", candidate.APIFormat),
		)
	}

	return nil
}

// CanFallback reports whether the failed attempt can switch to another
// execution target. This is broader than a channel switch: credential-scoped
// failures first try another credential on the same channel before moving to
// the next channel candidate.
func (p *PersistentOutboundTransformer) CanFallback(err error) bool {
	if p.state == nil || p.state.CurrentCandidate == nil {
		return false
	}

	if !isFallbackableError(err) {
		return false
	}

	failedID, failedFingerprint := p.currentCredentialForExclusion(err)
	if isCredentialScopedFallbackError(err) &&
		(failedID > 0 || failedFingerprint != "") &&
		p.hasSameChannelCredentialFallback(failedID, failedFingerprint) {
		return true
	}

	return p.nextCandidateIndexForFallback(failedID, failedFingerprint) >= 0
}

// PrepareForFallback switches to the next execution target after an attempt
// fails. It preserves candidate ordering, but lets credential-scoped failures
// escape locally to another credential in the same channel first.
func (p *PersistentOutboundTransformer) PrepareForFallback(ctx context.Context, err error) error {
	if p.state == nil || p.state.CurrentCandidate == nil {
		return errors.New("no current candidate available for fallback")
	}

	p.resetPassThroughStreamState()
	failedID, failedFingerprint := p.currentCredentialForExclusion(err)
	if isCredentialScopedFallbackError(err) {
		p.excludeFailedCredential(failedID, failedFingerprint)
	}

	if isCredentialScopedFallbackError(err) &&
		(failedID > 0 || failedFingerprint != "") &&
		p.hasSameChannelCredentialFallback(0, "") {
		p.state.RequestExec = nil
		p.state.PassThroughApplied = false
		p.state.FallbackTargetSwitches++
		p.wrapped = selectOutboundForCandidate(p.state.CurrentCandidate)

		if log.DebugEnabled(ctx) {
			candidate := p.state.CurrentCandidate
			model := ""
			if p.state.CurrentModelIndex < len(candidate.Models) {
				model = candidate.Models[p.state.CurrentModelIndex].ActualModel
			}
			log.Debug(ctx, "switching to same-channel credential fallback",
				log.String("channel", candidate.Channel.Name),
				log.String("model", model),
				log.Int("channel_id", candidate.Channel.ID),
				log.Int("current_candidate_index", p.state.CurrentCandidateIndex),
				log.Int("current_model_index", p.state.CurrentModelIndex),
			)
		}

		return nil
	}

	nextIndex := p.nextCandidateIndexForFallback(0, "")
	if nextIndex < 0 {
		return errors.New("no more candidates available for retry")
	}

	p.state.CurrentCandidateIndex = nextIndex
	p.state.CurrentModelIndex = 0
	p.state.RequestExec = nil
	p.state.PassThroughApplied = false
	p.state.FallbackTargetSwitches++

	candidate := p.state.ChannelModelsCandidates[p.state.CurrentCandidateIndex]
	p.state.CurrentCandidate = candidate
	p.wrapped = selectOutboundForCandidate(candidate)

	if log.DebugEnabled(ctx) {
		model := ""
		if len(candidate.Models) > 0 {
			model = candidate.Models[0].ActualModel
		}
		log.Debug(ctx, "switching to next fallback channel",
			log.String("channel", candidate.Channel.Name),
			log.String("model", model),
			log.Int("index", p.state.CurrentCandidateIndex),
			log.Int("route_tier", candidate.Priority),
			log.String("api_format", candidate.APIFormat),
		)
	}

	return nil
}

// CanRetry returns true if the current channel can be retried.
// It implements the pipeline.ChannelRetryable interface, it just check the error is retryable, the
// pipeline will ensure the maxSameChannelRetries is not exceeded.
func (p *PersistentOutboundTransformer) CanRetry(err error) bool {
	if p.state.CurrentCandidate == nil {
		return false
	}

	if err == nil {
		return false
	}

	if errors.Is(err, errSkipCandidateByCircuitBreaker) {
		return false
	}

	// Local queue rejection: same channel is full or timed out — bounce immediately
	// to the next channel rather than retrying.
	if isChannelQueueError(err) {
		return false
	}

	if IsResponseQualityGuardMatchedError(err) {
		log.Warn(context.Background(), "response quality guard requested same-target retry",
			log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
			log.String("actual_model", p.GetCurrentModelID()),
		)

		return true
	}

	// Empty response detection: allow same-target retry so the pipeline can
	// re-execute the same concrete target.
	if errors.Is(err, pipeline.ErrEmptyResponse) ||
		errors.Is(err, pipeline.ErrEmptyStreamChunks) ||
		errors.Is(err, pipeline.ErrEmptyAggregatedBody) {
		log.Debug(context.Background(), "empty response detected",
			log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
		)

		return true
	}

	if isCredentialScopedFallbackError(err) {
		if httpclient.HasRetryAfterHeader(err) {
			log.Debug(context.Background(), "credential-scoped error with Retry-After, skipping same-target retry",
				log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
			)
		} else {
			log.Debug(context.Background(), "credential-scoped error, skipping same-target retry",
				log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
			)
		}

		return false
	}

	if !isFallbackableError(err) {
		return false
	}

	statusCode := ExtractStatusCodeFromError(err)
	if statusCode >= 400 && statusCode < 500 {
		log.Debug(context.Background(), "non-transient client error, skipping same-target retry",
			log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
		)

		return false
	}

	if p.nextCandidateIndexForFallback(0, "") >= 0 {
		log.Debug(context.Background(), "fallback target available, skipping same-target retry",
			log.Int("channel_id", p.state.CurrentCandidate.Channel.ID),
		)

		return false
	}

	// otherwise check if the error is retryable.
	return isRetryableError(err)
}

// PrepareForRetry implements the pipeline.ChannelRetryable interface.
// This resets the request execution so the same concrete target can be retried.
func (p *PersistentOutboundTransformer) PrepareForRetry(ctx context.Context) error {
	candidate := p.state.CurrentCandidate

	// Reset request execution for the same channel.
	p.state.RequestExec = nil
	p.state.PassThroughApplied = false

	// Cancel any in-flight pass-through stream goroutine from the previous attempt
	// so it exits promptly and releases its upstream HTTP connection.
	p.resetPassThroughStreamState()

	p.wrapped = selectOutboundForCandidate(candidate)

	if log.DebugEnabled(ctx) {
		model := candidate.Models[p.state.CurrentModelIndex].ActualModel
		log.Debug(ctx, "prepared same-target retry",
			log.Any("channel", candidate.Channel.Name),
			log.Any("model", model),
			log.String("api_format", candidate.APIFormat),
			log.Int("current_candidate_index", p.state.CurrentCandidateIndex),
			log.Int("current_entry_index", p.state.CurrentModelIndex),
		)
	}

	return nil
}

func (p *PersistentOutboundTransformer) currentCredentialForExclusion(err error) (int, string) {
	if !isCredentialScopedFallbackError(err) || p.state == nil {
		return 0, ""
	}

	return p.state.CurrentCredentialID, p.state.CurrentCredentialFingerprint
}

func (p *PersistentOutboundTransformer) excludeFailedCredential(credentialID int, fingerprint string) {
	if p.state == nil {
		return
	}

	if credentialID > 0 && !slices.Contains(p.state.ExcludedCredentialIDs, credentialID) {
		p.state.ExcludedCredentialIDs = append(p.state.ExcludedCredentialIDs, credentialID)
	}
	if fingerprint != "" && !slices.Contains(p.state.ExcludedCredentialFingerprints, fingerprint) {
		p.state.ExcludedCredentialFingerprints = append(p.state.ExcludedCredentialFingerprints, fingerprint)
	}
}

func (p *PersistentOutboundTransformer) hasSameChannelCredentialFallback(extraExcludedID int, extraExcludedFingerprint string) bool {
	if p.state == nil || p.state.CurrentCandidate == nil {
		return false
	}

	candidate := p.state.CurrentCandidate
	if candidate.Channel == nil {
		return false
	}

	views := candidate.Channel.CredentialViews()
	if len(views) == 0 {
		return false
	}

	for _, view := range views {
		if !sameChannelFallbackCredentialViewSelectable(view) {
			continue
		}
		if credentialViewExcludedByState(p.state, view, extraExcludedID, extraExcludedFingerprint) {
			continue
		}

		return true
	}

	return false
}

func (p *PersistentOutboundTransformer) nextCandidateIndexForFallback(extraExcludedID int, extraExcludedFingerprint string) int {
	if p.state == nil {
		return -1
	}

	currentCandidate := p.state.CurrentCandidate
	if currentCandidate == nil &&
		p.state.CurrentCandidateIndex >= 0 &&
		p.state.CurrentCandidateIndex < len(p.state.ChannelModelsCandidates) {
		currentCandidate = p.state.ChannelModelsCandidates[p.state.CurrentCandidateIndex]
	}
	if currentCandidate == nil {
		return -1
	}

	currentPriority := currentCandidate.Priority
	for i := p.state.CurrentCandidateIndex + 1; i < len(p.state.ChannelModelsCandidates); i++ {
		candidate := p.state.ChannelModelsCandidates[i]
		if candidate == nil || candidate.Priority != currentPriority {
			continue
		}
		if candidateExecutableAfterCredentialExclusions(p.state, candidate, extraExcludedID, extraExcludedFingerprint) {
			return i
		}
	}

	for i := p.state.CurrentCandidateIndex + 1; i < len(p.state.ChannelModelsCandidates); i++ {
		candidate := p.state.ChannelModelsCandidates[i]
		if candidate == nil || candidate.Priority <= currentPriority {
			continue
		}
		if candidateExecutableAfterCredentialExclusions(p.state, candidate, extraExcludedID, extraExcludedFingerprint) {
			return i
		}
	}

	return -1
}

func candidateExecutableAfterCredentialExclusions(
	state *PersistenceState,
	candidate *ChannelModelsCandidate,
	extraExcludedID int,
	extraExcludedFingerprint string,
) bool {
	if candidate == nil || candidate.Channel == nil {
		return false
	}

	views := candidate.Channel.CredentialViews()
	if len(views) == 0 {
		return true
	}

	for _, view := range views {
		if !view.Enabled {
			continue
		}
		if credentialViewExcludedByState(state, view, extraExcludedID, extraExcludedFingerprint) {
			continue
		}

		return true
	}

	return false
}

func sameChannelFallbackCredentialViewSelectable(view biz.ChannelCredentialView) bool {
	if !view.Enabled || strings.TrimSpace(view.Secret.APIKey) == "" {
		return false
	}

	authKind := strings.ToLower(strings.TrimSpace(view.AuthKind))
	secretKind := strings.ToLower(strings.TrimSpace(view.SecretKind))

	return authKind == "api_key" || secretKind == "api_key"
}

func credentialViewExcludedByState(
	state *PersistenceState,
	view biz.ChannelCredentialView,
	extraExcludedID int,
	extraExcludedFingerprint string,
) bool {
	if state == nil {
		return false
	}

	if view.CredentialID > 0 {
		if view.CredentialID == extraExcludedID || slices.Contains(state.ExcludedCredentialIDs, view.CredentialID) {
			return true
		}
	}
	if view.Fingerprint != "" {
		if view.Fingerprint == extraExcludedFingerprint || slices.Contains(state.ExcludedCredentialFingerprints, view.Fingerprint) {
			return true
		}
	}

	return false
}

// CustomizeExecutor customizes the executor for the current channel.
// If the current channel has an executor, it will be used.
// Otherwise, the default executor will be used.
//
// The customized executor will be used to execute the request.
// e.g. the aws bedrock process need a custom executor to handle the request.
// It implements the pipeline.ChannelCustomizedExecutor interface.
func (p *PersistentOutboundTransformer) CustomizeExecutor(executor pipeline.Executor) pipeline.Executor {
	// Start with the default executor, then layer customizations.
	customizedExecutor := executor

	channel := p.GetCurrentChannel()
	if channel == nil {
		return customizedExecutor
	}

	// 1. Apply proxy settings. Test proxy override takes precedence over channel settings.
	if p.state.Proxy != nil {
		if channel.HTTPClient != nil {
			customizedExecutor = channel.HTTPClient.WithProxy(p.state.Proxy)
		} else {
			customizedExecutor = httpclient.NewHttpClientWithProxy(p.state.Proxy)
		}
	} else if channel.HTTPClient != nil {
		// Use the channel's own HTTP client, which is pre-configured with its proxy settings.
		customizedExecutor = channel.HTTPClient
	}
	// 2. Allow the selected outbound transformer (e.g., for AWS signing or Responses WebSocket) to further customize the client.
	outbound := p.wrapped
	if outbound == nil {
		outbound = channel.Outbound
	}
	if custom, ok := outbound.(pipeline.ChannelCustomizedExecutor); ok {
		return custom.CustomizeExecutor(customizedExecutor)
	}

	return customizedExecutor
}
