package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/openai"
)

// ErrEmptyStream means the upstream returned a 2xx but no usable stream content, so
// the caller may still fall back to another provider (nothing was emitted).
var ErrEmptyStream = errors.New("empty provider stream")

// sink is the callback that receives translated Anthropic SSE events.
type sink = func(anthropic.StreamEvent) error

// OpenAIStreamTranslator reconstructs the Anthropic SSE event sequence from an
// OpenAI Chat Completions SSE stream. It implements the lifecycle described in
// build-plan.md FR-8: a single content block is open at a time; OpenAI
// tool_call indices are remapped to Anthropic content-block indices offset by any
// preceding text/thinking block; tool arguments become input_json_delta fragments.
type OpenAIStreamTranslator struct {
	model       string
	inputTokens int

	started      bool
	nextIndex    int    // next Anthropic content-block index to assign
	openIndex    int    // currently open block index, or -1
	openKind     string // "", "text", "thinking", "tool"
	curToolOA    int    // OpenAI tool index of the currently open tool block, or -1
	sawTool      bool
	finishReason *string
	usage        *openai.Usage
	outChars     int
}

// NewOpenAIStreamTranslator prepares a translator; inputTokens is estimated from the
// request (used for message_start.usage, which must never be 0).
func NewOpenAIStreamTranslator(req *anthropic.Request, model string) *OpenAIStreamTranslator {
	return &OpenAIStreamTranslator{
		model:       model,
		inputTokens: EstimateRequestTokens(req),
		openIndex:   -1,
		curToolOA:   -1,
	}
}

// Started reports whether any event has been emitted. If Run returns an error while
// Started() is false, the caller may safely fall back to another provider.
func (t *OpenAIStreamTranslator) Started() bool { return t.started }

// Run consumes the OpenAI SSE body and emits Anthropic events via out. If the stream
// fails after the first event was emitted, Run emits an Anthropic error event and
// returns nil (the request is handled, no fallback). If it fails before any event,
// Run returns a non-nil error so the caller can fall back.
func (t *OpenAIStreamTranslator) Run(r io.Reader, out sink) error {
	reader := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			done, werr := t.processLine(trimmed, out)
			if werr != nil {
				return werr // sink write failed (e.g. client disconnected)
			}
			if done {
				return t.finalize(out)
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if t.started {
				_ = t.emitError(out, "upstream stream error: "+err.Error())
				return nil
			}
			return err
		}
	}
	if !t.started {
		return ErrEmptyStream
	}
	return t.finalize(out)
}

// processLine handles one non-empty SSE line. Returns done=true on "data: [DONE]".
func (t *OpenAIStreamTranslator) processLine(line []byte, out sink) (bool, error) {
	if line[0] == ':' { // SSE comment / keep-alive
		return false, nil
	}
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return false, nil // ignore event:/id: lines
	}
	data = bytes.TrimSpace(data)
	if string(data) == "[DONE]" {
		return true, nil
	}

	var chunk openai.StreamChunk
	if err := json.Unmarshal(data, &chunk); err != nil {
		return false, nil // tolerate malformed/partial keep-alive lines
	}
	if err := t.ensureStart(out); err != nil {
		return false, err
	}
	if chunk.Usage != nil {
		t.usage = chunk.Usage
	}
	for _, ch := range chunk.Choices {
		if ch.FinishReason != nil {
			t.finishReason = ch.FinishReason
		}
		if ch.Delta == nil {
			continue
		}
		d := ch.Delta
		if d.ReasoningContent != nil && *d.ReasoningContent != "" {
			if err := t.openBlock(out, "thinking"); err != nil {
				return false, err
			}
			t.outChars += len(*d.ReasoningContent)
			if err := emit(out, "content_block_delta", anthropic.ContentBlockDelta{
				Type: "content_block_delta", Index: t.openIndex,
				Delta: anthropic.StreamDelta{Type: "thinking_delta", Thinking: *d.ReasoningContent},
			}); err != nil {
				return false, err
			}
		}
		if d.Content != nil && *d.Content != "" {
			if err := t.openBlock(out, "text"); err != nil {
				return false, err
			}
			t.outChars += len(*d.Content)
			if err := emit(out, "content_block_delta", anthropic.ContentBlockDelta{
				Type: "content_block_delta", Index: t.openIndex,
				Delta: anthropic.StreamDelta{Type: "text_delta", Text: *d.Content},
			}); err != nil {
				return false, err
			}
		}
		for _, tc := range d.ToolCalls {
			if err := t.handleToolCall(out, tc); err != nil {
				return false, err
			}
		}
	}
	return false, nil
}

func (t *OpenAIStreamTranslator) handleToolCall(out sink, tc openai.ToolCall) error {
	oaIdx := 0
	if tc.Index != nil {
		oaIdx = *tc.Index
	}
	if t.openKind != "tool" || t.curToolOA != oaIdx {
		if err := t.closeCurrent(out); err != nil {
			return err
		}
		idx := t.nextIndex
		t.nextIndex++
		t.openIndex = idx
		t.openKind = "tool"
		t.curToolOA = oaIdx
		t.sawTool = true
		if err := emit(out, "content_block_start", anthropic.ContentBlockStart{
			Type: "content_block_start", Index: idx,
			ContentBlock: anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    tc.ID,
				Name:  tc.Function.Name,
				Input: json.RawMessage("{}"),
			},
		}); err != nil {
			return err
		}
	}
	if tc.Function.Arguments != "" {
		t.outChars += len(tc.Function.Arguments)
		return emit(out, "content_block_delta", anthropic.ContentBlockDelta{
			Type: "content_block_delta", Index: t.openIndex,
			Delta: anthropic.StreamDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
		})
	}
	return nil
}

// openBlock ensures a text/thinking block of the given kind is open.
func (t *OpenAIStreamTranslator) openBlock(out sink, kind string) error {
	if t.openKind == kind {
		return nil
	}
	if err := t.closeCurrent(out); err != nil {
		return err
	}
	idx := t.nextIndex
	t.nextIndex++
	t.openIndex = idx
	t.openKind = kind

	var block anthropic.ContentBlock
	switch kind {
	case "text":
		block = anthropic.ContentBlock{Type: "text", Text: ""}
	case "thinking":
		block = anthropic.ContentBlock{Type: "thinking", Thinking: ""}
	}
	return emit(out, "content_block_start", anthropic.ContentBlockStart{
		Type: "content_block_start", Index: idx, ContentBlock: block,
	})
}

func (t *OpenAIStreamTranslator) closeCurrent(out sink) error {
	if t.openKind == "" {
		return nil
	}
	idx := t.openIndex
	t.openKind = ""
	t.openIndex = -1
	t.curToolOA = -1
	return emit(out, "content_block_stop", anthropic.ContentBlockStop{Type: "content_block_stop", Index: idx})
}

func (t *OpenAIStreamTranslator) ensureStart(out sink) error {
	if t.started {
		return nil
	}
	t.started = true
	if err := emit(out, "message_start", anthropic.MessageStart{
		Type: "message_start",
		Message: anthropic.MessageStartMsg{
			ID:      anthropic.NewMessageID(),
			Type:    "message",
			Role:    "assistant",
			Model:   t.model,
			Content: []anthropic.ContentBlock{},
			Usage:   anthropic.Usage{InputTokens: t.inputTokens, OutputTokens: 0},
		},
	}); err != nil {
		return err
	}
	return emit(out, "ping", anthropic.Ping{Type: "ping"})
}

func (t *OpenAIStreamTranslator) finalize(out sink) error {
	if !t.started {
		if err := t.ensureStart(out); err != nil {
			return err
		}
	}
	if err := t.closeCurrent(out); err != nil {
		return err
	}
	stop := MapStopReason(t.finishReason, t.sawTool)
	outTok := 0
	if t.usage != nil {
		outTok = t.usage.CompletionTokens
	}
	if outTok == 0 {
		outTok = t.outChars / 4
		if outTok < 1 {
			outTok = 1
		}
	}
	if err := emit(out, "message_delta", anthropic.MessageDeltaEvent{
		Type:  "message_delta",
		Delta: anthropic.MessageDeltaBody{StopReason: &stop},
		Usage: anthropic.MessageDeltaUsage{OutputTokens: outTok},
	}); err != nil {
		return err
	}
	return emit(out, "message_stop", anthropic.MessageStop{Type: "message_stop"})
}

func (t *OpenAIStreamTranslator) emitError(out sink, msg string) error {
	return emit(out, "error", anthropic.ErrorEvent{
		Type:  "error",
		Error: anthropic.ErrorDetail{Type: "api_error", Message: msg},
	})
}

func emit(out sink, event string, payload any) error {
	return out(anthropic.StreamEvent{Event: event, Payload: payload})
}
