package translate

import (
	"encoding/json"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
)

func userText(s string) anthropic.Message {
	return anthropic.Message{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: s}}}
}

func TestToOpenAIRequest_SystemAndText(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-x",
		MaxTokens: 100,
		System:    json.RawMessage(`"be nice"`),
		Messages:  []anthropic.Message{userText("hi")},
	}
	o, err := ToOpenAIRequest(req, "gpt-test", Quirks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(o.Messages))
	}
	if o.Messages[0].Role != "system" || o.Messages[0].Content != "be nice" {
		t.Errorf("system message = %+v", o.Messages[0])
	}
	if o.Messages[1].Role != "user" || o.Messages[1].Content != "hi" {
		t.Errorf("user message = %+v", o.Messages[1])
	}
	if o.MaxTokens == nil || *o.MaxTokens != 100 {
		t.Errorf("max_tokens = %v, want 100", o.MaxTokens)
	}
	if o.Model != "gpt-test" {
		t.Errorf("model = %q", o.Model)
	}
}

func TestToOpenAIRequest_MaxCompletionTokensQuirk(t *testing.T) {
	req := &anthropic.Request{Model: "m", MaxTokens: 50, Messages: []anthropic.Message{userText("a")}}
	o, err := ToOpenAIRequest(req, "m", Quirks{UseMaxCompletionTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	if o.MaxCompletionTokens == nil || *o.MaxCompletionTokens != 50 {
		t.Errorf("max_completion_tokens = %v, want 50", o.MaxCompletionTokens)
	}
	if o.MaxTokens != nil {
		t.Errorf("max_tokens should be nil, got %v", *o.MaxTokens)
	}
}

func TestToOpenAIRequest_ToolUseAndResultRoundTrip(t *testing.T) {
	req := &anthropic.Request{
		Model:     "m",
		MaxTokens: 10,
		Messages: []anthropic.Message{
			{Role: "assistant", Content: []anthropic.ContentBlock{
				{Type: "tool_use", ID: "toolu_1", Name: "get_weather", Input: json.RawMessage(`{"city":"SF"}`)},
			}},
			{Role: "user", Content: []anthropic.ContentBlock{
				{Type: "tool_result", ToolUseID: "toolu_1", Content: json.RawMessage(`"18C"`)},
			}},
		},
	}
	o, err := ToOpenAIRequest(req, "m", Quirks{})
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Messages) != 2 {
		t.Fatalf("got %d messages, want 2", len(o.Messages))
	}
	asst := o.Messages[0]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Fatalf("assistant message = %+v", asst)
	}
	if asst.ToolCalls[0].ID != "toolu_1" || asst.ToolCalls[0].Function.Name != "get_weather" {
		t.Errorf("tool call = %+v", asst.ToolCalls[0])
	}
	toolMsg := o.Messages[1]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "toolu_1" || toolMsg.Content != "18C" {
		t.Errorf("tool result message = %+v", toolMsg)
	}
}

func TestToOpenAIRequest_ImageBecomesDataURL(t *testing.T) {
	req := &anthropic.Request{
		Model:     "m",
		MaxTokens: 10,
		Messages: []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{
			{Type: "image", Source: &anthropic.ImageSource{Type: "base64", MediaType: "image/png", Data: "AAAA"}},
		}}},
	}
	o, err := ToOpenAIRequest(req, "m", Quirks{})
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := o.Messages[0].Content.([]openai.ContentPart)
	if !ok || len(parts) != 1 {
		t.Fatalf("content = %#v", o.Messages[0].Content)
	}
	if parts[0].ImageURL == nil || parts[0].ImageURL.URL != "data:image/png;base64,AAAA" {
		t.Errorf("image url = %+v", parts[0].ImageURL)
	}
}

func TestConvertToolChoice(t *testing.T) {
	cases := map[string]string{
		`{"type":"auto"}`:               `"auto"`,
		`{"type":"any"}`:                `"required"`,
		`{"type":"none"}`:               `"none"`,
		`{"type":"tool","name":"getX"}`: `{"function":{"name":"getX"},"type":"function"}`,
	}
	for in, want := range cases {
		var tc anthropic.ToolChoice
		if err := json.Unmarshal([]byte(in), &tc); err != nil {
			t.Fatal(err)
		}
		got := convertToolChoice(&tc)
		if string(got) != want {
			t.Errorf("convertToolChoice(%s) = %s, want %s", in, got, want)
		}
	}
}
