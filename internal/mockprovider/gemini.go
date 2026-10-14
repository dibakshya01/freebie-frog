package mockprovider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"

	"github.com/dibakshya01/freebie-frog/internal/gemini"
)

// GeminiMock is a configurable fake Google Generative Language API (Gemini) server
// for tests and end-to-end verification without network access or real API keys.
type GeminiMock struct {
	Server *httptest.Server

	// StatusCode, when >= 400, makes the mock return an error with ErrorBody.
	StatusCode int
	ErrorBody  string
	// Response is the non-streaming body (if nil, a default text reply is returned).
	Response *gemini.GenerateContentResponse
	// Chunks are raw JSON payloads emitted as SSE "data:" lines for streaming
	// requests. Unlike OpenAI, Gemini sends NO "[DONE]" sentinel.
	Chunks []string

	mu          sync.Mutex
	lastRequest *gemini.GenerateContentRequest
	lastAPIKey  string
	lastPath    string
	requests    int
}

// NewGemini starts a GeminiMock listening on loopback.
func NewGemini() *GeminiMock {
	m := &GeminiMock{StatusCode: 200}
	m.Server = httptest.NewServer(http.HandlerFunc(m.handle))
	return m
}

// URL returns the v1beta API root to configure as a provider base_url.
func (m *GeminiMock) URL() string { return m.Server.URL + "/v1beta" }

// Close shuts the mock down.
func (m *GeminiMock) Close() { m.Server.Close() }

// LastRequest returns the most recent decoded request (nil if none).
func (m *GeminiMock) LastRequest() *gemini.GenerateContentRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRequest
}

// LastAPIKey returns the x-goog-api-key header of the most recent request.
func (m *GeminiMock) LastAPIKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastAPIKey
}

// LastPath returns the request path + method of the most recent request (so tests can
// assert generateContent vs streamGenerateContent?alt=sse).
func (m *GeminiMock) LastPath() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastPath
}

// Requests returns how many requests the mock has served.
func (m *GeminiMock) Requests() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.requests
}

func (m *GeminiMock) handle(w http.ResponseWriter, r *http.Request) {
	var req gemini.GenerateContentRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	m.mu.Lock()
	m.lastRequest = &req
	m.lastAPIKey = r.Header.Get("x-goog-api-key")
	m.lastPath = path
	m.requests++
	m.mu.Unlock()

	if m.StatusCode >= 400 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.StatusCode)
		if m.ErrorBody != "" {
			fmt.Fprint(w, m.ErrorBody)
		} else {
			fmt.Fprintf(w, `{"error":{"code":%d,"message":%q,"status":"ERROR"}}`, m.StatusCode, http.StatusText(m.StatusCode))
		}
		return
	}

	// Streaming requests carry alt=sse.
	if r.URL.Query().Get("alt") == "sse" {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, c := range m.Chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
			if fl != nil {
				fl.Flush()
			}
		}
		// Gemini sends no [DONE] sentinel; the stream simply ends.
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if m.Response != nil {
		_ = json.NewEncoder(w).Encode(m.Response)
		return
	}
	_ = json.NewEncoder(w).Encode(gemini.GenerateContentResponse{
		Candidates: []gemini.Candidate{{
			Content:      gemini.Content{Role: "model", Parts: []gemini.Part{{Text: "Hello from the Gemini mock."}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: &gemini.UsageMetadata{PromptTokenCount: 10, CandidatesTokenCount: 5, TotalTokenCount: 15},
	})
}
