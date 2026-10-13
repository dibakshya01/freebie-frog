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
	names := eventNames(events)
	// message_start first; an error event present; terminated with message_stop; any
	// open content block closed before the stop.
	if events[0].Event != "message_start" {
		t.Errorf("first event = %q, want message_start", events[0].Event)
	}
	if !containsEvent(names, "error") {
		t.Fatalf("expected an error event, got %v", names)
	}
	if names[len(names)-1] != "message_stop" {
		t.Fatalf("stream must terminate with message_stop, got %v", names)
	}
	if !containsEvent(names, "content_block_stop") {
		t.Errorf("open block should be closed on error, got %v", names)
	}
}

func containsEvent(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestStream_ToolNameAfterArgs(t *testing.T) {
	// A provider that sends id/name only after the first argument fragment must still
	// produce a content_block_start with the real id/name (defensive accumulation).
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"x\":"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"do_it","arguments":"1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)

	var start *anthropic.ContentBlockStart
	var args strings.Builder
	for _, e := range events {
		switch p := e.Payload.(type) {
		case anthropic.ContentBlockStart:
			if p.ContentBlock.Type == "tool_use" {
				cp := p
				start = &cp
			}
		case anthropic.ContentBlockDelta:
			if p.Delta.Type == "input_json_delta" {
				args.WriteString(p.Delta.PartialJSON)
			}
		}
	}
	if start == nil || start.ContentBlock.ID != "call_9" || start.ContentBlock.Name != "do_it" {
		t.Fatalf("tool block id/name not recovered from later chunk: %+v", start)
	}
	if args.String() != `{"x":1}` {
		t.Errorf("assembled args = %q, want {\"x\":1}", args.String())
	}
}

func TestStream_SyntheticKeyDoesNotCollideWithNegativeIndex(t *testing.T) {
	// An index-less tool (synthetic key) followed by an explicit index:-1 tool must
	// remain two distinct blocks (N2).
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"a","type":"function","function":{"name":"f_synth","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":-1,"id":"b","type":"function","function":{"name":"f_neg","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)
	var names []string
	for _, e := range events {
		if p, ok := e.Payload.(anthropic.ContentBlockStart); ok && p.ContentBlock.Type == "tool_use" {
			names = append(names, p.ContentBlock.Name)
		}
	}
	if len(names) != 2 || names[0] != "f_synth" || names[1] != "f_neg" {
		t.Fatalf("expected two distinct tool blocks [f_synth f_neg], got %v", names)
	}
}

func TestStream_ToolNameNeverArrivesGetsPlaceholder(t *testing.T) {
	// A broken upstream that sends only args (no name/id) must still produce a
	// schema-valid tool_use with a non-empty name and id (N3).
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"a\":1}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)
	var start *anthropic.ContentBlockStart
	for _, e := range events {
		if p, ok := e.Payload.(anthropic.ContentBlockStart); ok && p.ContentBlock.Type == "tool_use" {
			cp := p
			start = &cp
		}
	}
	if start == nil || start.ContentBlock.Name == "" || start.ContentBlock.ID == "" {
		t.Fatalf("nameless tool must get a placeholder name and id, got %+v", start)
	}
}

func TestStream_ParallelToolsWithoutIndex(t *testing.T) {
	// Two index-less tool calls (each with its own id) must not be merged.
	events := runStream(t, []string{
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"a","type":"function","function":{"name":"f1","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{"tool_calls":[{"id":"b","type":"function","function":{"name":"f2","arguments":"{}"}}]}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	}, true)

	var names []string
	for _, e := range events {
		if p, ok := e.Payload.(anthropic.ContentBlockStart); ok && p.ContentBlock.Type == "tool_use" {
			names = append(names, p.ContentBlock.Name)
		}
	}
	if len(names) != 2 || names[0] != "f1" || names[1] != "f2" {
		t.Fatalf("expected two distinct tool blocks f1,f2; got %v", names)
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
