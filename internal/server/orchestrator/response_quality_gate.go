package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/pkg/xregexp"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
)

const (
	responseQualityGuardModeObserveOnly  = "observe_only"
	responseQualityGuardModeRetryOnMatch = "retry_on_match"
)

type ResponseQualityGuardMatchedError struct {
	RequestedModel  string
	ActualModel     string
	Stream          bool
	ReasoningTokens int64
	Threshold       int64
	GuardMode       string
}

func (e *ResponseQualityGuardMatchedError) Error() string {
	if e == nil {
		return "response quality guard matched"
	}

	return fmt.Sprintf(
		"response quality guard matched: actual_model=%s requested_model=%s stream=%t reasoning_tokens=%d threshold=%d mode=%s",
		e.ActualModel,
		e.RequestedModel,
		e.Stream,
		e.ReasoningTokens,
		e.Threshold,
		e.GuardMode,
	)
}

func IsResponseQualityGuardMatchedError(err error) bool {
	var target *ResponseQualityGuardMatchedError

	return err != nil && errors.As(err, &target)
}

func withResponseQualityGate(
	outbound *PersistentOutboundTransformer,
	state *PersistenceState,
	policy *biz.RetryPolicy,
) pipeline.Middleware {
	if outbound == nil || state == nil || policy == nil || !policy.ResponseQualityGuard.Enabled {
		return &noopResponseQualityGate{}
	}

	return &responseQualityGate{
		outbound: outbound,
		state:    state,
		config:   policy.ResponseQualityGuard,
	}
}

type responseQualityGate struct {
	pipeline.DummyMiddleware

	outbound *PersistentOutboundTransformer
	state    *PersistenceState
	config   biz.ResponseQualityGuard
}

func (m *responseQualityGate) Name() string {
	return "response-quality-gate"
}

func (m *responseQualityGate) OnOutboundRawResponse(ctx context.Context, response *httpclient.Response) (*httpclient.Response, error) {
	if response == nil {
		return response, nil
	}

	rule, matched := m.matchRule(m.clientRequestedStream())
	if !matched {
		return response, nil
	}

	unified, err := m.outbound.TransformResponse(ctx, response)
	if err != nil {
		return nil, err
	}

	reasoningTokens, ok := responseReasoningTokens(unified)
	if !ok || reasoningTokens > rule.ReasoningTokensLTE {
		return response, nil
	}

	return m.failOrObserve(ctx, false, reasoningTokens, rule, func() *httpclient.Response { return response })
}

func (m *responseQualityGate) OnOutboundRawStream(ctx context.Context, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*httpclient.StreamEvent], error) {
	rule, matched := m.matchRule(m.clientRequestedStream())
	if !matched || !rule.BufferStreamUntilDecision {
		return stream, nil
	}

	buffered := make([]*httpclient.StreamEvent, 0, 32)
	for stream.Next() {
		event := stream.Current()
		if event != nil {
			buffered = append(buffered, event)
		}
	}

	err := stream.Err()
	closeErr := stream.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}

	reasoningTokens, ok, err := m.inspectBufferedRawStream(ctx, buffered)
	if err != nil {
		return nil, err
	}
	if !ok || reasoningTokens > rule.ReasoningTokensLTE {
		return streams.SliceStream(buffered), nil
	}

	return m.failOrObserveStream(ctx, reasoningTokens, rule, buffered)
}

func (m *responseQualityGate) inspectBufferedRawStream(ctx context.Context, buffered []*httpclient.StreamEvent) (int64, bool, error) {
	request := m.state.RawProviderRequest
	if request == nil {
		return 0, false, nil
	}

	body, _, err := m.outbound.AggregateStreamChunks(ctx, request, buffered)
	if err != nil {
		return 0, false, err
	}

	resp, err := m.outbound.TransformResponse(ctx, &httpclient.Response{
		StatusCode: 200,
		Headers:    httpclientResponseHeadersJSON(),
		Body:       body,
		Request:    request,
	})
	if err != nil {
		return 0, false, err
	}

	tokens, ok := responseReasoningTokens(resp)
	return tokens, ok, nil
}

func (m *responseQualityGate) failOrObserve(
	ctx context.Context,
	stream bool,
	reasoningTokens int64,
	rule biz.ResponseQualityGuardRule,
	onObserve func() *httpclient.Response,
) (*httpclient.Response, error) {
	if m.state != nil && m.state.RequestExec != nil {
		m.state.RequestExec.ResponseQualityGuardMatched = true
	}

	actualModel := m.outbound.GetCurrentModelID()
	requestedModel := m.state.OriginalModel
	fields := []log.Field{
		log.String("requested_model", requestedModel),
		log.String("actual_model", actualModel),
		log.Bool("stream", stream),
		log.Int64("reasoning_tokens", reasoningTokens),
		log.Int64("reasoning_tokens_lte", rule.ReasoningTokensLTE),
		log.String("mode", m.config.Mode),
	}

	if m.config.Mode == responseQualityGuardModeRetryOnMatch {
		log.Warn(ctx, "response quality guard matched and requested retry", fields...)
		return nil, &ResponseQualityGuardMatchedError{
			RequestedModel:  requestedModel,
			ActualModel:     actualModel,
			Stream:          stream,
			ReasoningTokens: reasoningTokens,
			Threshold:       rule.ReasoningTokensLTE,
			GuardMode:       m.config.Mode,
		}
	}

	log.Info(ctx, "response quality guard matched", fields...)
	return onObserve(), nil
}

func (m *responseQualityGate) failOrObserveStream(
	ctx context.Context,
	reasoningTokens int64,
	rule biz.ResponseQualityGuardRule,
	buffered []*httpclient.StreamEvent,
) (streams.Stream[*httpclient.StreamEvent], error) {
	if m.state != nil && m.state.RequestExec != nil {
		m.state.RequestExec.ResponseQualityGuardMatched = true
	}

	actualModel := m.outbound.GetCurrentModelID()
	requestedModel := m.state.OriginalModel
	fields := []log.Field{
		log.String("requested_model", requestedModel),
		log.String("actual_model", actualModel),
		log.Bool("stream", true),
		log.Int64("reasoning_tokens", reasoningTokens),
		log.Int64("reasoning_tokens_lte", rule.ReasoningTokensLTE),
		log.String("mode", m.config.Mode),
	}

	if m.config.Mode == responseQualityGuardModeRetryOnMatch {
		log.Warn(ctx, "response quality guard matched and requested retry", fields...)
		return nil, &ResponseQualityGuardMatchedError{
			RequestedModel:  requestedModel,
			ActualModel:     actualModel,
			Stream:          true,
			ReasoningTokens: reasoningTokens,
			Threshold:       rule.ReasoningTokensLTE,
			GuardMode:       m.config.Mode,
		}
	}

	log.Info(ctx, "response quality guard matched", fields...)
	return streams.SliceStream(buffered), nil
}

func (m *responseQualityGate) matchRule(stream bool) (biz.ResponseQualityGuardRule, bool) {
	actualModel := m.outbound.GetCurrentModelID()
	if actualModel == "" {
		return biz.ResponseQualityGuardRule{}, false
	}

	for _, rule := range m.config.Rules {
		if stream && !rule.ApplyToStream {
			continue
		}
		if !stream && !rule.ApplyToNonStream {
			continue
		}
		if len(rule.ModelMatch) == 0 {
			continue
		}
		if lo.SomeBy(rule.ModelMatch, func(pattern string) bool {
			return xregexp.MatchString(pattern, actualModel)
		}) {
			return rule, true
		}
	}

	return biz.ResponseQualityGuardRule{}, false
}

func (m *responseQualityGate) clientRequestedStream() bool {
	if m == nil || m.state == nil {
		return false
	}

	if m.state.OriginalRequestStream != nil {
		return *m.state.OriginalRequestStream
	}

	if m.state.LlmRequest != nil && m.state.LlmRequest.Stream != nil {
		return *m.state.LlmRequest.Stream
	}

	return false
}

func responseReasoningTokens(response *llm.Response) (int64, bool) {
	if response == nil || response.Usage == nil || response.Usage.CompletionTokensDetails == nil {
		return 0, false
	}

	return response.Usage.CompletionTokensDetails.ReasoningTokens, true
}

func httpclientResponseHeadersJSON() http.Header {
	return http.Header{
		"Content-Type": {"application/json"},
	}
}

type noopResponseQualityGate struct {
	pipeline.DummyMiddleware
}

func (m *noopResponseQualityGate) Name() string {
	return "response-quality-gate-noop"
}
