package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/credentialquotascope"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/upstreamcredential"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer"
)

// mockTransformer is a simple mock transformer for testing.
type mockTransformer struct {
	aggregatedResponse []byte
	aggregatedMeta     llm.ResponseMeta
	aggregatedErr      error
	apiFormat          llm.APIFormat

	credentialID          int
	credentialAPIKey      string
	credentialFingerprint string
	secretFingerprint     string
	resourceScopeKey      string
	credentialName        string
	credentialKeyHint     string
	credentialSource      string
	credentialQuotaStatus string
	quotaScopeID          int
	quotaScopeName        string
	quotaScopeStatus      string
}

func (m *mockTransformer) TransformRequest(ctx context.Context, req *llm.Request) (*httpclient.Request, error) {
	if m.credentialID > 0 || m.credentialAPIKey != "" || m.credentialFingerprint != "" {
		contexts.WithChannelCredential(ctx, m.credentialID, m.credentialAPIKey, m.credentialFingerprint)
	}
	if m.secretFingerprint != "" || m.resourceScopeKey != "" {
		contexts.WithChannelCredentialIdentity(ctx, m.secretFingerprint, m.resourceScopeKey)
	}
	if m.credentialName != "" || m.credentialKeyHint != "" || m.credentialSource != "" || m.credentialQuotaStatus != "" {
		contexts.WithChannelCredentialMetadata(ctx, m.credentialName, m.credentialKeyHint, m.credentialSource, m.credentialQuotaStatus)
	}
	if m.quotaScopeID > 0 || m.quotaScopeName != "" || m.quotaScopeStatus != "" {
		contexts.WithChannelCredentialQuotaScope(ctx, m.quotaScopeID, m.quotaScopeName, m.quotaScopeStatus)
	}

	body, err := json.Marshal(map[string]any{
		"model":       req.Model,
		"messages":    req.Messages,
		"temperature": 0.5,
		"max_tokens":  1000,
	})
	if err != nil {
		return nil, err
	}

	return &httpclient.Request{
		Method: "POST",
		URL:    "https://api.example.com/v1/chat/completions",
		Body:   body,
	}, nil
}

func (m *mockTransformer) TransformResponse(ctx context.Context, resp *httpclient.Response) (*llm.Response, error) {
	return &llm.Response{}, nil
}

func (m *mockTransformer) TransformStream(ctx context.Context, req *httpclient.Request, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	return nil, nil
}

func (m *mockTransformer) TransformError(ctx context.Context, err *httpclient.Error) *llm.ResponseError {
	return nil
}

func (m *mockTransformer) AggregateStreamChunks(ctx context.Context, _ *httpclient.Request, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return m.aggregatedResponse, m.aggregatedMeta, m.aggregatedErr
}

func (m *mockTransformer) APIFormat() llm.APIFormat {
	if m.apiFormat != "" {
		return m.apiFormat
	}

	return llm.APIFormatOpenAIChatCompletion
}

type credentialSelectingTransformer struct {
	provider *biz.TraceStickyKeyProvider
}

func (m *credentialSelectingTransformer) TransformRequest(ctx context.Context, req *llm.Request) (*httpclient.Request, error) {
	apiKey := m.provider.Get(ctx)

	body, err := json.Marshal(map[string]any{
		"model":   req.Model,
		"api_key": apiKey,
	})
	if err != nil {
		return nil, err
	}

	return &httpclient.Request{
		Method: "POST",
		URL:    "https://api.example.com/v1/chat/completions",
		Body:   body,
	}, nil
}

func (m *credentialSelectingTransformer) TransformResponse(ctx context.Context, resp *httpclient.Response) (*llm.Response, error) {
	return &llm.Response{}, nil
}

func (m *credentialSelectingTransformer) TransformStream(ctx context.Context, req *httpclient.Request, stream streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
	return nil, nil
}

func (m *credentialSelectingTransformer) TransformError(ctx context.Context, err *httpclient.Error) *llm.ResponseError {
	return nil
}

func (m *credentialSelectingTransformer) AggregateStreamChunks(ctx context.Context, _ *httpclient.Request, chunks []*httpclient.StreamEvent) ([]byte, llm.ResponseMeta, error) {
	return nil, llm.ResponseMeta{}, nil
}

func (m *credentialSelectingTransformer) APIFormat() llm.APIFormat {
	return llm.APIFormatOpenAIChatCompletion
}

func TestPersistentOutboundTransformer_TransformRequest_OriginalModelRestoration(t *testing.T) {
	tests := []struct {
		name               string
		originalModel      string
		inputModel         string
		actualModel        string
		expectedFinalModel string
	}{
		{
			name:               "no original model - should use candidate ActualModel",
			originalModel:      "",
			inputModel:         "gpt-4",
			actualModel:        "gpt-4",
			expectedFinalModel: "gpt-4",
		},
		{
			name:               "has original model - should use candidate ActualModel (not OriginalModel)",
			originalModel:      "gpt-3.5-turbo",
			inputModel:         "mapped-gpt-4",
			actualModel:        "gpt-4",
			expectedFinalModel: "gpt-4",
		},
		{
			name:               "candidate ActualModel different from input - should use ActualModel",
			originalModel:      "gpt-4",
			inputModel:         "mapped-gpt-4",
			actualModel:        "claude-3-opus",
			expectedFinalModel: "claude-3-opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			ctx := context.Background()

			channel := &biz.Channel{
				Channel: &ent.Channel{
					ID:              1,
					Name:            "test-channel",
					SupportedModels: []string{"gpt-4", "gpt-3.5-turbo"},
					Settings:        nil,
				},
				Outbound: &mockTransformer{},
			}

			processor := &PersistentOutboundTransformer{
				wrapped: &mockTransformer{},
				state: &PersistenceState{
					OriginalModel:    tt.originalModel,
					CurrentCandidate: &ChannelModelsCandidate{Channel: channel},
					ChannelModelsCandidates: []*ChannelModelsCandidate{
						{Channel: channel, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: tt.inputModel, ActualModel: tt.actualModel}}},
					},
					CurrentCandidateIndex: 0,
					RequestExec:           &ent.RequestExecution{ID: 1}, // Dummy to skip creation
				},
			}

			text := "Hello"
			llmRequest := &llm.Request{
				Model: tt.inputModel,
				Messages: []llm.Message{
					{
						Role: "user",
						Content: llm.MessageContent{
							Content: &text,
						},
					},
				},
			}

			// Execute
			channelRequest, err := processor.TransformRequest(ctx, llmRequest)

			// Assert
			require.NoError(t, err)
			require.NotNil(t, channelRequest)

			// Verify model restoration in the request body
			bodyStr := string(channelRequest.Body)
			model := gjson.Get(bodyStr, "model")
			require.Equal(t, tt.expectedFinalModel, model.String())

			// Also verify the llmRequest was modified
			require.Equal(t, tt.expectedFinalModel, llmRequest.Model)
		})
	}
}

func TestPersistRequestExecutionStoresCredentialPerRetryAttempt(t *testing.T) {
	ctx, client := setupTest(t)
	project := createTestProject(t, ctx, client)
	ch1 := createTestChannel(t, ctx, client)
	ch2, err := client.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Second OpenAI Channel").
		SetBaseURL("https://api.openai.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "second-test-api-key"}).
		SetSupportedModels([]string{"gpt-4"}).
		SetDefaultTestModel("gpt-4").
		Save(ctx)
	require.NoError(t, err)
	_, requestService, _, _ := setupTestServices(t, client)

	req, err := client.Request.Create().
		SetProjectID(project.ID).
		SetModelID("gpt-4").
		SetStatus(request.StatusProcessing).
		SetRequestBody(objects.JSONRawMessage([]byte(`{"model":"gpt-4"}`))).
		Save(ctx)
	require.NoError(t, err)

	scope1, err := client.CredentialQuotaScope.Create().
		SetName("scope-one").
		SetStatus(credentialquotascope.StatusAvailable).
		Save(ctx)
	require.NoError(t, err)
	scope2, err := client.CredentialQuotaScope.Create().
		SetName("scope-two").
		SetStatus(credentialquotascope.StatusWarning).
		Save(ctx)
	require.NoError(t, err)
	cred1, err := client.UpstreamCredential.Create().
		SetName("first credential").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetSecretPayload(objects.UpstreamCredentialSecretFromAPIKey("first-raw-key")).
		SetFingerprint("cred:first").
		SetSecretFingerprint("secret:first").
		SetKeyHint("firs...t-key").
		SetQuotaStatus("available").
		SetQuotaScopeID(scope1.ID).
		Save(ctx)
	require.NoError(t, err)
	cred2, err := client.UpstreamCredential.Create().
		SetName("second credential").
		SetAuthKind(upstreamcredential.AuthKindAPIKey).
		SetSecretKind(upstreamcredential.SecretKindAPIKey).
		SetSecretPayload(objects.UpstreamCredentialSecretFromAPIKey("second-raw-key")).
		SetFingerprint("cred:second").
		SetSecretFingerprint("secret:second").
		SetKeyHint("seco...d-key").
		SetQuotaStatus("warning").
		SetQuotaScopeID(scope2.ID).
		Save(ctx)
	require.NoError(t, err)

	firstOutbound := &mockTransformer{
		credentialID:          cred1.ID,
		credentialAPIKey:      "first-raw-key",
		credentialFingerprint: "cred:first",
		secretFingerprint:     "secret:first",
		resourceScopeKey:      "openai:secret:first",
		credentialName:        "first credential",
		credentialKeyHint:     "firs...t-key",
		credentialSource:      biz.ChannelCredentialSourceRef,
		credentialQuotaStatus: "available",
		quotaScopeID:          scope1.ID,
		quotaScopeName:        scope1.Name,
		quotaScopeStatus:      scope1.Status.String(),
	}
	secondOutbound := &mockTransformer{
		credentialID:          cred2.ID,
		credentialAPIKey:      "second-raw-key",
		credentialFingerprint: "cred:second",
		secretFingerprint:     "secret:second",
		resourceScopeKey:      "openai:secret:second",
		credentialName:        "second credential",
		credentialKeyHint:     "seco...d-key",
		credentialSource:      biz.ChannelCredentialSourceRef,
		credentialQuotaStatus: "warning",
		quotaScopeID:          scope2.ID,
		quotaScopeName:        scope2.Name,
		quotaScopeStatus:      scope2.Status.String(),
	}

	state := &PersistenceState{
		Request:        req,
		RequestService: requestService,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			{
				Channel:  &biz.Channel{Channel: ch1, Outbound: firstOutbound},
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				Priority: 0,
			},
			{
				Channel:  &biz.Channel{Channel: ch2, Outbound: secondOutbound},
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				Priority: 0,
			},
		},
		CurrentCandidateIndex: 0,
		CurrentModelIndex:     0,
		CurrentCandidate:      &ChannelModelsCandidate{Channel: &biz.Channel{Channel: ch1, Outbound: firstOutbound}},
	}
	state.CurrentCandidate = state.ChannelModelsCandidates[0]
	processor := &PersistentOutboundTransformer{
		wrapped: firstOutbound,
		state:   state,
	}
	middleware := persistRequestExecution(processor)
	llmReq := &llm.Request{Model: "gpt-4"}

	rawReq, err := processor.TransformRequest(ctx, llmReq)
	require.NoError(t, err)
	_, err = middleware.OnOutboundRawRequest(ctx, rawReq)
	require.NoError(t, err)
	firstExec := processor.GetRequestExecution()
	require.NotNil(t, firstExec)

	require.NoError(t, processor.NextChannel(ctx))
	rawReq, err = processor.TransformRequest(ctx, llmReq)
	require.NoError(t, err)
	_, err = middleware.OnOutboundRawRequest(ctx, rawReq)
	require.NoError(t, err)
	secondExec := processor.GetRequestExecution()
	require.NotNil(t, secondExec)

	executions, err := client.RequestExecution.Query().
		Where(requestexecution.RequestID(req.ID)).
		Order(ent.Asc(requestexecution.FieldID)).
		All(ctx)
	require.NoError(t, err)
	require.Len(t, executions, 2)

	require.Equal(t, firstExec.ID, executions[0].ID)
	require.Equal(t, ch1.ID, executions[0].ChannelID)
	require.Equal(t, cred1.ID, executions[0].CredentialID)
	require.Equal(t, "cred:first", executions[0].CredentialFingerprint)
	require.Equal(t, "secret:first", executions[0].SecretFingerprint)
	require.Equal(t, "openai:secret:first", executions[0].ResourceScopeKey)
	require.Equal(t, "first credential", executions[0].CredentialNameSnapshot)
	require.Equal(t, "firs...t-key", executions[0].CredentialKeyHint)
	require.Equal(t, biz.ChannelCredentialSourceRef, executions[0].CredentialSource)
	require.Equal(t, "available", executions[0].CredentialQuotaStatusSnapshot)
	require.Equal(t, scope1.ID, executions[0].QuotaScopeID)
	require.Equal(t, "scope-one", executions[0].QuotaScopeNameSnapshot)
	require.Equal(t, credentialquotascope.StatusAvailable.String(), executions[0].QuotaScopeStatusSnapshot)

	require.Equal(t, secondExec.ID, executions[1].ID)
	require.Equal(t, ch2.ID, executions[1].ChannelID)
	require.Equal(t, cred2.ID, executions[1].CredentialID)
	require.Equal(t, "cred:second", executions[1].CredentialFingerprint)
	require.Equal(t, "secret:second", executions[1].SecretFingerprint)
	require.Equal(t, "openai:secret:second", executions[1].ResourceScopeKey)
	require.Equal(t, "second credential", executions[1].CredentialNameSnapshot)
	require.Equal(t, "seco...d-key", executions[1].CredentialKeyHint)
	require.Equal(t, biz.ChannelCredentialSourceRef, executions[1].CredentialSource)
	require.Equal(t, "warning", executions[1].CredentialQuotaStatusSnapshot)
	require.Equal(t, scope2.ID, executions[1].QuotaScopeID)
	require.Equal(t, "scope-two", executions[1].QuotaScopeNameSnapshot)
	require.Equal(t, credentialquotascope.StatusWarning.String(), executions[1].QuotaScopeStatusSnapshot)
}

func TestPersistentOutboundTransformer_PrepareForRetry(t *testing.T) {
	// Setup
	ctx := context.Background()

	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: &mockTransformer{},
	}

	t.Run("single model, retry should trigger 'reuse same model' logic", func(t *testing.T) {
		// Case: single model, retry should trigger "reuse same model" logic
		processor := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models: []biz.ChannelModelEntry{
						{RequestModel: "gpt-4", ActualModel: "gpt-4"},
					},
				},
				CurrentModelIndex: 0,
				RequestExec:       &ent.RequestExecution{ID: 1},
			},
		}

		// Execute PrepareForRetry
		// It should reset RequestExec and do not increase the CurrentModelIndex
		err := processor.PrepareForRetry(ctx)

		// Assert
		require.NoError(t, err)
		require.Zero(t, processor.state.CurrentModelIndex)
		require.Nil(t, processor.state.RequestExec)
	})

	t.Run("multiple models, retry still reuses same target", func(t *testing.T) {
		processor := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models: []biz.ChannelModelEntry{
						{RequestModel: "gpt-4", ActualModel: "gpt-4"},
						{RequestModel: "gpt-3.5-turbo", ActualModel: "gpt-3.5-turbo"},
					},
				},
				CurrentModelIndex: 0,
				RequestExec:       &ent.RequestExecution{ID: 1},
			},
		}

		err := processor.PrepareForRetry(ctx)

		require.NoError(t, err)
		require.Zero(t, processor.state.CurrentModelIndex)
		require.Nil(t, processor.state.RequestExec)
	})
}

func TestPersistentOutboundTransformer_PrepareForRetry_UsesCandidateAPIFormatOutbound(t *testing.T) {
	ctx := context.Background()

	primaryOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIChatCompletion}
	embeddingOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIEmbedding}
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: primaryOutbound,
		Outbounds: map[string]transformer.Outbound{
			llm.APIFormatOpenAIEmbedding.String(): embeddingOutbound,
		},
	}

	processor := &PersistentOutboundTransformer{
		wrapped: primaryOutbound,
		state: &PersistenceState{
			CurrentCandidate: &ChannelModelsCandidate{
				Channel:   channel,
				APIFormat: llm.APIFormatOpenAIEmbedding.String(),
				Models: []biz.ChannelModelEntry{
					{RequestModel: "text-embedding-3-small", ActualModel: "text-embedding-3-small"},
					{RequestModel: "text-embedding-3-large", ActualModel: "text-embedding-3-large"},
				},
			},
			CurrentModelIndex: 0,
			RequestExec:       &ent.RequestExecution{ID: 1},
		},
	}

	err := processor.PrepareForRetry(ctx)
	require.NoError(t, err)
	require.Zero(t, processor.state.CurrentModelIndex)
	require.Same(t, embeddingOutbound, processor.wrapped)
}

func TestPersistentOutboundTransformer_NextChannel_UsesCandidateAPIFormatOutbound(t *testing.T) {
	ctx := context.Background()

	primaryOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIChatCompletion}
	embeddingOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIEmbedding}
	chatChannel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "chat-channel",
		},
		Outbound: primaryOutbound,
	}
	embeddingChannel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   2,
			Name: "embedding-channel",
		},
		Outbound: primaryOutbound,
		Outbounds: map[string]transformer.Outbound{
			llm.APIFormatOpenAIEmbedding.String(): embeddingOutbound,
		},
	}

	processor := &PersistentOutboundTransformer{
		wrapped: primaryOutbound,
		state: &PersistenceState{
			CurrentCandidateIndex: 0,
			ChannelModelsCandidates: []*ChannelModelsCandidate{
				{
					Channel: chatChannel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4o-mini", ActualModel: "gpt-4o-mini"}},
				},
				{
					Channel:   embeddingChannel,
					APIFormat: llm.APIFormatOpenAIEmbedding.String(),
					Models:    []biz.ChannelModelEntry{{RequestModel: "text-embedding-3-small", ActualModel: "text-embedding-3-small"}},
				},
			},
		},
	}

	err := processor.NextChannel(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processor.state.CurrentCandidateIndex)
	require.Same(t, embeddingChannel, processor.state.CurrentCandidate.Channel)
	require.Same(t, embeddingOutbound, processor.wrapped)
}

func TestPersistentOutboundTransformer_PrepareForFallback_SameChannelCredential(t *testing.T) {
	ctx := context.Background()

	ch := &biz.Channel{
		Channel: &ent.Channel{
			ID:      1,
			Name:    "same-channel",
			Type:    channel.TypeOpenai,
			BaseURL: "https://api.openai.com/v1",
		},
	}
	firstFingerprint := ch.CredentialFingerprintForAPIKey("key-1")
	secondFingerprint := ch.CredentialFingerprintForAPIKey("key-2")
	ch = ch.WithCredentialViewsForSelection([]biz.ChannelCredentialView{
		{
			CredentialID: 1,
			Fingerprint:  firstFingerprint,
			Secret:       objects.UpstreamCredentialSecretFromAPIKey("key-1"),
			AuthKind:     "api_key",
			SecretKind:   "api_key",
			Enabled:      true,
		},
		{
			CredentialID: 2,
			Fingerprint:  secondFingerprint,
			Secret:       objects.UpstreamCredentialSecretFromAPIKey("key-2"),
			AuthKind:     "api_key",
			SecretKind:   "api_key",
			Enabled:      true,
		},
	})
	outboundTransformer := &credentialSelectingTransformer{provider: biz.NewTraceStickyKeyProvider(ch)}
	ch.Outbound = outboundTransformer

	state := &PersistenceState{
		PreferredCredentialFingerprint: firstFingerprint,
		CurrentCandidateIndex:          0,
		CurrentModelIndex:              0,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			{
				Channel:  ch,
				Priority: 0,
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
			},
		},
	}
	state.CurrentCandidate = state.ChannelModelsCandidates[0]
	processor := &PersistentOutboundTransformer{
		wrapped: outboundTransformer,
		state:   state,
	}

	rawReq, err := processor.TransformRequest(ctx, &llm.Request{Model: "gpt-4"})
	require.NoError(t, err)
	require.Equal(t, "key-1", gjson.GetBytes(rawReq.Body, "api_key").String())
	require.Equal(t, firstFingerprint, state.CurrentCredentialFingerprint)

	upstreamErr := &httpclient.Error{StatusCode: http.StatusUnauthorized}
	require.True(t, processor.CanFallback(upstreamErr))
	require.NoError(t, processor.PrepareForFallback(ctx, upstreamErr))
	require.Equal(t, 0, state.CurrentCandidateIndex)
	require.Equal(t, []string{firstFingerprint}, state.ExcludedCredentialFingerprints)
	require.Equal(t, 1, state.FallbackTargetSwitches)

	rawReq, err = processor.TransformRequest(ctx, &llm.Request{Model: "gpt-4"})
	require.NoError(t, err)
	require.Equal(t, "key-2", gjson.GetBytes(rawReq.Body, "api_key").String())
	require.Equal(t, secondFingerprint, state.CurrentCredentialFingerprint)
}

func TestPersistentOutboundTransformer_PrepareForFallback_StaysInSamePriorityBeforeLowerPriority(t *testing.T) {
	ctx := context.Background()

	newCandidate := func(id int, name string, priority int) *ChannelModelsCandidate {
		return &ChannelModelsCandidate{
			Channel: &biz.Channel{
				Channel: &ent.Channel{
					ID:   id,
					Name: name,
				},
				Outbound: &mockTransformer{},
			},
			Priority: priority,
			Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
		}
	}

	state := &PersistenceState{
		CurrentCandidateIndex: 0,
		CurrentModelIndex:     0,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			newCandidate(1, "current", 0),
			newCandidate(2, "same-priority", 0),
			newCandidate(3, "lower-priority", 1),
		},
	}
	state.CurrentCandidate = state.ChannelModelsCandidates[0]
	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state:   state,
	}

	require.True(t, processor.CanFallback(&httpclient.Error{StatusCode: http.StatusInternalServerError}))
	require.NoError(t, processor.PrepareForFallback(ctx, &httpclient.Error{StatusCode: http.StatusInternalServerError}))
	require.Equal(t, 1, state.CurrentCandidateIndex)
	require.Equal(t, 2, state.CurrentCandidate.Channel.ID)
	require.Equal(t, 0, state.CurrentCandidate.Priority)

	require.NoError(t, processor.PrepareForFallback(ctx, &httpclient.Error{StatusCode: http.StatusInternalServerError}))
	require.Equal(t, 2, state.CurrentCandidateIndex)
	require.Equal(t, 3, state.CurrentCandidate.Channel.ID)
	require.Equal(t, 1, state.CurrentCandidate.Priority)
}

func TestPersistentOutboundTransformer_PrepareForFallback_SwitchesExplicitSameChannelModelCandidate(t *testing.T) {
	ctx := context.Background()
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "same-channel",
		},
		Outbound: &mockTransformer{},
	}
	state := &PersistenceState{
		CurrentCandidateIndex: 0,
		CurrentModelIndex:     0,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			{
				Channel:  channel,
				Priority: 0,
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "model-a"}},
			},
			{
				Channel:  channel,
				Priority: 0,
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "model-b"}},
			},
		},
	}
	state.CurrentCandidate = state.ChannelModelsCandidates[0]
	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state:   state,
	}
	err := &httpclient.Error{StatusCode: http.StatusInternalServerError}

	require.False(t, processor.CanRetry(err))
	require.True(t, processor.CanFallback(err))
	require.NoError(t, processor.PrepareForFallback(ctx, err))
	require.Equal(t, 1, state.CurrentCandidateIndex)
	require.Zero(t, state.CurrentModelIndex)
	require.Same(t, channel, state.CurrentCandidate.Channel)
	require.Equal(t, "model-b", state.CurrentCandidate.Models[0].ActualModel)
}

func TestPersistentOutboundTransformer_CanRetry_UsesFallbackWhenAvailable(t *testing.T) {
	channelOne := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "primary",
		},
		Outbound: &mockTransformer{},
	}
	channelTwo := &biz.Channel{
		Channel: &ent.Channel{
			ID:   2,
			Name: "fallback",
		},
		Outbound: &mockTransformer{},
	}

	state := &PersistenceState{
		CurrentCandidateIndex: 0,
		CurrentModelIndex:     0,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			{
				Channel:  channelOne,
				Priority: 0,
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
			},
			{
				Channel:  channelTwo,
				Priority: 0,
				Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
			},
		},
	}
	state.CurrentCandidate = state.ChannelModelsCandidates[0]
	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state:   state,
	}
	err := &httpclient.Error{StatusCode: http.StatusInternalServerError}

	require.False(t, processor.CanRetry(err))
	require.True(t, processor.CanFallback(err))
}

func TestPersistentOutboundTransformer_PrepareForFallback_SkipsCandidateWithFailedCredentialOnly(t *testing.T) {
	ctx := context.Background()

	sharedKey := "shared-key"
	otherKey := "other-key"
	sharedFingerprint := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", sharedKey)
	otherFingerprint := biz.ChannelCredentialFingerprintForAPIKey(channel.TypeOpenai.String(), "https://api.openai.com/v1", otherKey)

	newCredentialCandidate := func(id int, name string, key string, fingerprint string) *ChannelModelsCandidate {
		return &ChannelModelsCandidate{
			Channel: &biz.Channel{
				Channel: &ent.Channel{
					ID:      id,
					Name:    name,
					Type:    channel.TypeOpenai,
					BaseURL: "https://api.openai.com/v1",
					Credentials: objects.ChannelCredentials{
						APIKeys: []string{key},
					},
				},
				Outbound: &mockTransformer{},
			},
			Priority: 0,
			Models:   []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
		}
	}

	state := &PersistenceState{
		CurrentCredentialFingerprint: sharedFingerprint,
		CurrentCandidateIndex:        0,
		CurrentModelIndex:            0,
		ChannelModelsCandidates: []*ChannelModelsCandidate{
			newCredentialCandidate(1, "failed", sharedKey, sharedFingerprint),
			newCredentialCandidate(2, "same-failed-credential", sharedKey, sharedFingerprint),
			newCredentialCandidate(3, "other-credential", otherKey, otherFingerprint),
		},
	}
	state.ChannelModelsCandidates[0].Channel = state.ChannelModelsCandidates[0].Channel.WithCredentialViewsForSelection([]biz.ChannelCredentialView{
		{
			Fingerprint: sharedFingerprint,
			Secret:      objects.UpstreamCredentialSecretFromAPIKey(sharedKey),
			AuthKind:    "api_key",
			SecretKind:  "api_key",
			Enabled:     true,
		},
	})
	state.ChannelModelsCandidates[1].Channel = state.ChannelModelsCandidates[1].Channel.WithCredentialViewsForSelection([]biz.ChannelCredentialView{
		{
			Fingerprint: sharedFingerprint,
			Secret:      objects.UpstreamCredentialSecretFromAPIKey(sharedKey),
			AuthKind:    "api_key",
			SecretKind:  "api_key",
			Enabled:     true,
		},
	})
	state.ChannelModelsCandidates[2].Channel = state.ChannelModelsCandidates[2].Channel.WithCredentialViewsForSelection([]biz.ChannelCredentialView{
		{
			Fingerprint: otherFingerprint,
			Secret:      objects.UpstreamCredentialSecretFromAPIKey(otherKey),
			AuthKind:    "api_key",
			SecretKind:  "api_key",
			Enabled:     true,
		},
	})
	state.CurrentCandidate = state.ChannelModelsCandidates[0]

	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state:   state,
	}

	err := &httpclient.Error{StatusCode: http.StatusUnauthorized}
	require.True(t, processor.CanFallback(err))
	require.NoError(t, processor.PrepareForFallback(ctx, err))
	require.Equal(t, 2, state.CurrentCandidateIndex)
	require.Equal(t, 3, state.CurrentCandidate.Channel.ID)
	require.Equal(t, []string{sharedFingerprint}, state.ExcludedCredentialFingerprints)
}

func TestSelectOutboundForCandidate(t *testing.T) {
	primaryOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIChatCompletion}
	embeddingOutbound := &mockTransformer{apiFormat: llm.APIFormatOpenAIEmbedding}

	t.Run("nil candidate returns nil", func(t *testing.T) {
		require.Nil(t, selectOutboundForCandidate(nil))
	})

	t.Run("candidate with nil channel returns nil", func(t *testing.T) {
		candidate := &ChannelModelsCandidate{APIFormat: llm.APIFormatOpenAIEmbedding.String()}
		require.Nil(t, selectOutboundForCandidate(candidate))
	})

	t.Run("api format set and found in outbounds returns matching outbound", func(t *testing.T) {
		channel := &biz.Channel{
			Channel:   &ent.Channel{ID: 1, Name: "test"},
			Outbound:  primaryOutbound,
			Outbounds: map[string]transformer.Outbound{llm.APIFormatOpenAIEmbedding.String(): embeddingOutbound},
		}
		candidate := &ChannelModelsCandidate{
			Channel:   channel,
			APIFormat: llm.APIFormatOpenAIEmbedding.String(),
		}
		require.Same(t, embeddingOutbound, selectOutboundForCandidate(candidate))
	})

	t.Run("api format set but not in outbounds falls back to channel outbound", func(t *testing.T) {
		channel := &biz.Channel{
			Channel:   &ent.Channel{ID: 1, Name: "test"},
			Outbound:  primaryOutbound,
			Outbounds: map[string]transformer.Outbound{},
		}
		candidate := &ChannelModelsCandidate{
			Channel:   channel,
			APIFormat: llm.APIFormatOpenAIEmbedding.String(),
		}
		require.Same(t, primaryOutbound, selectOutboundForCandidate(candidate))
	})

	t.Run("nil outbounds falls back to channel outbound", func(t *testing.T) {
		channel := &biz.Channel{
			Channel:  &ent.Channel{ID: 1, Name: "test"},
			Outbound: primaryOutbound,
		}
		candidate := &ChannelModelsCandidate{
			Channel:   channel,
			APIFormat: llm.APIFormatOpenAIEmbedding.String(),
		}
		require.Same(t, primaryOutbound, selectOutboundForCandidate(candidate))
	})

	t.Run("empty api format falls back to channel outbound", func(t *testing.T) {
		channel := &biz.Channel{
			Channel:   &ent.Channel{ID: 1, Name: "test"},
			Outbound:  primaryOutbound,
			Outbounds: map[string]transformer.Outbound{llm.APIFormatOpenAIEmbedding.String(): embeddingOutbound},
		}
		candidate := &ChannelModelsCandidate{
			Channel:   channel,
			APIFormat: "",
		}
		require.Same(t, primaryOutbound, selectOutboundForCandidate(candidate))
	})
}

func TestPersistentOutboundTransformer_TransformRequest_ResetsStreamCompletedForNewAttempt(t *testing.T) {
	ctx := context.Background()

	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:              1,
			Name:            "test-channel",
			SupportedModels: []string{"gpt-4"},
		},
		Outbound: &mockTransformer{},
	}

	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state: &PersistenceState{
			StreamCompleted: true,
			ChannelModelsCandidates: []*ChannelModelsCandidate{
				{Channel: channel, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}}},
			},
			CurrentCandidateIndex: 0,
			RequestExec:           &ent.RequestExecution{ID: 1},
		},
	}

	text := "Hello"
	llmRequest := &llm.Request{
		Model: "gpt-4",
		Messages: []llm.Message{{
			Role: "user",
			Content: llm.MessageContent{
				Content: &text,
			},
		}},
	}

	_, err := processor.TransformRequest(ctx, llmRequest)
	require.NoError(t, err)
	require.False(t, processor.state.StreamCompleted)
}

func TestPersistentOutboundTransformer_CanRetry(t *testing.T) {
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: &mockTransformer{},
	}

	retryableErr := &httpclient.Error{StatusCode: http.StatusTooManyRequests}
	nonRetryableErr := &httpclient.Error{StatusCode: http.StatusBadRequest}

	t.Run("no current candidate", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: nil,
			},
		}

		require.False(t, outbound.CanRetry(retryableErr))
	})

	t.Run("nil error", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
			},
		}

		require.False(t, outbound.CanRetry(nil))
	})

	t.Run("non-retryable error", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
			},
		}

		require.False(t, outbound.CanRetry(nonRetryableErr))
	})

	t.Run("skip-by-circuit-breaker should not trigger same-channel retry", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models: []biz.ChannelModelEntry{
						{RequestModel: "gpt-4", ActualModel: "gpt-4"},
						{RequestModel: "gpt-3.5-turbo", ActualModel: "gpt-3.5-turbo"},
					},
				},
				CurrentModelIndex: 0,
			},
		}

		require.False(t, outbound.CanRetry(errSkipCandidateByCircuitBreaker))
	})

	t.Run("auto-aggregate empty errors are retryable", func(t *testing.T) {
		for _, retryErr := range []error{
			fmt.Errorf("failed to auto-aggregate streaming response: %w", pipeline.ErrEmptyResponse),
			fmt.Errorf("failed to auto-aggregate streaming response: %w", pipeline.ErrEmptyStreamChunks),
			fmt.Errorf("failed to auto-aggregate streaming response: %w", pipeline.ErrEmptyAggregatedBody),
		} {
			outbound := &PersistentOutboundTransformer{
				wrapped: &mockTransformer{},
				state: &PersistenceState{
					CurrentCandidate: &ChannelModelsCandidate{
						Channel: channel,
						Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
					},
					CurrentModelIndex: 0,
				},
			}

			require.True(t, outbound.CanRetry(retryErr))
		}
	})
}

func TestShouldForceStreamingForCandidate(t *testing.T) {
	newCandidate := func(policy objects.CapabilityPolicy, apiFormat llm.APIFormat) *ChannelModelsCandidate {
		return &ChannelModelsCandidate{
			APIFormat: apiFormat.String(),
			Channel: &biz.Channel{
				Channel: &ent.Channel{
					Policies: objects.ChannelPolicies{Stream: policy},
				},
			},
		}
	}

	t.Run("supported require-stream fallback request forces streaming", func(t *testing.T) {
		require.True(t, shouldForceStreamingForCandidate(
			newCandidate(objects.CapabilityPolicyRequire, llm.APIFormatOpenAIChatCompletion),
			&llm.Request{RequestType: llm.RequestTypeChat, APIFormat: llm.APIFormatOpenAIChatCompletion},
		))
	})

	t.Run("native non-stream candidate does not force streaming", func(t *testing.T) {
		require.False(t, shouldForceStreamingForCandidate(
			newCandidate(objects.CapabilityPolicyUnlimited, llm.APIFormatOpenAIChatCompletion),
			&llm.Request{RequestType: llm.RequestTypeChat, APIFormat: llm.APIFormatOpenAIChatCompletion},
		))
	})

	t.Run("unsupported embedding request does not force streaming", func(t *testing.T) {
		require.False(t, shouldForceStreamingForCandidate(
			newCandidate(objects.CapabilityPolicyRequire, llm.APIFormatOpenAIEmbedding),
			&llm.Request{RequestType: llm.RequestTypeEmbedding, APIFormat: llm.APIFormatOpenAIEmbedding},
		))
	})

	t.Run("unsupported compact request does not force streaming", func(t *testing.T) {
		require.False(t, shouldForceStreamingForCandidate(
			newCandidate(objects.CapabilityPolicyRequire, llm.APIFormatOpenAIResponseCompact),
			&llm.Request{RequestType: llm.RequestTypeCompact, APIFormat: llm.APIFormatOpenAIResponseCompact},
		))
	})

	t.Run("client requested stream keeps existing streaming path", func(t *testing.T) {
		require.False(t, shouldForceStreamingForCandidate(
			newCandidate(objects.CapabilityPolicyRequire, llm.APIFormatOpenAIChatCompletion),
			&llm.Request{Stream: lo.ToPtr(true), RequestType: llm.RequestTypeChat, APIFormat: llm.APIFormatOpenAIChatCompletion},
		))
	})
}

func TestIsCompletedAggregatedOutboundResponse(t *testing.T) {
	t.Run("usage with completion tokens means completed", func(t *testing.T) {
		require.True(t, isCompletedAggregated(llm.ResponseMeta{Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}))
	})

	t.Run("usage with zero completion tokens is not completed", func(t *testing.T) {
		require.False(t, isCompletedAggregated(llm.ResponseMeta{Usage: &llm.Usage{PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10}}))
	})

	t.Run("response id without usage is not completed", func(t *testing.T) {
		require.False(t, isCompletedAggregated(llm.ResponseMeta{ID: "resp_123"}))
	})

	t.Run("missing usage and id is not completed", func(t *testing.T) {
		require.False(t, isCompletedAggregated(llm.ResponseMeta{}))
	})
}

type sliceEventStream struct {
	events []*httpclient.StreamEvent
	index  int
	err    error
	closed bool
}

func (s *sliceEventStream) Next() bool {
	if s.index >= len(s.events) {
		return false
	}

	s.index++
	return true
}

func (s *sliceEventStream) Current() *httpclient.StreamEvent {
	if s.index == 0 || s.index > len(s.events) {
		return nil
	}

	return s.events[s.index-1]
}

func (s *sliceEventStream) Err() error {
	return s.err
}

func (s *sliceEventStream) Close() error {
	s.closed = true
	return nil
}

func TestOutboundPersistentStream_Close_AggregatedResponsesCompletionHandling(t *testing.T) {
	ctx := context.Background()
	ctx = authz.WithTestBypass(ctx)

	t.Run("response in_progress without terminal event is not completed", func(t *testing.T) {
		client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
		defer client.Close()

		ctx := ent.NewContext(ctx, client)
		project := createTestProject(t, ctx, client)
		ch := createTestChannel(t, ctx, client)
		_, requestService, _, usageLogService := setupTestServices(t, client)

		req, err := client.Request.Create().
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetStatus(request.StatusPending).
			SetRequestBody([]byte(`{"stream":true}`)).
			Save(ctx)
		require.NoError(t, err)

		exec, err := client.RequestExecution.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetRequestBody([]byte(`{"stream":true}`)).
			SetFormat("openai/responses").
			SetStatus(requestexecution.StatusPending).
			SetStream(true).
			Save(ctx)
		require.NoError(t, err)

		stream := &sliceEventStream{
			events: []*httpclient.StreamEvent{{Type: "response.in_progress", Data: []byte(`{"type":"response.in_progress"}`)}},
		}
		transformer := &mockTransformer{
			apiFormat:          llm.APIFormatOpenAIResponse,
			aggregatedResponse: []byte(`{"id":"resp_123","status":"in_progress"}`),
		}
		state := &PersistenceState{}

		persistentStream := NewOutboundPersistentStream(ctx, stream, req, exec, requestService, usageLogService, transformer, nil, state)
		for persistentStream.Next() {
			_ = persistentStream.Current()
		}
		require.NoError(t, persistentStream.Close())

		dbExec, err := client.RequestExecution.Get(ctx, exec.ID)
		require.NoError(t, err)
		require.NotEqual(t, requestexecution.StatusCompleted, dbExec.Status)
		require.Equal(t, requestexecution.StatusFailed, dbExec.Status)
		require.Contains(t, dbExec.ErrorMessage, "stream ended without terminal event or completed response")
		require.False(t, state.StreamCompleted)
	})

	t.Run("aggregated completed response without terminal event is completed", func(t *testing.T) {
		client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
		defer client.Close()

		ctx := ent.NewContext(ctx, client)
		project := createTestProject(t, ctx, client)
		ch := createTestChannel(t, ctx, client)
		_, requestService, _, usageLogService := setupTestServices(t, client)

		req, err := client.Request.Create().
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetStatus(request.StatusPending).
			SetRequestBody([]byte(`{"stream":true}`)).
			Save(ctx)
		require.NoError(t, err)

		exec, err := client.RequestExecution.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetRequestBody([]byte(`{"stream":true}`)).
			SetFormat("openai/responses").
			SetStatus(requestexecution.StatusPending).
			SetStream(true).
			Save(ctx)
		require.NoError(t, err)

		aggregated := []byte(`{"id":"resp_456","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)
		stream := &sliceEventStream{
			events: []*httpclient.StreamEvent{{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"hi"}`)}},
		}
		transformer := &mockTransformer{
			apiFormat:          llm.APIFormatOpenAIResponse,
			aggregatedResponse: aggregated,
			aggregatedMeta: llm.ResponseMeta{
				ID: "resp_456",
				Usage: &llm.Usage{
					PromptTokens:     10,
					CompletionTokens: 2,
					TotalTokens:      12,
				},
			},
		}
		state := &PersistenceState{}

		persistentStream := NewOutboundPersistentStream(ctx, stream, req, exec, requestService, usageLogService, transformer, nil, state)
		for persistentStream.Next() {
			_ = persistentStream.Current()
		}
		require.NoError(t, persistentStream.Close())

		dbExec, err := client.RequestExecution.Get(ctx, exec.ID)
		require.NoError(t, err)
		require.Equal(t, requestexecution.StatusCompleted, dbExec.Status)
		require.JSONEq(t, string(aggregated), string(dbExec.ResponseBody))
		require.Equal(t, "resp_456", dbExec.ExternalID)
		require.True(t, state.StreamCompleted)
	})

	t.Run("canceled client with aggregated completed response is still completed", func(t *testing.T) {
		client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
		defer client.Close()

		baseCtx := ent.NewContext(ctx, client)
		project := createTestProject(t, baseCtx, client)
		ch := createTestChannel(t, baseCtx, client)
		_, requestService, _, usageLogService := setupTestServices(t, client)

		req, err := client.Request.Create().
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetStatus(request.StatusPending).
			SetRequestBody([]byte(`{"stream":true}`)).
			Save(baseCtx)
		require.NoError(t, err)

		exec, err := client.RequestExecution.Create().
			SetRequestID(req.ID).
			SetProjectID(project.ID).
			SetChannelID(ch.ID).
			SetModelID("gpt-4.1").
			SetRequestBody([]byte(`{"stream":true}`)).
			SetFormat("openai/responses").
			SetStatus(requestexecution.StatusPending).
			SetStream(true).
			Save(baseCtx)
		require.NoError(t, err)

		aggregated := []byte(`{"id":"resp_codex_like","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}]}`)
		stream := &sliceEventStream{
			events: []*httpclient.StreamEvent{{Type: "response.output_text.delta", Data: []byte(`{"type":"response.output_text.delta","delta":"done"}`)}},
			err:    context.Canceled,
		}
		transformer := &mockTransformer{
			apiFormat:          llm.APIFormatOpenAIResponse,
			aggregatedResponse: aggregated,
			aggregatedMeta: llm.ResponseMeta{
				ID: "resp_codex_like",
				Usage: &llm.Usage{
					PromptTokens:     20,
					CompletionTokens: 1,
					TotalTokens:      21,
				},
			},
		}
		state := &PersistenceState{}

		requestCtx, cancel := context.WithCancel(baseCtx)
		cancel()

		persistentStream := NewOutboundPersistentStream(requestCtx, stream, req, exec, requestService, usageLogService, transformer, nil, state)
		for persistentStream.Next() {
			_ = persistentStream.Current()
		}
		require.NoError(t, persistentStream.Close())

		dbExec, err := client.RequestExecution.Get(baseCtx, exec.ID)
		require.NoError(t, err)
		require.Equal(t, requestexecution.StatusCompleted, dbExec.Status)
		require.JSONEq(t, string(aggregated), string(dbExec.ResponseBody))
		require.Equal(t, "resp_codex_like", dbExec.ExternalID)
		require.True(t, state.StreamCompleted)
	})
}

func TestPersistentOutboundTransformer_TransformRequest_WithPrepopulatedState(t *testing.T) {
	// Setup
	ctx := context.Background()

	// Pre-populate channels (now done by inbound transformer)
	testChannel := &biz.Channel{
		Channel: &ent.Channel{
			ID:              1,
			Name:            "test-channel",
			SupportedModels: []string{"gpt-4", "gpt-3.5-turbo"}, // Add gpt-3.5-turbo
			Settings:        nil,
		},
		Outbound: &mockTransformer{},
	}

	processor := &PersistentOutboundTransformer{
		wrapped: &mockTransformer{},
		state: &PersistenceState{
			OriginalModel: "gpt-3.5-turbo",
			ChannelModelsCandidates: []*ChannelModelsCandidate{
				{Channel: testChannel, Priority: 0, Models: []biz.ChannelModelEntry{{RequestModel: "gpt-3.5-turbo", ActualModel: "gpt-3.5-turbo"}}},
			}, // Pre-populated by inbound
			CurrentCandidateIndex: 0,
			RequestExec:           &ent.RequestExecution{ID: 1}, // Dummy to skip creation
		},
	}

	text := "Hello"
	llmRequest := &llm.Request{
		Model: "mapped-gpt-4", // This was mapped by inbound transformer
		Messages: []llm.Message{
			{
				Role: "user",
				Content: llm.MessageContent{
					Content: &text,
				},
			},
		},
	}

	// Execute
	channelRequest, err := processor.TransformRequest(ctx, llmRequest)

	// Assert
	require.NoError(t, err)
	require.NotNil(t, channelRequest)

	// Verify original model was restored
	require.Equal(t, "gpt-3.5-turbo", llmRequest.Model)

	// Verify channel was used
	require.Equal(t, testChannel, processor.state.CurrentCandidate.Channel)
}

func TestFilterResponseCustomToolMessagesForNonResponsesOutbound(t *testing.T) {
	baseRequest := &llm.Request{
		APIFormat: llm.APIFormatOpenAIResponse,
		Messages: []llm.Message{
			{
				Role: "assistant",
				ToolCalls: []llm.ToolCall{
					{
						ID:   "call_custom_1",
						Type: llm.ToolTypeResponsesCustomTool,
						ResponseCustomToolCall: &llm.ResponseCustomToolCall{
							CallID: "call_custom_1",
							Name:   "apply_patch",
							Input:  "*** Begin Patch\n*** End Patch\n",
						},
					},
					{
						ID:   "call_function_1",
						Type: llm.ToolTypeFunction,
						Function: llm.FunctionCall{
							Name:      "get_weather",
							Arguments: "{}",
						},
					},
				},
			},
			{
				Role:       "tool",
				ToolCallID: func() *string { v := "call_custom_1"; return &v }(),
				Content: llm.MessageContent{
					Content: func() *string { v := "custom"; return &v }(),
				},
			},
			{
				Role:       "tool",
				ToolCallID: func() *string { v := "call_function_1"; return &v }(),
				Content: llm.MessageContent{
					Content: func() *string { v := "function"; return &v }(),
				},
			},
		},
	}

	t.Run("filters when inbound is responses and outbound is not", func(t *testing.T) {
		got := filterResponseCustomToolMessagesForNonResponsesOutbound(baseRequest, llm.APIFormatOpenAIChatCompletion)
		require.NotSame(t, baseRequest, got)
		require.Len(t, got.Messages, 2)
		require.Len(t, got.Messages[0].ToolCalls, 1)
		require.Equal(t, llm.ToolTypeFunction, got.Messages[0].ToolCalls[0].Type)
		require.NotNil(t, got.Messages[1].ToolCallID)
		require.Equal(t, "call_function_1", *got.Messages[1].ToolCallID)
	})

	t.Run("does not filter when outbound is responses", func(t *testing.T) {
		got := filterResponseCustomToolMessagesForNonResponsesOutbound(baseRequest, llm.APIFormatOpenAIResponse)
		require.Same(t, baseRequest, got)
	})

	t.Run("does not filter when inbound is not responses", func(t *testing.T) {
		nonResponsesReq := *baseRequest
		nonResponsesReq.APIFormat = llm.APIFormatOpenAIChatCompletion
		got := filterResponseCustomToolMessagesForNonResponsesOutbound(&nonResponsesReq, llm.APIFormatOpenAIChatCompletion)
		require.Same(t, &nonResponsesReq, got)
	})
}

// ========== 429 Retry-After Tests ==========

func TestPersistentOutboundTransformer_CanRetry_429_WithRetryAfter(t *testing.T) {
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: &mockTransformer{},
	}

	t.Run("429 with Retry-After should not retry same channel", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error with Retry-After header
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers:    http.Header{"Retry-After": []string{"30"}},
		}

		require.False(t, outbound.CanRetry(httpErr))
	})

	t.Run("429 with multiple headers including Retry-After should not retry", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error with multiple headers
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers: http.Header{
				"Retry-After":  []string{"60"},
				"Content-Type": []string{"application/json"},
			},
		}

		require.False(t, outbound.CanRetry(httpErr))
	})
}

func TestPersistentOutboundTransformer_CanRetry_429_WithoutRetryAfter(t *testing.T) {
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: &mockTransformer{},
	}

	t.Run("429 without Retry-After (nil headers) should skip same-target retry", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error without headers
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers:    nil,
		}

		require.False(t, outbound.CanRetry(httpErr))
	})

	t.Run("429 without Retry-After (empty headers) should skip same-target retry", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error with empty headers
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers:    http.Header{},
		}

		require.False(t, outbound.CanRetry(httpErr))
	})

	t.Run("429 without Retry-After (headers but no Retry-After key) should skip same-target retry", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models:  []biz.ChannelModelEntry{{RequestModel: "gpt-4", ActualModel: "gpt-4"}},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error with headers but no Retry-After
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers: http.Header{
				"Content-Type": []string{"application/json"},
			},
		}

		require.False(t, outbound.CanRetry(httpErr))
	})
}

func TestPersistentOutboundTransformer_CanRetry_429_WithMultipleModels(t *testing.T) {
	channel := &biz.Channel{
		Channel: &ent.Channel{
			ID:   1,
			Name: "test-channel",
		},
		Outbound: &mockTransformer{},
	}

	t.Run("429 with Retry-After should not retry even with multiple models", func(t *testing.T) {
		outbound := &PersistentOutboundTransformer{
			wrapped: &mockTransformer{},
			state: &PersistenceState{
				CurrentCandidate: &ChannelModelsCandidate{
					Channel: channel,
					Models: []biz.ChannelModelEntry{
						{RequestModel: "gpt-4", ActualModel: "gpt-4"},
						{RequestModel: "gpt-3.5-turbo", ActualModel: "gpt-3.5-turbo"},
					},
				},
				CurrentModelIndex: 0,
			},
		}

		// 429 error with Retry-After header
		httpErr := &httpclient.Error{
			StatusCode: http.StatusTooManyRequests,
			Headers:    http.Header{"Retry-After": []string{"30"}},
		}

		// Should skip retry even though there are more models
		require.False(t, outbound.CanRetry(httpErr))
	})
}
