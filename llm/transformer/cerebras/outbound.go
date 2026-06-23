package cerebras

import (
	"context"
	"fmt"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/auth"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer"
	"github.com/looplj/axonhub/llm/transformer/openrouter"
)

const DefaultBaseURL = "https://api.cerebras.ai/v1"

type Config struct {
	BaseURL        string              `json:"base_url,omitempty"`
	APIKeyProvider auth.APIKeyProvider `json:"-"`
}

var _ transformer.Outbound = (*OutboundTransformer)(nil)

type OutboundTransformer struct {
	transformer.Outbound
}

func NewOutboundTransformerWithConfig(config *Config) (transformer.Outbound, error) {
	if config == nil {
		return nil, fmt.Errorf("invalid Cerebras transformer configuration: config is nil")
	}

	if config.APIKeyProvider == nil {
		return nil, fmt.Errorf("invalid Cerebras transformer configuration: API key provider is required")
	}

	baseURL := config.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	t, err := openrouter.NewOutboundTransformerWithConfig(&openrouter.Config{
		BaseURL:        baseURL,
		APIKeyProvider: config.APIKeyProvider,
	})
	if err != nil {
		return nil, fmt.Errorf("invalid Cerebras transformer configuration: %w", err)
	}

	return &OutboundTransformer{Outbound: t}, nil
}

func (t *OutboundTransformer) TransformRequest(ctx context.Context, llmReq *llm.Request) (*httpclient.Request, error) {
	if llmReq == nil {
		return nil, fmt.Errorf("chat completion request is nil")
	}

	reqCopy := *llmReq
	reqCopy.Store = nil

	return t.Outbound.TransformRequest(ctx, &reqCopy)
}
