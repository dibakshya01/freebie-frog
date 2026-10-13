package provider

import (
	"context"
	"encoding/json"
	"io"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// Stream performs a streaming completion, translating the OpenAI SSE stream into
// Anthropic SSE events emitted via sink.
func (a *OpenAICompatible) Stream(ctx context.Context, req *anthropic.Request, t Target, sinkFn func(anthropic.StreamEvent) error) error {
	oreq, err := translate.ToOpenAIRequest(req, t.Model, a.quirks)
	if err != nil {
		return &Error{StatusCode: 400, Type: "invalid_request_error", Message: err.Error()}
	}
	oreq.Stream = true
	if a.quirks.StreamUsage {
		oreq.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}

	body, err := json.Marshal(oreq)
	if err != nil {
		return &Error{StatusCode: 500, Type: "api_error", Message: err.Error()}
	}
	httpReq, err := a.newHTTPRequest(ctx, body)
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
		return httpError(resp.StatusCode, data)
	}

	tr := translate.NewOpenAIStreamTranslator(req, req.Model)
	runErr := tr.Run(resp.Body, sinkFn)
	if runErr != nil && !tr.Started() {
		// Nothing emitted yet -> the router may fall back.
		return &Error{StatusCode: 502, Type: "api_error", Message: "upstream stream failed: " + runErr.Error(), Retryable: true}
	}
	return runErr
}
