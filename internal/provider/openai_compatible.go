package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// maxResponseBytes caps how much of a non-streaming provider response we read.
const maxResponseBytes = 16 << 20 // 16 MiB

// OpenAICompatible is the generic adapter for any provider speaking the OpenAI
// Chat Completions API (NVIDIA NIM, Groq, DeepSeek, OpenRouter, Together, Ollama,
// vLLM, LM Studio, ...). Differences are expressed via translate.Quirks + base URL.
type OpenAICompatible struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
	quirks  translate.Quirks
}

// NewOpenAICompatible builds the adapter. baseURL should include the API root
// (e.g. https://api.groq.com/openai/v1); "/chat/completions" is appended.
func NewOpenAICompatible(name, baseURL, apiKey string, client *http.Client, quirks translate.Quirks) *OpenAICompatible {
	return &OpenAICompatible{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  client,
		quirks:  quirks,
	}
}

// Name returns the adapter's configured provider name.
func (a *OpenAICompatible) Name() string { return a.name }

// Capabilities reports what this adapter supports.
func (a *OpenAICompatible) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Tools: true, Images: !a.quirks.DisableImages}
}

func (a *OpenAICompatible) newHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if a.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+a.apiKey)
	}
	return req, nil
}

// Complete performs a non-streaming completion.
func (a *OpenAICompatible) Complete(ctx context.Context, req *anthropic.Request, t Target) (*anthropic.Response, error) {
	oreq, err := translate.ToOpenAIRequest(req, t.Model, a.quirks)
	if err != nil {
		return nil, &Error{StatusCode: 400, Type: "invalid_request_error", Message: err.Error()}
	}
	oreq.Stream = false
	oreq.StreamOptions = nil

	body, err := json.Marshal(oreq)
	if err != nil {
		return nil, &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}
	httpReq, err := a.newHTTPRequest(ctx, body)
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
		return nil, httpError(resp.StatusCode, data)
	}

	var or openai.Response
	if err := json.Unmarshal(data, &or); err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: "invalid provider JSON: " + err.Error(), Retryable: true}
	}
	ar, err := translate.ToAnthropicResponse(&or, req.Model)
	if err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: err.Error(), Retryable: true}
	}
	return ar, nil
}

// httpError maps an upstream HTTP status + body to an Anthropic-typed provider error.
func httpError(status int, body []byte) *Error {
	e := &Error{StatusCode: status, Message: providerMessage(body, status)}
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

// providerMessage extracts a human message from an OpenAI-style error body.
func providerMessage(body []byte, status int) string {
	var env openai.ErrorEnvelope
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
