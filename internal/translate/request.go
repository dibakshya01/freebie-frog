package translate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
)

// ToOpenAIRequest converts an Anthropic request into an OpenAI Chat Completions
// request targeting the given provider model id, applying provider quirks.
func ToOpenAIRequest(a *anthropic.Request, modelID string, q Quirks) (*openai.Request, error) {
	o := &openai.Request{Model: modelID, Stream: a.Stream}

	var msgs []openai.Message
	if sys := anthropic.SystemText(a.System); sys != "" {
		msgs = append(msgs, openai.Message{Role: "system", Content: sys})
	}
	for _, m := range a.Messages {
		converted, err := convertMessage(m, q)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, converted...)
	}
	o.Messages = msgs

	maxTok := a.MaxTokens
	if maxTok > 0 {
		if q.UseMaxCompletionTokens {
			o.MaxCompletionTokens = &maxTok
		} else {
			o.MaxTokens = &maxTok
		}
	}
	o.Temperature = a.Temperature
	o.TopP = a.TopP
	if len(a.StopSequences) > 0 {
		o.Stop = a.StopSequences
	}

	for _, t := range a.Tools {
		o.Tools = append(o.Tools, openai.Tool{
			Type: "function",
			Function: openai.ToolFunction{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  t.InputSchema,
			},
		})
	}
	if a.ToolChoice != nil {
		if tc := convertToolChoice(a.ToolChoice); tc != nil {
			o.ToolChoice = tc
		}
	}
	if a.Stream && q.StreamUsage {
		o.StreamOptions = &openai.StreamOptions{IncludeUsage: true}
	}
	return o, nil
}

// convertMessage maps one Anthropic message to one or more OpenAI messages.
// tool_result blocks become separate role:"tool" messages.
func convertMessage(m anthropic.Message, q Quirks) ([]openai.Message, error) {
	switch m.Role {
	case "assistant":
		return convertAssistantMessage(m)
	default: // user, system-in-messages, or anything else -> treat as user side
		return convertUserMessage(m, q)
	}
}

func convertUserMessage(m anthropic.Message, q Quirks) ([]openai.Message, error) {
	var out []openai.Message
	var parts []openai.ContentPart
	var text strings.Builder
	hasImage := false

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(b.Text)
			parts = append(parts, openai.ContentPart{Type: "text", Text: b.Text})
		case "image":
			if q.DisableImages {
				continue
			}
			url, err := imageToDataURL(b.Source)
			if err != nil {
				return nil, err
			}
			hasImage = true
			parts = append(parts, openai.ContentPart{Type: "image_url", ImageURL: &openai.ImageURL{URL: url}})
		case "tool_result":
			out = append(out, openai.Message{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    toolResultText(b.Content),
			})
		}
	}

	if len(parts) > 0 {
		var content any
		if hasImage {
			content = parts
		} else {
			content = text.String()
		}
		out = append(out, openai.Message{Role: "user", Content: content})
	}
	return out, nil
}

func convertAssistantMessage(m anthropic.Message) ([]openai.Message, error) {
	msg := openai.Message{Role: "assistant"}
	var text strings.Builder
	var toolCalls []openai.ToolCall

	for _, b := range m.Content {
		switch b.Type {
		case "text":
			text.WriteString(b.Text)
		case "tool_use":
			args := string(b.Input)
			if strings.TrimSpace(args) == "" {
				args = "{}"
			}
			toolCalls = append(toolCalls, openai.ToolCall{
				ID:       b.ID,
				Type:     "function",
				Function: openai.FunctionCall{Name: b.Name, Arguments: args},
			})
		case "thinking", "redacted_thinking":
			// No OpenAI equivalent; dropped (documented in CAPABILITIES.md).
		}
	}
	if text.Len() > 0 {
		msg.Content = text.String()
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	return []openai.Message{msg}, nil
}

func imageToDataURL(s *anthropic.ImageSource) (string, error) {
	if s == nil {
		return "", fmt.Errorf("image block missing source")
	}
	switch s.Type {
	case "base64":
		if s.MediaType == "" || s.Data == "" {
			return "", fmt.Errorf("base64 image missing media_type or data")
		}
		return fmt.Sprintf("data:%s;base64,%s", s.MediaType, s.Data), nil
	case "url":
		return s.URL, nil
	default:
		return "", fmt.Errorf("unsupported image source type %q", s.Type)
	}
}

// toolResultText flattens an Anthropic tool_result content (string or blocks) to a
// plain string for an OpenAI tool message.
func toolResultText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return trimmed
	}
	var blocks []anthropic.ContentBlock
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var sb strings.Builder
		for _, b := range blocks {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return trimmed
}

func convertToolChoice(tc *anthropic.ToolChoice) json.RawMessage {
	switch tc.Type {
	case "auto":
		return json.RawMessage(`"auto"`)
	case "any":
		return json.RawMessage(`"required"`)
	case "none":
		return json.RawMessage(`"none"`)
	case "tool":
		if tc.Name == "" {
			return json.RawMessage(`"required"`)
		}
		b, _ := json.Marshal(map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		})
		return b
	}
	return nil
}
