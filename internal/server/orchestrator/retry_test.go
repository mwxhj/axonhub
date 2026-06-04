package orchestrator

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestDeriveLoadBalancerStrategy(t *testing.T) {
	defaultStrategy := "adaptive"
	retryPolicy := &biz.RetryPolicy{
		LoadBalancerStrategy: defaultStrategy,
	}

	tests := []struct {
		name     string
		apiKey   *ent.APIKey
		expected string
	}{
		{
			name:     "apiKey is nil",
			apiKey:   nil,
			expected: defaultStrategy,
		},
		{
			name:     "api key without profiles uses system strategy",
			apiKey:   &ent.APIKey{},
			expected: defaultStrategy,
		},
		{
			name:     "api key cannot override system strategy anymore",
			apiKey:   &ent.APIKey{},
			expected: defaultStrategy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deriveLoadBalancerStrategy(retryPolicy, tt.apiKey)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractStatusCodeFromError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{
			name:     "error is nil",
			err:      nil,
			expected: 0,
		},
		{
			name: "httpclient.Error",
			err: &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
			},
			expected: http.StatusTooManyRequests,
		},
		{
			name: "llm.ResponseError",
			err: &llm.ResponseError{
				StatusCode: http.StatusInternalServerError,
			},
			expected: http.StatusInternalServerError,
		},
		{
			name:     "wrapped httpclient.Error",
			err:      errors.New("wrapped: " + (&httpclient.Error{StatusCode: 401}).Error()), // This won't work with errors.As unless we use fmt.Errorf with %w
			expected: 0,
		},
		{
			name:     "wrapped httpclient.Error with %w",
			err:      errors.Join(errors.New("error"), &httpclient.Error{StatusCode: 401}),
			expected: 401,
		},
		{
			name:     "generic error",
			err:      errors.New("generic error"),
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractStatusCodeFromError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "error is nil",
			err:      nil,
			expected: false,
		},
		{
			name: "429 Too Many Requests is retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusTooManyRequests,
			},
			expected: true,
		},
		{
			name: "400 Bad Request is not retryable",
			err: &httpclient.Error{
				StatusCode: http.StatusBadRequest,
			},
			expected: false,
		},
		{
			name: "500 Internal Server Error is retryable",
			err: &llm.ResponseError{
				StatusCode: http.StatusInternalServerError,
			},
			expected: true,
		},
		{
			name:     "generic error is not retryable",
			err:      errors.New("generic error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
