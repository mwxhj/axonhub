package cerebras

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
)

func TestOutboundTransformerWithConfig_UsesDefaultBaseURLAndStripsStore(t *testing.T) {
	transformer, err := NewOutboundTransformerWithConfig(&Config{
		APIKeyProvider: auth.NewStaticKeyProvider("test-key"),
	})
	require.NoError(t, err)

	req, err := transformer.TransformRequest(context.Background(), &llm.Request{
		Model: "cerebras-model",
		Messages: []llm.Message{
			{
				Role: "user",
				Content: llm.MessageContent{
					Content: lo.ToPtr("Hello"),
				},
			},
		},
		Store: lo.ToPtr(true),
	})
	require.NoError(t, err)
	require.NotNil(t, req)
	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, DefaultBaseURL+"/chat/completions", req.URL)
	require.NotNil(t, req.Auth)
	require.Equal(t, "bearer", req.Auth.Type)
	require.Equal(t, "test-key", req.Auth.APIKey)

	var body map[string]any
	require.NoError(t, json.Unmarshal(req.Body, &body))
	require.Equal(t, "cerebras-model", body["model"])
	require.NotContains(t, body, "store")
}

func TestOutboundTransformerWithConfig_UsesCustomBaseURL(t *testing.T) {
	transformer, err := NewOutboundTransformerWithConfig(&Config{
		BaseURL:        "https://example.cerebras.local/",
		APIKeyProvider: auth.NewStaticKeyProvider("test-key"),
	})
	require.NoError(t, err)

	req, err := transformer.TransformRequest(context.Background(), &llm.Request{
		Model: "cerebras-model",
		Messages: []llm.Message{
			{
				Role: "user",
				Content: llm.MessageContent{
					Content: lo.ToPtr("Hello"),
				},
			},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, req)
	require.Equal(t, "https://example.cerebras.local/chat/completions", req.URL)
}
