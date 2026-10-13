package provider_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/provider"
	"github.com/dibakshya01/freebie-frog/internal/security"
)

func passthroughClient() *http.Client {
	policy := security.DialPolicy{AllowHosts: map[string]bool{
		"127.0.0.1": true, "localhost": true, "::1": true,
	}}
	return security.NewHTTPClient(policy)
}

func TestPassthroughAdapter_Complete(t *testing.T) {
	var gotPath, gotKey, gotVersion string
	var gotBody anthropic.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(anthropic.Response{
			ID: "msg_up", Type: "message", Role: "assistant", Model: "claude-upstream",
			Content:    []anthropic.ContentBlock{{Type: "text", Text: "pong"}},
			StopReason: "end_turn",
			Usage:      anthropic.Usage{InputTokens: 4, OutputTokens: 2},
		})
	}))
	defer srv.Close()

	ad := provider.NewPassthroughAdapter("anthropic", srv.URL, "sk-test", passthroughClient())
	req := &anthropic.Request{Model: "claude-in", MaxTokens: 50, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "ping"}}},
	}}
	resp, err := ad.Complete(context.Background(), req, provider.Target{Model: "claude-upstream"})
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q, want /v1/messages", gotPath)
	}
	if gotKey != "sk-test" || gotVersion != "2023-06-01" {
		t.Errorf("auth headers: x-api-key=%q anthropic-version=%q", gotKey, gotVersion)
	}
	// Model is rewritten to the routed target on the wire, and stream is false.
	if gotBody.Model != "claude-upstream" || gotBody.Stream {
		t.Errorf("forwarded request = %+v", gotBody)
	}
	// Response is parsed and the client-facing model id is echoed back.
	if len(resp.Content) != 1 || resp.Content[0].Text != "pong" {
		t.Errorf("content = %+v", resp.Content)
	}
	if resp.Model != "claude-in" {
		t.Errorf("model echoed = %q, want claude-in", resp.Model)
	}
}

func TestPassthroughAdapter_StreamFaithfulRelay(t *testing.T) {
	// The upstream Anthropic SSE sequence that must be relayed verbatim.
	upstream := strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-upstream","content":[],"usage":{"input_tokens":4,"output_tokens":0}}}`,
		``,
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		``,
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		``,
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		``,
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		``,
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		``,
	}, "\n")

	var gotStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body anthropic.Request
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotStream = body.Stream
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, upstream)
	}))
	defer srv.Close()

	ad := provider.NewPassthroughAdapter("anthropic", srv.URL, "sk-test", passthroughClient())
	req := &anthropic.Request{Model: "claude-in", MaxTokens: 50, Stream: true, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "ping"}}},
	}}

	var events []anthropic.StreamEvent
	err := ad.Stream(context.Background(), req, provider.Target{Model: "claude-upstream"}, func(ev anthropic.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !gotStream {
		t.Error("upstream request should have stream:true")
	}

	gotNames := make([]string, len(events))
	for i, e := range events {
		gotNames[i] = e.Event
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if strings.Join(gotNames, ",") != strings.Join(want, ",") {
		t.Fatalf("event names = %v, want %v", gotNames, want)
	}

	// Payloads must be relayed as raw JSON verbatim.
	raw, ok := events[2].Payload.(json.RawMessage)
	if !ok {
		t.Fatalf("payload type = %T, want json.RawMessage", events[2].Payload)
	}
	var delta anthropic.ContentBlockDelta
	if err := json.Unmarshal(raw, &delta); err != nil {
		t.Fatal(err)
	}
	if delta.Delta.Type != "text_delta" || delta.Delta.Text != "hi" {
		t.Errorf("relayed delta = %+v", delta)
	}
}

func TestPassthroughAdapter_ErrorBeforeFirstEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	defer srv.Close()

	ad := provider.NewPassthroughAdapter("anthropic", srv.URL, "sk-test", passthroughClient())
	req := &anthropic.Request{Model: "claude-in", MaxTokens: 50, Stream: true, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "ping"}}},
	}}
	called := false
	err := ad.Stream(context.Background(), req, provider.Target{Model: "claude-upstream"}, func(anthropic.StreamEvent) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("expected an error before the first event (so the router can fall back)")
	}
	if called {
		t.Error("no event should have been emitted on a 429")
	}
	if !provider.IsRetryable(err) {
		t.Errorf("429 error should be retryable: %v", err)
	}
	pe, ok := err.(*provider.Error)
	if !ok || pe.StatusCode != 429 || pe.Message != "slow down" {
		t.Errorf("error = %+v", err)
	}
}
