package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/gemini"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// defaultGeminiBaseURL is the Generative Language API v1beta root.
const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// GeminiAdapter routes to the Google Generative Language API (Gemini) natively,
// translating Anthropic requests to/from the Gemini wire format. The model id is
// carried in the URL path and auth is the x-goog-api-key header.
type GeminiAdapter struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewGeminiAdapter builds the adapter. An empty baseURL defaults to the public
// v1beta endpoint; the injected client should be SSRF-guarded by the caller.
func NewGeminiAdapter(name, baseURL, apiKey string, client *http.Client) *GeminiAdapter {
	if baseURL == "" {
		baseURL = defaultGeminiBaseURL
	}
	return &GeminiAdapter{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  client,
	}
}

// Name returns the adapter's configured provider name.
func (a *GeminiAdapter) Name() string { return a.name }

// Capabilities reports what this adapter supports.
func (a *GeminiAdapter) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Tools: true, Images: true}
}

// endpoint builds {baseURL}/models/{model}:{method}{query}. The method separator ":"
// must not be percent-encoded, so only the model segment is escaped.
func (a *GeminiAdapter) endpoint(model, method, query string) string {
	u := a.baseURL + "/models/" + url.PathEscape(model) + ":" + method
	if query != "" {
		u += "?" + query
	}
	return u
}

func (a *GeminiAdapter) newHTTPRequest(ctx context.Context, endpoint string, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("x-goog-api-key", a.apiKey)
	}
	return req, nil
}

// Complete performs a non-streaming generation.
func (a *GeminiAdapter) Complete(ctx context.Context, req *anthropic.Request, t Target) (*anthropic.Response, error) {
	greq, err := translate.ToGeminiRequest(req, t.Model)
	if err != nil {
		return nil, &Error{StatusCode: 400, Type: "invalid_request_error", Message: err.Error()}
	}
	body, err := json.Marshal(greq)
	if err != nil {
		return nil, &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}
	httpReq, err := a.newHTTPRequest(ctx, a.endpoint(t.Model, "generateContent", ""), body)
	if err != nil {
		return nil, &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: "upstream request failed: " + err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode >= 400 {
		return nil, geminiHTTPError(resp.StatusCode, data)
	}

	var gr gemini.GenerateContentResponse
	if err := json.Unmarshal(data, &gr); err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: "invalid provider JSON: " + err.Error(), Retryable: true}
	}
	ar, err := translate.ToAnthropicResponseFromGemini(&gr, req.Model)
	if err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: err.Error(), Retryable: true}
	}
	return ar, nil
}

// Stream performs a streaming generation, translating the Gemini SSE stream into
// Anthropic SSE events emitted via sink.
func (a *GeminiAdapter) Stream(ctx context.Context, req *anthropic.Request, t Target, sinkFn func(anthropic.StreamEvent) error) error {
	greq, err := translate.ToGeminiRequest(req, t.Model)
	if err != nil {
		return &Error{StatusCode: 400, Type: "invalid_request_error", Message: err.Error()}
	}
	body, err := json.Marshal(greq)
	if err != nil {
		return &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}
	httpReq, err := a.newHTTPRequest(ctx, a.endpoint(t.Model, "streamGenerateContent", "alt=sse"), body)
	if err != nil {
		return &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return &Error{StatusCode: 502, Type: "api_error", Message: "upstream request failed: " + err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	// Status is known before any event is emitted -> safe to fall back.
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return geminiHTTPError(resp.StatusCode, data)
	}

	tr := translate.NewGeminiStreamTranslator(req, req.Model)
	runErr := tr.Run(resp.Body, sinkFn)
	if runErr != nil && !tr.Started() {
		// Nothing emitted yet -> the router may fall back.
		return &Error{StatusCode: 502, Type: "api_error", Message: "upstream stream failed: " + runErr.Error(), Retryable: true}
	}
	return runErr
}

// geminiHTTPError maps an upstream HTTP status + Google error body to an
// Anthropic-typed provider error, marking 429/5xx retryable.
func geminiHTTPError(status int, body []byte) *Error {
	e := &Error{StatusCode: status, Message: geminiMessage(body, status)}
	switch {
	case status == 429:
		e.Type, e.Retryable = "rate_limit_error", true
	case status == 401 || status == 403:
		e.Type = "authentication_error"
	case status == 400 || status == 422:
		e.Type = "invalid_request_error"
	case status >= 500:
		e.Type, e.Retryable = "api_error", true
	default:
		e.Type = "api_error"
	}
	return e
}

// geminiMessage extracts a human message from a Google API error body.
func geminiMessage(body []byte, status int) string {
	var env gemini.ErrorEnvelope
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Message != "" {
		return env.Error.Message
	}
	s := strings.TrimSpace(string(body))
	if s == "" {
		return http.StatusText(status)
	}
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
