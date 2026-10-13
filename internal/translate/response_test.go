package translate

import (
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/openai"
)

func sp(s string) *string { return &s }

func TestToAnthropicResponse_Text(t *testing.T) {
	content := "hello world"
	o := &openai.Response{
		Choices: []openai.Choice{{
			Message:      &openai.RespMessage{Role: "assistant", Content: &content},
			FinishReason: sp("stop"),
		}},
		Usage: &openai.Usage{PromptTokens: 3, CompletionTokens: 2},
	}
	a, err := ToAnthropicResponse(o, "claude-x")
	if err != nil {
		t.Fatal(err)
	}
	if a.Type != "message" || a.Role != "assistant" || a.Model != "claude-x" {
		t.Errorf("envelope = %+v", a)
	}
	if len(a.Content) != 1 || a.Content[0].Type != "text" || a.Content[0].Text != "hello world" {
		t.Errorf("content = %+v", a.Content)
	}
	if a.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", a.StopReason)
	}
	if a.Usage.InputTokens != 3 || a.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", a.Usage)
	}
}

func TestToAnthropicResponse_ToolUse(t *testing.T) {
	o := &openai.Response{
		Choices: []openai.Choice{{
			Message: &openai.RespMessage{
				Role: "assistant",
				ToolCalls: []openai.ToolCall{{
					ID:       "call_1",
					Type:     "function",
					Function: openai.FunctionCall{Name: "get_weather", Arguments: `{"city":"SF"}`},
				}},
			},
			FinishReason: sp("tool_calls"),
		}},
	}
	a, err := ToAnthropicResponse(o, "claude-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Content) != 1 || a.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", a.Content)
	}
	if a.Content[0].ID != "call_1" || a.Content[0].Name != "get_weather" || string(a.Content[0].Input) != `{"city":"SF"}` {
		t.Errorf("tool_use = %+v", a.Content[0])
	}
	if a.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", a.StopReason)
	}
}

func TestToAnthropicResponse_InvalidToolArgsBecomeEmptyObject(t *testing.T) {
	o := &openai.Response{Choices: []openai.Choice{{
		Message: &openai.RespMessage{Role: "assistant", ToolCalls: []openai.ToolCall{{
			ID: "c", Function: openai.FunctionCall{Name: "f", Arguments: "not json"},
		}}},
		FinishReason: sp("tool_calls"),
	}}}
	a, err := ToAnthropicResponse(o, "m")
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Content[0].Input) != "{}" {
		t.Errorf("invalid args should become {}, got %s", a.Content[0].Input)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := []struct {
		fr       string
		hasTools bool
		want     string
	}{
		{"stop", false, "end_turn"},
		{"length", false, "max_tokens"},
		{"tool_calls", true, "tool_use"},
		{"content_filter", false, "refusal"},
		{"stop", true, "tool_use"}, // tool calls present overrides
		{"weird", false, "end_turn"},
	}
	for _, c := range cases {
		got := MapStopReason(&c.fr, c.hasTools)
		if got != c.want {
			t.Errorf("MapStopReason(%q, %v) = %q, want %q", c.fr, c.hasTools, got, c.want)
		}
	}
	if got := MapStopReason(nil, false); got != "end_turn" {
		t.Errorf("nil finish_reason = %q, want end_turn", got)
	}
}
