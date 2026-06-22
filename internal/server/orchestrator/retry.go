package orchestrator

import (
	"errors"
	"net/http"

	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/pipeline"
)

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	return httpclient.IsHTTPStatusCodeRetryable(ExtractStatusCodeFromError(err))
}

func isCredentialScopedFallbackError(err error) bool {
	if err == nil {
		return false
	}

	switch ExtractStatusCodeFromError(err) {
	case http.StatusUnauthorized,
		http.StatusPaymentRequired,
		http.StatusForbidden,
		http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

func isFallbackableError(err error) bool {
	if err == nil {
		return false
	}

	if errors.Is(err, errSkipCandidateByCircuitBreaker) ||
		isChannelQueueError(err) ||
		errors.Is(err, pipeline.ErrEmptyResponse) ||
		errors.Is(err, pipeline.ErrEmptyStreamChunks) ||
		errors.Is(err, pipeline.ErrEmptyAggregatedBody) {
		return true
	}

	statusCode := ExtractStatusCodeFromError(err)
	switch {
	case statusCode == http.StatusBadRequest:
		return false
	case statusCode == http.StatusUnauthorized,
		statusCode == http.StatusPaymentRequired,
		statusCode == http.StatusForbidden,
		statusCode == http.StatusNotFound,
		statusCode == http.StatusRequestTimeout,
		statusCode == http.StatusTooManyRequests:
		return true
	case statusCode >= http.StatusInternalServerError:
		return true
	case statusCode > 0:
		return false
	default:
		return pipeline.IsUpstreamError(err)
	}
}

// ExtractStatusCodeFromError attempts to extract HTTP status code from various error types.
func ExtractStatusCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	var httpErr *httpclient.Error
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode
	}

	var llmErr *llm.ResponseError
	if errors.As(err, &llmErr) {
		return llmErr.StatusCode
	}

	return 0
}

func stickySessionEnabled(retryPolicy *biz.RetryPolicy) bool {
	return retryPolicy != nil && retryPolicy.LoadBalancerStrategy == biz.LoadBalancerStrategyStickySession
}
