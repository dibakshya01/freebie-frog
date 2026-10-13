package translate

import (
	"io"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
)

func runGeminiStream(t *testing.T, chunks []string) []anthropic.StreamEvent {
	t.Helper()
	var raw strings.Builder
	for _, c := range chunks {
		raw.WriteString("data: " + c + "\n\n")
	}
	// Note: NO [DONE] sentinel — Gemini ends the stream on close.
	return runGeminiStreamReader(t, strings.NewReader(raw.String()))
}

func runGeminiStreamReader(t *testing.T, r io.Reader) []anthropic.StreamEvent {
	t.Helper()
	req := &anthropic.Request{Model: "m", Messages: []anthropic.Message{userText("hi")}}
	tr := NewGeminiStreamTranslator(req, "claude-x")
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

func TestGeminiStream_TextOnly(t *testing.T) {
	events := runGeminiStream(t, []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":""}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":4}}`,
	})

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
	if md.Usage.OutputTokens != 4 {
		t.Errorf("output tokens = %d, want 4 (from usageMetadata)", md.Usage.OutputTokens)
	}
}

func TestGeminiStream_FunctionCall_WholeArgs(t *testing.T) {
	events := runGeminiStream(t, []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"get_weather","args":{"city":"SF"}}}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[]},"finishReason":"STOP"}]}`,
	})

	assertNames(t, eventNames(events), []string{
		"message_start", "ping",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	})

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
	if toolStart.Index != 0 || toolStart.ContentBlock.Name != "get_weather" {
		t.Errorf("tool block = %+v", toolStart)
	}
	// Gemini delivers functionCall whole: a single input_json_delta with the full args.
	if argJSON.String() != `{"city":"SF"}` {
		t.Errorf("assembled tool args = %q, want {\"city\":\"SF\"}", argJSON.String())
	}
	md := findMessageDelta(t, events)
	if md.Delta.StopReason == nil || *md.Delta.StopReason != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", md.Delta.StopReason)
	}
}

func TestGeminiStream_GuardsThoughtParts(t *testing.T) {
	// A part carrying only thought/thoughtSignature and no text must not produce a
	// text_delta or crash.
	events := runGeminiStream(t, []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"thought":true,"thoughtSignature":"abc"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"answer"}]},"finishReason":"STOP"}]}`,
	})
	var text strings.Builder
	for _, e := range events {
		if d, ok := e.Payload.(anthropic.ContentBlockDelta); ok && d.Delta.Type == "text_delta" {
			text.WriteString(d.Delta.Text)
		}
	}
	if text.String() != "answer" {
		t.Errorf("text = %q, want answer (thought part must be dropped)", text.String())
	}
}

func TestGeminiStream_MidStreamError_EmitsErrorEvent(t *testing.T) {
	r := &errReader{
		data: []byte("data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"hi\"}]}}]}\n\n"),
		err:  io.ErrUnexpectedEOF,
	}
	events := runGeminiStreamReader(t, r)
	if len(events) == 0 || events[len(events)-1].Event != "error" {
		t.Fatalf("expected a trailing error event, got %v", eventNames(events))
	}
	if events[0].Event != "message_start" {
		t.Errorf("first event = %q, want message_start", events[0].Event)
	}
}

func TestGeminiStream_EmptyStreamFallsBack(t *testing.T) {
	req := &anthropic.Request{Model: "m", Messages: []anthropic.Message{userText("hi")}}
	tr := NewGeminiStreamTranslator(req, "claude-x")
	err := tr.Run(strings.NewReader(""), func(anthropic.StreamEvent) error { return nil })
	if err == nil {
		t.Fatal("empty stream should return an error so the caller can fall back")
	}
	if tr.Started() {
		t.Error("Started() should be false for an empty stream")
	}
}
