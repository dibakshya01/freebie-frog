// Package anthropic holds the Anthropic Messages API wire types that freebie-frog
// speaks on its ingress side. Built from the public API specification only
// (see docs/SOURCES.md).
package anthropic

import (
	"encoding/json"
	"strings"
)

// Request is an incoming Anthropic Messages API request.
type Request struct {
	Model         string          `json:"model"`
	Messages      []Message       `json:"messages"`
	System        json.RawMessage `json:"system,omitempty"` // string OR []ContentBlock
	MaxTokens     int             `json:"max_tokens"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	Tools         []Tool          `json:"tools,omitempty"`
	ToolChoice    *ToolChoice     `json:"tool_choice,omitempty"`
	Metadata      json.RawMessage `json:"metadata,omitempty"`
}

// Message is one turn. Content may arrive as a string or an array of blocks; it is
// always normalized to a block slice on decode.
type Message struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// UnmarshalJSON normalizes string-or-array content into a block slice.
func (m *Message) UnmarshalJSON(b []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	blocks, err := ParseContent(raw.Content)
	if err != nil {
		return err
	}
	m.Content = blocks
	return nil
}

// ContentBlock is a union over Anthropic content block types. Unused fields stay
// zero and are omitted on marshal.
type ContentBlock struct {
	Type string `json:"type"`

	// text / thinking / redacted_thinking
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`

	// image
	Source *ImageSource `json:"source,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // string OR []ContentBlock
	IsError   bool            `json:"is_error,omitempty"`
}

// ImageSource is the source of an image block.
type ImageSource struct {
	Type      string `json:"type"` // base64 | url | file
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
	URL       string `json:"url,omitempty"`
	FileID    string `json:"file_id,omitempty"`
}

// Tool is a tool definition.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// ToolChoice controls tool use.
type ToolChoice struct {
	Type                   string `json:"type"` // auto | any | tool | none
	Name                   string `json:"name,omitempty"`
	DisableParallelToolUse bool   `json:"disable_parallel_tool_use,omitempty"`
}

// Response is an Anthropic Messages API response object.
type Response struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message"
	Role         string         `json:"role"` // "assistant"
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// Usage is token accounting.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ErrorResponse is the Anthropic error envelope.
type ErrorResponse struct {
	Type  string      `json:"type"` // "error"
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner error.
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// NewError builds an error envelope.
func NewError(etype, msg string) ErrorResponse {
	return ErrorResponse{Type: "error", Error: ErrorDetail{Type: etype, Message: msg}}
}

// ParseContent normalizes a string-or-array content value into a block slice.
func ParseContent(raw json.RawMessage) ([]ContentBlock, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []ContentBlock{{Type: "text", Text: s}}, nil
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// SystemText extracts the system prompt as plain text from the string-or-blocks form.
func SystemText(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var sb strings.Builder
	for i, b := range blocks {
		if b.Type != "text" {
			continue
		}
		if i > 0 && sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(b.Text)
	}
	return sb.String()
}
