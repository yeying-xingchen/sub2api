package service

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamFailoverErrorIsTransientUpstream5xx(t *testing.T) {
	transient := []int{
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
		520, 521, 522, 523, 524,
	}
	for _, status := range transient {
		err := &UpstreamFailoverError{StatusCode: status}
		require.True(t, err.IsTransientUpstream5xx(), "status %d should be transient", status)
	}

	for _, status := range []int{0, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, 529} {
		err := &UpstreamFailoverError{StatusCode: status}
		require.False(t, err.IsTransientUpstream5xx(), "status %d should not be transient", status)
	}

	var nilErr *UpstreamFailoverError
	require.False(t, nilErr.IsTransientUpstream5xx())
}

func TestShouldRetryOnSameAccount(t *testing.T) {
	// 临时 5xx 即使没有 RetryableOnSameAccount 标记也应先同账号重试。
	require.True(t, (&UpstreamFailoverError{StatusCode: http.StatusBadGateway}).ShouldRetryOnSameAccount())

	// 显式标记的可重试错误保持原语义。
	require.True(t, (&UpstreamFailoverError{
		StatusCode:             http.StatusTooManyRequests,
		RetryableOnSameAccount: true,
	}).ShouldRetryOnSameAccount())

	// 非临时状态码且未显式标记时不重试。
	require.False(t, (&UpstreamFailoverError{StatusCode: http.StatusUnauthorized}).ShouldRetryOnSameAccount())

	var nilErr *UpstreamFailoverError
	require.False(t, nilErr.ShouldRetryOnSameAccount())
}

func TestShouldFailoverOpenAIPassthroughResponseTransient5xxForOAuth(t *testing.T) {
	svc := &OpenAIGatewayService{}
	oauth := &Account{Type: AccountTypeOAuth}
	body := []byte(`{"error":{"message":"upstream unavailable"}}`)

	for _, status := range []int{500, 502, 503, 504, 520, 521, 522, 523, 524} {
		require.True(t, shouldFailoverOpenAIPassthroughResponse(oauth, status, body),
			"OAuth status %d should trigger same-account retry/failover", status)
		require.True(t, svc.shouldFailoverOpenAIUpstreamResponse(oauth, status, "upstream unavailable", body))
	}

	// 非临时请求错误与 cyber_policy 仍不换号。
	require.False(t, shouldFailoverOpenAIPassthroughResponse(oauth, http.StatusBadRequest, body))
	require.False(t, shouldFailoverOpenAIPassthroughResponse(oauth, http.StatusBadGateway,
		[]byte(`{"error":{"code":"cyber_policy","message":"blocked"}}`)))
}
