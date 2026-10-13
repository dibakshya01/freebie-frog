package translate

import (
	"io"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
)

func runStream(t *testing.T, chunks []string, withDone bool) []anthropic.StreamEvent {
	t.Helper()
	var raw strings.Builder
	for _, c := range chunks {
		raw.WriteString("data: " + c + "\n\n")
	}
	if withDone {
		raw.WriteString("data: [DONE]\n\n")
	}
	return runStreamReader(t, strings.NewReader(raw.String()))
}

func runStreamReader(t *testing.T, r io.Reader) []anthropic.StreamEvent {
	t.Helper()
	req := &anthropic.Request{Model: "m", Messages: []anthropic.Message{userText("hi")}}
	tr := NewOpenAIStreamTranslator(req, "claude-x")
	var events []anthropic.StreamEvent
	err := tr.Run(r, func(ev anthropic.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return events
}

func eventNames(events []anthropic.StreamEvent) []string {
	names := make([]string, len(events))
	for i, e := range events {
		names[i] = e.Event
	}
	return names
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("event sequence mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestStream_TextOnly(t *testing.T) {
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, true)

	assertNames(t, eventNames(events), []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})

	var text strings.Builder
	for _, e := range events {
		if d, ok := e.Payload.(anthropic.ContentBlockDelta); ok && d.Delta.Type == "text_delta" {
			text.WriteString(d.Delta.Text)
		}
	}
	if text.String() != "Hello" {
		t.Errorf("concatenated text = %q, want Hello", text.String())
	}
	md := findMessageDelta(t, events)
	if md.Delta.StopReason == nil || *md.Delta.StopReason != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", md.Delta.StopReason)
	}
}

func TestStream_TextThenTool_IndexMapping(t *testing.T) {
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"role":"assistant","content":""}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"Let me check."}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_weather","arguments":""}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"SF\"}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)

	assertNames(t, eventNames(events), []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop", // text (index 0)
		"content_block_start", "content_block_delta", "content_block_delta", "content_block_stop", // tool (index 1)
		"message_delta", "message_stop",
	})

	// The tool_use block must be content-block index 1 (offset past the text block).
	var toolStart *anthropic.ContentBlockStart
	var argJSON strings.Builder
	for _, e := range events {
		switch p := e.Payload.(type) {
		case anthropic.ContentBlockStart:
			if p.ContentBlock.Type == "tool_use" {
				cp := p
				toolStart = &cp
			}
		case anthropic.ContentBlockDelta:
			if p.Delta.Type == "input_json_delta" {
				argJSON.WriteString(p.Delta.PartialJSON)
			}
		}
	}
	if toolStart == nil {
		t.Fatal("no tool_use content_block_start")
	}
	if toolStart.Index != 1 {
		t.Errorf("tool block index = %d, want 1 (offset past text block)", toolStart.Index)
	}
	if toolStart.ContentBlock.ID != "call_1" || toolStart.ContentBlock.Name != "get_weather" {
		t.Errorf("tool block = %+v", toolStart.ContentBlock)
	}
	if argJSON.String() != `{"city":"SF"}` {
		t.Errorf("assembled tool args = %q, want {\"city\":\"SF\"}", argJSON.String())
	}
	md := findMessageDelta(t, events)
	if md.Delta.StopReason == nil || *md.Delta.StopReason != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", md.Delta.StopReason)
	}
}

func TestStream_ToolOnly_IndexZero(t *testing.T) {
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)
	for _, e := range events {
		if p, ok := e.Payload.(anthropic.ContentBlockStart); ok && p.ContentBlock.Type == "tool_use" {
			if p.Index != 0 {
				t.Errorf("tool-only block index = %d, want 0", p.Index)
			}
		}
	}
}

func TestStream_ReasoningBecomesThinking(t *testing.T) {
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"reasoning_content":"thinking..."}}]}`,
		`{"choices":[{"index":0,"delta":{"content":"answer"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}, true)
	sawThinking := false
	for _, e := range events {
		if d, ok := e.Payload.(anthropic.ContentBlockDelta); ok && d.Delta.Type == "thinking_delta" {
			sawThinking = true
			if d.Delta.Thinking != "thinking..." {
				t.Errorf("thinking delta = %q", d.Delta.Thinking)
			}
		}
	}
	if !sawThinking {
		t.Error("reasoning_content was not mapped to a thinking_delta")
	}
}

type errReader struct {
	data []byte
	sent bool
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if !e.sent {
		e.sent = true
		return copy(p, e.data), nil
	}
	return 0, e.err
}

func TestStream_MidStreamError_EmitsErrorEvent(t *testing.T) {
	r := &errReader{
		data: []byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n"),
		err:  io.ErrUnexpectedEOF,
	}
	events := runStreamReader(t, r)
	if len(events) == 0 || events[len(events)-1].Event != "error" {
		t.Fatalf("expected a trailing error event, got %v", eventNames(events))
	}
	// message_start must have been emitted before the error (we committed).
	if events[0].Event != "message_start" {
		t.Errorf("first event = %q, want message_start", events[0].Event)
	}
}

func findMessageDelta(t *testing.T, events []anthropic.StreamEvent) anthropic.MessageDeltaEvent {
	t.Helper()
	for _, e := range events {
		if md, ok := e.Payload.(anthropic.MessageDeltaEvent); ok {
			return md
		}
	}
	t.Fatal("no message_delta event")
	return anthropic.MessageDeltaEvent{}
}
