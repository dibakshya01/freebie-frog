package security

import (
	"net/http"
	"time"
)

// NewHTTPClient builds an HTTP client for outbound provider calls: SSRF-guarded
// dialing and redirects disabled (so a redirect can't bounce to an internal host).
// No overall client timeout is set — streaming responses are long-lived and callers
// bound non-streaming calls with a context deadline instead.
func NewHTTPClient(policy DialPolicy) *http.Client {
	transport := &http.Transport{
		DialContext:           policy.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
