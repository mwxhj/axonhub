package orchestrator

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func newResponseQualityGateOutbound(actualModel string) *PersistentOutboundTransformer {
	return &PersistentOutboundTransformer{
		wrapped: openAIResponseQualityMockTransformer{},
		state: &PersistenceState{
			OriginalModel: "gpt-5",
			CurrentCandidate: &ChannelModelsCandidate{
				Channel: &biz.Channel{
					Channel: &ent.Channel{ID: 1, Name: "test-channel"},
				},
				Models: []biz.ChannelModelEntry{
					{RequestModel: "gpt-5", ActualModel: actualModel},
				},
			},
			CurrentModelIndex: 0,
			RawProviderRequest: &httpclient.Request{
				APIFormat: llm.APIFormatOpenAIChatCompletion.String(),
			},
		},
	}
}

type openAIResponseQualityMockTransformer struct{}

func (openAIResponseQualityMockTransformer) APIFormat() llm.APIFormat {
	return llm.APIFormatOpenAIChatCompletion
}
func (openAIResponseQualityMockTransformer) TransformRequest(ctx context.Context, request *llm.Request) (*httpclient.Request, error) {
	return &httpclient.Request{}, nil
}
func (openAIResponseQualityMockTransformer) TransformError(ctx context.Context, err *httpclient.Error) *llm.ResponseError {
	return nil
}
func (openAIResponseQualityMockTransformer) TransformStream(ctx context.Context, req *httpclient.Request, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	return nil, nil
}
func (openAIResponseQualityMockTransformer) AggregateStreamChunks(ctx context.Context, req *httpclient.Request, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return []byte(`{"id":"agg","usage":{"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":100},"prompt_tokens":1,"total_tokens":11}}`), llm.ResponseMeta{}, nil
}
func (openAIResponseQualityMockTransformer) TransformResponse(ctx context.Context, response *httpclient.Response) (*llm.Response, error) {
	return &llm.Response{
		Usage: &llm.Usage{
			CompletionTokensDetails: &llm.CompletionTokensDetails{ReasoningTokens: 100},
		},
	}, nil
}

func TestResponseQualityGate_OnOutboundRawResponse_RetryBeforeSuccessSideEffects(t *testing.T) {
	outbound := newResponseQualityGateOutbound("gpt-5.4")
	gate := withResponseQualityGate(outbound, outbound.state, &biz.RetryPolicy{
		ResponseQualityGuard: biz.ResponseQualityGuard{
			Enabled: true,
			Mode:    responseQualityGuardModeRetryOnMatch,
			Rules: []biz.ResponseQualityGuardRule{
				{
					ModelMatch:         []string{"gpt-5.4"},
					ReasoningTokensLTE: 516,
					ApplyToNonStream:   true,
				},
			},
		},
	}).(*responseQualityGate)

	resp, err := gate.OnOutboundRawResponse(context.Background(), &httpclient.Response{
		StatusCode: http.StatusOK,
		Headers:    http.Header{"Content-Type": []string{"application/json"}},
		Body:       []byte(`{"ok":true}`),
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.True(t, IsResponseQualityGuardMatchedError(err))
}

func TestResponseQualityGate_OnOutboundRawStream_RetryAfterBufferedVerdict(t *testing.T) {
	outbound := newResponseQualityGateOutbound("gpt-5.4")
	streamRequested := true
	outbound.state.OriginalRequestStream = &streamRequested
	outbound.state.RawProviderRequest = &httpclient.Request{
		APIFormat: llm.APIFormatOpenAIChatCompletion.String(),
	}

	gate := withResponseQualityGate(outbound, outbound.state, &biz.RetryPolicy{
		ResponseQualityGuard: biz.ResponseQualityGuard{
			Enabled: true,
			Mode:    responseQualityGuardModeRetryOnMatch,
			Rules: []biz.ResponseQualityGuardRule{
				{
					ModelMatch:                []string{"gpt-5.4"},
					ReasoningTokensLTE:        516,
					ApplyToStream:             true,
					BufferStreamUntilDecision: true,
				},
			},
		},
	}).(*responseQualityGate)

	stream, err := gate.OnOutboundRawStream(context.Background(), streams.SliceStream([]*httpclient.StreamEvent{
		{Data: []byte(`{"id":"a","choices":[{"delta":{"content":"hi"}}]}`)},
		{Data: []byte(`{"id":"a","choices":[{"finish_reason":"stop"}],"usage":{"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":100},"prompt_tokens":1,"total_tokens":11}}`)},
	}))
	require.Error(t, err)
	assert.Nil(t, stream)
	assert.True(t, IsResponseQualityGuardMatchedError(err))
}

func TestResponseQualityGate_OnOutboundRawStream_ObserveOnlyReplaysBufferedChunks(t *testing.T) {
	outbound := newResponseQualityGateOutbound("gpt-5.4")
	streamRequested := true
	outbound.state.OriginalRequestStream = &streamRequested
	outbound.state.RawProviderRequest = &httpclient.Request{
		APIFormat: llm.APIFormatOpenAIChatCompletion.String(),
	}

	gate := withResponseQualityGate(outbound, outbound.state, &biz.RetryPolicy{
		ResponseQualityGuard: biz.ResponseQualityGuard{
			Enabled: true,
			Mode:    responseQualityGuardModeObserveOnly,
			Rules: []biz.ResponseQualityGuardRule{
				{
					ModelMatch:                []string{"gpt-5.4"},
					ReasoningTokensLTE:        516,
					ApplyToStream:             true,
					BufferStreamUntilDecision: true,
				},
			},
		},
	}).(*responseQualityGate)

	result, err := gate.OnOutboundRawStream(context.Background(), streams.SliceStream([]*httpclient.StreamEvent{
		{Data: []byte(`{"id":"a","choices":[{"delta":{"content":"hi"}}]}`)},
		{Data: []byte(`{"id":"a","choices":[{"finish_reason":"stop"}],"usage":{"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":100},"prompt_tokens":1,"total_tokens":11}}`)},
	}))
	require.NoError(t, err)

	var chunks []*httpclient.StreamEvent
	for result.Next() {
		chunks = append(chunks, result.Current())
	}

	require.NoError(t, result.Err())
	require.Len(t, chunks, 2)
}

type aggregateTrackingTransformer struct {
	openAIResponseQualityMockTransformer

	aggregateCalled bool
}

func (t *aggregateTrackingTransformer) AggregateStreamChunks(ctx context.Context, req *httpclient.Request, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	t.aggregateCalled = true

	return []byte(`{"id":"agg","usage":{"completion_tokens":10,"completion_tokens_details":{"reasoning_tokens":100},"prompt_tokens":1,"total_tokens":11}}`), llm.ResponseMeta{}, nil
}

func TestResponseQualityGate_OnOutboundRawStream_UsesOutboundAggregateContract(t *testing.T) {
	transformer := &aggregateTrackingTransformer{}
	outbound := newResponseQualityGateOutbound("gpt-5.4")
	outbound.wrapped = transformer
	streamRequested := true
	outbound.state.OriginalRequestStream = &streamRequested
	outbound.state.RawProviderRequest = &httpclient.Request{
		APIFormat: "custom/provider",
	}

	gate := withResponseQualityGate(outbound, outbound.state, &biz.RetryPolicy{
		ResponseQualityGuard: biz.ResponseQualityGuard{
			Enabled: true,
			Mode:    responseQualityGuardModeObserveOnly,
			Rules: []biz.ResponseQualityGuardRule{
				{
					ModelMatch:                []string{"gpt-5.4"},
					ReasoningTokensLTE:        516,
					ApplyToStream:             true,
					BufferStreamUntilDecision: true,
				},
			},
		},
	}).(*responseQualityGate)

	result, err := gate.OnOutboundRawStream(context.Background(), streams.SliceStream([]*httpclient.StreamEvent{
		{Data: []byte(`{"chunk":1}`)},
	}))
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, transformer.aggregateCalled)
}

func TestPersistRequestExecution_OnOutboundRawError_ResponseQualityGuardMarksExecutionFailed(t *testing.T) {
	t.Parallel()

	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := authz.WithTestBypass(context.Background())
	ctx = ent.NewContext(ctx, client)

	req, err := client.Request.Create().
		SetProjectID(1).
		SetSource("test").
		SetModelID("gpt-5").
		SetFormat("openai/chat_completions").
		SetRequestBody([]byte(`{}`)).
		SetStatus(request.StatusProcessing).
		Save(ctx)
	require.NoError(t, err)

	execRow, err := client.RequestExecution.Create().
		SetProjectID(1).
		SetRequestID(req.ID).
		SetChannelID(1).
		SetModelID("gpt-5.4").
		SetFormat("openai/chat_completions").
		SetRequestBody([]byte(`{}`)).
		SetStatus(requestexecution.StatusProcessing).
		Save(ctx)
	require.NoError(t, err)

	systemService := biz.NewSystemService(biz.SystemServiceParams{Ent: client})
	requestService := biz.NewRequestService(client, systemService, nil, nil, nil)

	middleware := &persistRequestExecutionMiddleware{
		outbound: &PersistentOutboundTransformer{
			state: &PersistenceState{
				Request:        req,
				RequestExec:    execRow,
				RequestService: requestService,
			},
		},
	}

	guardErr := &ResponseQualityGuardMatchedError{
		RequestedModel:  "gpt-5",
		ActualModel:     "gpt-5.4",
		ReasoningTokens: 100,
		Threshold:       516,
		GuardMode:       responseQualityGuardModeRetryOnMatch,
	}

	middleware.OnOutboundRawError(ctx, guardErr)

	dbExec, err := client.RequestExecution.Get(ctx, execRow.ID)
	require.NoError(t, err)
	assert.Equal(t, requestexecution.StatusFailed, dbExec.Status)
	assert.Contains(t, dbExec.ErrorMessage, "response quality guard matched")
}
