package translate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
)

// ToAnthropicResponse converts a non-streaming OpenAI response into an Anthropic
// Message. The reported model name is echoed back as the client requested it.
func ToAnthropicResponse(o *openai.Response, requestedModel string) (*anthropic.Response, error) {
	if len(o.Choices) == 0 {
		return nil, fmt.Errorf("provider response has no choices")
	}
	ch := o.Choices[0]
	if ch.Message == nil {
		return nil, fmt.Errorf("provider response choice has no message")
	}

	var content []anthropic.ContentBlock
	if ch.Message.Content != nil && *ch.Message.Content != "" {
		content = append(content, anthropic.ContentBlock{Type: "text", Text: *ch.Message.Content})
	}
	for _, tc := range ch.Message.ToolCalls {
		input := json.RawMessage(strings.TrimSpace(tc.Function.Arguments))
		if len(input) == 0 || !json.Valid(input) {
			input = json.RawMessage("{}")
		}
		id := tc.ID
		if id == "" {
			id = newToolUseID()
		}
		name := tc.Function.Name
		if name == "" {
			name = "unknown"
		}
		content = append(content, anthropic.ContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: input,
		})
	}
	if content == nil {
		content = []anthropic.ContentBlock{}
	}

	resp := &anthropic.Response{
		ID:         anthropic.NewMessageID(),
		Type:       "message",
		Role:       "assistant",
		Model:      requestedModel,
		Content:    content,
		StopReason: MapStopReason(ch.FinishReason, len(ch.Message.ToolCalls) > 0),
	}
	if o.Usage != nil {
		resp.Usage = anthropic.Usage{
			InputTokens:  o.Usage.PromptTokens,
			OutputTokens: o.Usage.CompletionTokens,
		}
	}
	return resp, nil
}

// MapStopReason maps an OpenAI finish_reason to an Anthropic stop_reason. The
// presence of tool calls is authoritative for a tool stop (providers vary on the
// finish_reason string).
func MapStopReason(finishReason *string, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_use"
	}
	if finishReason == nil {
		return "end_turn"
	}
	switch *finishReason {
	case "stop":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	case "content_filter":
		return "refusal"
	default:
		return "end_turn"
	}
}
