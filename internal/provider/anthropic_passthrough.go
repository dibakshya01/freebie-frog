package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
)

// defaultAnthropicBaseURL is the public Anthropic Messages API root.
const defaultAnthropicBaseURL = "https://api.anthropic.com"

// anthropicVersion is the required anthropic-version header value.
const anthropicVersion = "2023-06-01"

// PassthroughAdapter forwards to a real Anthropic-compatible upstream with no wire
// translation: the request is re-marshaled (only the model id is rewritten to the
// routed target) and the upstream's response / SSE stream is relayed faithfully.
type PassthroughAdapter struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewPassthroughAdapter builds the adapter. An empty baseURL defaults to the public
// Anthropic API; the injected client should be SSRF-guarded by the caller.
func NewPassthroughAdapter(name, baseURL, apiKey string, client *http.Client) *PassthroughAdapter {
	if baseURL == "" {
		baseURL = defaultAnthropicBaseURL
	}
	return &PassthroughAdapter{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client:  client,
	}
}

// Name returns the adapter's configured provider name.
func (a *PassthroughAdapter) Name() string { return a.name }

// Capabilities reports what this adapter supports.
func (a *PassthroughAdapter) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Tools: true, Images: true}
}

func (a *PassthroughAdapter) newHTTPRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("anthropic-version", anthropicVersion)
	if a.apiKey != "" {
		req.Header.Set("x-api-key", a.apiKey)
	}
	return req, nil
}

// marshalWithModel re-marshals the Anthropic request, overriding the model id with
// the routed target and forcing the stream flag.
func marshalWithModel(req *anthropic.Request, model string, stream bool) ([]byte, error) {
	cp := *req
	cp.Model = model
	cp.Stream = stream
	return json.Marshal(&cp)
}

// Complete forwards a non-streaming request and parses the Anthropic response.
func (a *PassthroughAdapter) Complete(ctx context.Context, req *anthropic.Request, t Target) (*anthropic.Response, error) {
	body, err := marshalWithModel(req, t.Model, false)
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
		return nil, anthropicHTTPError(resp.StatusCode, data)
	}

	var ar anthropic.Response
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, &Error{StatusCode: 502, Type: "api_error", Message: "invalid provider JSON: " + err.Error(), Retryable: true}
	}
	// Echo the model the client requested.
	ar.Model = req.Model
	return &ar, nil
}

// Stream forwards a streaming request and relays the upstream Anthropic SSE stream
// verbatim: each event:/data: pair is re-emitted as an anthropic.StreamEvent with the
// data as a json.RawMessage payload. An error is returned only before the first event.
func (a *PassthroughAdapter) Stream(ctx context.Context, req *anthropic.Request, t Target, sinkFn func(anthropic.StreamEvent) error) error {
	body, err := marshalWithModel(req, t.Model, true)
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

	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return anthropicHTTPError(resp.StatusCode, data)
	}

	return relayAnthropicSSE(resp.Body, sinkFn)
}

// relayAnthropicSSE parses an Anthropic SSE body (event: / data: pairs separated by
// blank lines) and re-emits each event faithfully. It returns an error only if the
// stream fails before the first event is emitted (so the caller may fall back).
func relayAnthropicSSE(r io.Reader, out func(anthropic.StreamEvent) error) error {
	reader := bufio.NewReaderSize(r, 1<<16)
	emitted := false
	var eventName string
	var dataBuf bytes.Buffer

	dispatch := func() error {
		if dataBuf.Len() == 0 && eventName == "" {
			return nil
		}
		name := eventName
		if name == "" {
			name = "message"
		}
		ev := anthropic.StreamEvent{Event: name, Payload: json.RawMessage(append([]byte(nil), dataBuf.Bytes()...))}
		eventName = ""
		dataBuf.Reset()
		if err := out(ev); err != nil {
			return err
		}
		emitted = true
		return nil
	}

	for {
		line, err := reader.ReadBytes('\n')
		trimmed := bytes.TrimRight(line, "\r\n")

		if len(trimmed) == 0 {
			// Blank line -> dispatch the accumulated event.
			if derr := dispatch(); derr != nil {
				return derr
			}
		} else if trimmed[0] == ':' {
			// SSE comment / keep-alive -> ignore.
		} else if field, value, ok := cutSSEField(trimmed); ok {
			switch field {
			case "event":
				eventName = value
			case "data":
				if dataBuf.Len() > 0 {
					dataBuf.WriteByte('\n')
				}
				dataBuf.WriteString(value)
			}
		}

		if err != nil {
			if err == io.EOF {
				if derr := dispatch(); derr != nil {
					return derr
				}
				break
			}
			if emitted {
				return nil // already streaming; nothing more to send
			}
			return &Error{StatusCode: 502, Type: "api_error", Message: "upstream stream failed: " + err.Error(), Retryable: true}
		}
	}

	if !emitted {
		return &Error{StatusCode: 502, Type: "api_error", Message: "empty provider stream", Retryable: true}
	}
	return nil
}

// cutSSEField splits an SSE line into field name and value, trimming one optional
// leading space from the value per the SSE spec.
func cutSSEField(line []byte) (string, string, bool) {
	name, val, found := bytes.Cut(line, []byte(":"))
	if !found {
		return string(line), "", true
	}
	v := val
	if len(v) > 0 && v[0] == ' ' {
		v = v[1:]
	}
	return string(name), string(v), true
}

// anthropicHTTPError maps an upstream HTTP status + Anthropic error body to a typed
// provider error, marking 429/5xx retryable.
func anthropicHTTPError(status int, body []byte) *Error {
	e := &Error{StatusCode: status, Message: anthropicMessage(body, status)}
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

// anthropicMessage extracts a human message from an Anthropic error envelope.
func anthropicMessage(body []byte, status int) string {
	var env anthropic.ErrorResponse
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
