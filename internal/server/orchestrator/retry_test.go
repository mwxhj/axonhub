package orchestrator

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestStickySessionEnabled(t *testing.T) {
	tests := []struct {
		name        string
		retryPolicy *biz.RetryPolicy
		expected    bool
	}{
		{
			name:        "nil policy",
			retryPolicy: nil,
			expected:    false,
		},
		{
			name:        "adaptive does not enable sticky binding",
			retryPolicy: &biz.RetryPolicy{LoadBalancerStrategy: biz.LoadBalancerStrategyAdaptive},
			expected:    false,
		},
		{
			name:        "sticky-session enables sticky binding",
			retryPolicy: &biz.RetryPolicy{LoadBalancerStrategy: biz.LoadBalancerStrategyStickySession},
			expected:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stickySessionEnabled(tt.retryPolicy)
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
