package provider_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/gemini"
	"github.com/dibakshya01/freebie-frog/internal/mockprovider"
	"github.com/dibakshya01/freebie-frog/internal/provider"
	"github.com/dibakshya01/freebie-frog/internal/security"
)

func geminiAdapter(t *testing.T, mock *mockprovider.GeminiMock) *provider.GeminiAdapter {
	t.Helper()
	// An SSRF-guarded client that permits the loopback mock.
	policy := security.DialPolicy{AllowHosts: map[string]bool{
		"127.0.0.1": true, "localhost": true, "::1": true,
	}}
	client := security.NewHTTPClient(policy)
	return provider.NewGeminiAdapter("gemini", mock.URL(), "test-key", client)
}

func TestGeminiAdapter_CompleteText(t *testing.T) {
	mock := mockprovider.NewGemini()
	defer mock.Close()
	ad := geminiAdapter(t, mock)

	req := &anthropic.Request{Model: "claude-x", MaxTokens: 100, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "hi"}}},
	}}
	resp, err := ad.Complete(context.Background(), req, provider.Target{Provider: "gemini", Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Text != "Hello from the Gemini mock." {
		t.Errorf("content = %+v", resp.Content)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q", resp.StopReason)
	}
	if resp.Model != "claude-x" {
		t.Errorf("model echoed = %q, want claude-x", resp.Model)
	}
	// The downstream call must hit :generateContent with the routed model + api key.
	if lp := mock.LastPath(); !strings.Contains(lp, "/models/gemini-test:generateContent") {
		t.Errorf("path = %q", lp)
	}
	if mock.LastAPIKey() != "test-key" {
		t.Errorf("x-goog-api-key = %q, want test-key", mock.LastAPIKey())
	}
}

func TestGeminiAdapter_CompleteFunctionCallAndSchemaPassthrough(t *testing.T) {
	mock := mockprovider.NewGemini()
	mock.Response = &gemini.GenerateContentResponse{
		Candidates: []gemini.Candidate{{
			Content: gemini.Content{Role: "model", Parts: []gemini.Part{{
				FunctionCall: &gemini.FunctionCall{Name: "get_weather", Args: json.RawMessage(`{"city":"SF"}`)},
			}}},
			FinishReason: "STOP",
		}},
	}
	defer mock.Close()
	ad := geminiAdapter(t, mock)

	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`)
	req := &anthropic.Request{
		Model: "claude-x", MaxTokens: 100,
		Messages: []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "weather?"}}}},
		Tools:    []anthropic.Tool{{Name: "get_weather", InputSchema: schema}},
	}
	resp, err := ad.Complete(context.Background(), req, provider.Target{Model: "gemini-test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Content) != 1 || resp.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", resp.Content)
	}
	if resp.Content[0].Name != "get_weather" || string(resp.Content[0].Input) != `{"city":"SF"}` {
		t.Errorf("tool_use = %+v", resp.Content[0])
	}
	if resp.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", resp.StopReason)
	}
	// Verify the schema reached the provider verbatim via parametersJsonSchema.
	lr := mock.LastRequest()
	if lr == nil || len(lr.Tools) != 1 || len(lr.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("provider saw tools = %+v", lr)
	}
	if string(lr.Tools[0].FunctionDeclarations[0].ParametersJsonSchema) != string(schema) {
		t.Errorf("parametersJsonSchema = %s, want %s", lr.Tools[0].FunctionDeclarations[0].ParametersJsonSchema, schema)
	}
}

func TestGeminiAdapter_StreamText(t *testing.T) {
	mock := mockprovider.NewGemini()
	mock.Chunks = []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hello"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":" there"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}],"usageMetadata":{"candidatesTokenCount":2}}`,
	}
	defer mock.Close()
	ad := geminiAdapter(t, mock)

	req := &anthropic.Request{Model: "claude-x", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "hi"}}},
	}}
	var events []anthropic.StreamEvent
	err := ad.Stream(context.Background(), req, provider.Target{Model: "gemini-test"}, func(ev anthropic.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var text strings.Builder
	for _, e := range events {
		if d, ok := e.Payload.(anthropic.ContentBlockDelta); ok && d.Delta.Type == "text_delta" {
			text.WriteString(d.Delta.Text)
		}
	}
	if text.String() != "Hello there" {
		t.Errorf("streamed text = %q", text.String())
	}
	if events[0].Event != "message_start" || events[len(events)-1].Event != "message_stop" {
		t.Errorf("event bookends = %q .. %q", events[0].Event, events[len(events)-1].Event)
	}
	if lp := mock.LastPath(); !strings.Contains(lp, ":streamGenerateContent") || !strings.Contains(lp, "alt=sse") {
		t.Errorf("stream path = %q, want :streamGenerateContent?alt=sse", lp)
	}
}

func TestGeminiAdapter_StreamFunctionCall(t *testing.T) {
	mock := mockprovider.NewGemini()
	mock.Chunks = []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
	}
	defer mock.Close()
	ad := geminiAdapter(t, mock)

	req := &anthropic.Request{Model: "claude-x", MaxTokens: 100, Stream: true, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "weather?"}}},
	}}
	var events []anthropic.StreamEvent
	err := ad.Stream(context.Background(), req, provider.Target{Model: "gemini-test"}, func(ev anthropic.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var argJSON strings.Builder
	sawToolStart := false
	for _, e := range events {
		switch p := e.Payload.(type) {
		case anthropic.ContentBlockStart:
			if p.ContentBlock.Type == "tool_use" && p.ContentBlock.Name == "get_weather" {
				sawToolStart = true
			}
		case anthropic.ContentBlockDelta:
			if p.Delta.Type == "input_json_delta" {
				argJSON.WriteString(p.Delta.PartialJSON)
			}
		}
	}
	if !sawToolStart {
		t.Fatal("no tool_use content_block_start")
	}
	if argJSON.String() != `{"city":"SF"}` {
		t.Errorf("tool args = %q", argJSON.String())
	}
}

func TestGeminiAdapter_RetryableOn429(t *testing.T) {
	mock := mockprovider.NewGemini()
	mock.StatusCode = 429
	defer mock.Close()
	ad := geminiAdapter(t, mock)

	req := &anthropic.Request{Model: "claude-x", MaxTokens: 10, Messages: []anthropic.Message{
		{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "hi"}}},
	}}
	_, err := ad.Complete(context.Background(), req, provider.Target{Model: "gemini-test"})
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !provider.IsRetryable(err) {
		t.Errorf("429 error should be retryable: %v", err)
	}
	pe, ok := err.(*provider.Error)
	if !ok || pe.StatusCode != 429 || pe.Type != "rate_limit_error" {
		t.Errorf("error = %+v", err)
	}
}
