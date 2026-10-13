package anthropic

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// StreamEvent is one Anthropic SSE event produced by an adapter: an event name and
// a JSON-marshalable payload (which itself carries a "type" field). Keeping this
// transport-agnostic lets adapters be tested without an HTTP response writer.
type StreamEvent struct {
	Event   string
	Payload any
}

// --- event payload types (each carries its own "type") ---

// MessageStart is the first streaming event.
type MessageStart struct {
	Type    string          `json:"type"` // "message_start"
	Message MessageStartMsg `json:"message"`
}

// MessageStartMsg is the skeleton message inside message_start.
type MessageStartMsg struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"` // "message"
	Role         string         `json:"role"` // "assistant"
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"` // empty at start
	StopReason   *string        `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

// ContentBlockStart opens a content block.
type ContentBlockStart struct {
	Type         string       `json:"type"` // "content_block_start"
	Index        int          `json:"index"`
	ContentBlock ContentBlock `json:"content_block"`
}

// ContentBlockDelta carries an incremental change to an open block.
type ContentBlockDelta struct {
	Type  string      `json:"type"` // "content_block_delta"
	Index int         `json:"index"`
	Delta StreamDelta `json:"delta"`
}

// StreamDelta is the delta body (text_delta | input_json_delta | thinking_delta | signature_delta).
type StreamDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// ContentBlockStop closes a content block.
type ContentBlockStop struct {
	Type  string `json:"type"` // "content_block_stop"
	Index int    `json:"index"`
}

// MessageDeltaEvent carries top-level changes (stop_reason) and cumulative usage.
type MessageDeltaEvent struct {
	Type  string            `json:"type"` // "message_delta"
	Delta MessageDeltaBody  `json:"delta"`
	Usage MessageDeltaUsage `json:"usage"`
}

// MessageDeltaBody holds the terminal stop reason.
type MessageDeltaBody struct {
	StopReason   *string `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence"`
}

// MessageDeltaUsage is the cumulative output usage.
type MessageDeltaUsage struct {
	OutputTokens int `json:"output_tokens"`
}

// MessageStop ends the stream.
type MessageStop struct {
	Type string `json:"type"` // "message_stop"
}

// Ping is a keep-alive event.
type Ping struct {
	Type string `json:"type"` // "ping"
}

// ErrorEvent is a mid-stream error (after HTTP 200).
type ErrorEvent struct {
	Type  string      `json:"type"` // "error"
	Error ErrorDetail `json:"error"`
}

// SSEWriter writes Anthropic SSE events to an http.ResponseWriter, flushing each.
type SSEWriter struct {
	w io.Writer
	f http.Flusher
}

// NewSSEWriter prepares w for SSE and returns a writer. It sets the required headers.
func NewSSEWriter(w http.ResponseWriter) *SSEWriter {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	fl, _ := w.(http.Flusher)
	return &SSEWriter{w: w, f: fl}
}

// Emit writes one StreamEvent (event line + data line + blank line) and flushes.
func (s *SSEWriter) Emit(ev StreamEvent) error {
	data, err := json.Marshal(ev.Payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", ev.Event, data); err != nil {
		return err
	}
	if s.f != nil {
		s.f.Flush()
	}
	return nil
}
