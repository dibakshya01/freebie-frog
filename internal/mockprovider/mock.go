// Package mockprovider is an in-process OpenAI-compatible server for tests and
// end-to-end verification without network access or real API keys.
package mockprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/dibakshya01/freebie-frog/internal/openai"
)

// Mock is a configurable fake OpenAI Chat Completions server.
type Mock struct {
	Server *httptest.Server

	// StatusCode, when >= 400, makes the mock return an error with ErrorBody.
	StatusCode int
	ErrorBody  string
	// Response is the non-streaming body (if nil, a default text reply is returned).
	Response *openai.Response
	// Chunks are raw JSON payloads emitted as SSE "data:" lines for streaming
	// requests; "[DONE]" is appended automatically.
	Chunks []string

	mu          sync.Mutex
	lastRequest *openai.Request
	requests    int
}

// New starts a Mock listening on loopback.
func New() *Mock {
	m := &Mock{StatusCode: 200}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL returns the API root to configure as a provider base_url.
func (m *Mock) URL() string { return m.Server.URL + "/v1" }

// Close shuts the mock down.
func (m *Mock) Close() { m.Server.Close() }

// LastRequest returns the most recent decoded request (nil if none).
func (m *Mock) LastRequest() *openai.Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// Requests returns how many requests the mock has served.
func (m *Mock) Requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

func (m *Mock) handle(w http.ResponseWriter, r *http.Request) {
	var req openai.Request
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	m.lastRequest = &req
	m.requests++
	m.mu.Unlock()

	if m.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.StatusCode)
		if m.ErrorBody != "" {
			fmt.Fprint(w, m.ErrorBody)
		} else {
			fmt.Fprintf(w, `{"error":{"message":%q,"type":"error"}}`, http.StatusText(m.StatusCode))
		}
		return
	}

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, c := range m.Chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if fl != nil {
				fl.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if m.Response != nil {
		_ = json.NewEncoder(w).Encode(m.Response)
		return
	}
	content := "Hello from the mock."
	_ = json.NewEncoder(w).Encode(openai.Response{
		ID:     "chatcmpl-mock",
		Object: "chat.completion",
		Model:  req.Model,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      &openai.RespMessage{Role: "assistant", Content: &content},
			FinishReason: strptr("stop"),
		}},
		Usage: &openai.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	})
}

func strptr(s string) *string { return &s }
