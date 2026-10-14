package server_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/mockprovider"
	"github.com/dibakshya01/freebie-frog/internal/obs"
	"github.com/dibakshya01/freebie-frog/internal/router"
	"github.com/dibakshya01/freebie-frog/internal/secret"
	"github.com/dibakshya01/freebie-frog/internal/server"
)

func buildServer(t *testing.T, mock *mockprovider.Mock) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Providers = map[string]config.Provider{
		"mock": {
			Type:       "openai",
			BaseURL:    mock.URL(),
			APIKey:     "test-key",
			AllowLocal: true, // the mock listens on loopback; exercise the local opt-in
			Models:     []string{"test-model"},
		},
	}
	cfg.Models = config.ModelRouting{Default: []string{"mock/gpt-test"}}

	reg, err := router.Build(&cfg, secret.New())
	if err != nil {
		t.Fatalf("router.Build: %v", err)
	}
	log := obs.NewLogger("error", "text")
	deps := server.Deps{Registry: reg, Dispatcher: router.NewDispatcher(reg, log)}
	return server.New(cfg.Server, log, deps).Handler()
}

func TestMessages_NonStreaming(t *testing.T) {
	mock := mockprovider.New()
	defer mock.Close()
	ts := httptest.NewServer(buildServer(t, mock))
	defer ts.Close()

	body := `{"model":"claude-test","max_tokens":100,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d: %s", resp.StatusCode, b)
	}
	var msg anthropic.Response
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "message" || msg.Role != "assistant" {
		t.Errorf("envelope = %+v", msg)
	}
	if len(msg.Content) != 1 || msg.Content[0].Text != "Hello from the mock." {
		t.Errorf("content = %+v", msg.Content)
	}
	if msg.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", msg.StopReason)
	}
	// The downstream request should carry the routed provider model id.
	if lr := mock.LastRequest(); lr == nil || lr.Model != "gpt-test" {
		t.Errorf("provider saw model %v, want gpt-test", lr)
	}
}

func TestMessages_Streaming(t *testing.T) {
	mock := mockprovider.New()
	mock.Chunks = []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hello"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":" there"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}
	defer mock.Close()
	ts := httptest.NewServer(buildServer(t, mock))
	defer ts.Close()

	body := `{"model":"claude-test","max_tokens":100,"stream":true,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	raw, _ := io.ReadAll(resp.Body)
	out := string(raw)
	for _, want := range []string{"event: message_start", "event: content_block_start", "event: content_block_delta", "event: message_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("stream missing %q\n---\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Hello") || !strings.Contains(out, "there") {
		t.Errorf("stream missing text deltas:\n%s", out)
	}
}

func TestMessages_NoProvidersReturns503(t *testing.T) {
	log := obs.NewLogger("error", "text")
	h := server.New(config.Default().Server, log, server.Deps{}).Handler()
	ts := httptest.NewServer(h)
	defer ts.Close()

	body := `{"model":"claude-test","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestMessages_BadJSONReturns400(t *testing.T) {
	mock := mockprovider.New()
	defer mock.Close()
	ts := httptest.NewServer(buildServer(t, mock))
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", bytes.NewReader([]byte("{not json")))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestCountTokens(t *testing.T) {
	mock := mockprovider.New()
	defer mock.Close()
	ts := httptest.NewServer(buildServer(t, mock))
	defer ts.Close()

	body := `{"model":"claude-test","messages":[{"role":"user","content":"hello world this is a token count test"}]}`
	resp, err := http.Post(ts.URL+"/v1/messages/count_tokens", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		InputTokens int `json:"input_tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.InputTokens < 1 {
		t.Errorf("input_tokens = %d, want >= 1", out.InputTokens)
	}
}
