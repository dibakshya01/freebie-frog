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

// pendingTool buffers a streaming tool call until its name is known, so the Anthropic
// content_block_start carries the real id/name even when the provider sends arguments
// before (or without) the name on the first chunk (FR-8 defensive accumulation).
type pendingTool struct {
	key       int  // OpenAI tool_calls index, or a synthetic key when absent
	synthetic bool // true when key is a synthetic id (index was absent)
	anthIndex int
	id        string
	name      string
	args      bytes.Buffer
	open      bool
}

// OpenAIStreamTranslator reconstructs the Anthropic SSE event sequence from an OpenAI
// Chat Completions SSE stream (build-plan.md FR-8).
type OpenAIStreamTranslator struct {
	model       string
	inputTokens int

	started   bool
	nextIndex int    // next Anthropic content-block index to assign
	openKind  string // "", "text", "thinking" (simple blocks)
	openIndex int    // index of the open simple block
	tool      *pendingTool
	synthKey  int

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
	}
}

// Started reports whether any event has been emitted. If Run returns an error while
// Started() is false, the caller may safely fall back to another provider.
func (t *OpenAIStreamTranslator) Started() bool { return t.started }

// Run consumes the OpenAI SSE body and emits Anthropic events via out.
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
				t.terminateWithError(out, "upstream stream error: "+err.Error())
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

func (t *OpenAIStreamTranslator) processLine(line []byte, out sink) (bool, error) {
	if line[0] == ':' {
		return false, nil
	}
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return false, nil
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
			if err := t.openSimple(out, "thinking"); err != nil {
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
			if err := t.openSimple(out, "text"); err != nil {
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

// toolRef identifies the tool-call slot: an explicit OpenAI index (synthetic=false),
// or a synthetic key when the index is absent (synthetic=true). The synthetic flag
// keeps synthetic keys from colliding with explicit (even negative) indices.
func (t *OpenAIStreamTranslator) toolRef(tc openai.ToolCall) (int, bool) {
	if tc.Index != nil {
		return *tc.Index, false
	}
	// No index: a tool call carrying an id (or when none is active) starts a new slot;
	// otherwise it continues the current index-less tool.
	if t.tool != nil && t.tool.synthetic && tc.ID == "" {
		return t.tool.key, true
	}
	t.synthKey++
	return t.synthKey, true
}

func (t *OpenAIStreamTranslator) handleToolCall(out sink, tc openai.ToolCall) error {
	key, synthetic := t.toolRef(tc)
	if t.tool == nil || t.tool.key != key || t.tool.synthetic != synthetic {
		if err := t.finishTool(out); err != nil {
			return err
		}
		t.tool = &pendingTool{key: key, synthetic: synthetic}
	}
	if tc.ID != "" {
		t.tool.id = tc.ID
	}
	if tc.Function.Name != "" {
		t.tool.name = tc.Function.Name
	}
	if err := t.tryOpenTool(out); err != nil {
		return err
	}
	if tc.Function.Arguments != "" {
		t.outChars += len(tc.Function.Arguments)
		if t.tool.open {
			return emit(out, "content_block_delta", anthropic.ContentBlockDelta{
				Type: "content_block_delta", Index: t.tool.anthIndex,
				Delta: anthropic.StreamDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
			})
		}
		t.tool.args.WriteString(tc.Function.Arguments)
	}
	return nil
}

// tryOpenTool emits content_block_start once the tool's name is known, flushing any
// buffered argument fragments.
func (t *OpenAIStreamTranslator) tryOpenTool(out sink) error {
	if t.tool == nil || t.tool.open || t.tool.name == "" {
		return nil
	}
	return t.openTool(out)
}

func (t *OpenAIStreamTranslator) openTool(out sink) error {
	if err := t.closeSimple(out); err != nil {
		return err
	}
	t.tool.anthIndex = t.nextIndex
	t.nextIndex++
	t.tool.open = true
	t.sawTool = true
	// Guarantee a schema-valid block even if a broken upstream never sent name/id.
	if t.tool.id == "" {
		t.tool.id = newToolUseID()
	}
	if t.tool.name == "" {
		t.tool.name = "unknown"
	}
	if err := emit(out, "content_block_start", anthropic.ContentBlockStart{
		Type: "content_block_start", Index: t.tool.anthIndex,
		ContentBlock: anthropic.ContentBlock{Type: "tool_use", ID: t.tool.id, Name: t.tool.name, Input: json.RawMessage("{}")},
	}); err != nil {
		return err
	}
	if t.tool.args.Len() > 0 {
		args := t.tool.args.String()
		t.tool.args.Reset()
		return emit(out, "content_block_delta", anthropic.ContentBlockDelta{
			Type: "content_block_delta", Index: t.tool.anthIndex,
			Delta: anthropic.StreamDelta{Type: "input_json_delta", PartialJSON: args},
		})
	}
	return nil
}

// finishTool closes the current tool block (opening it best-effort first if the name
// never arrived, so buffered arguments aren't lost).
func (t *OpenAIStreamTranslator) finishTool(out sink) error {
	if t.tool == nil {
		return nil
	}
	if !t.tool.open {
		if err := t.openTool(out); err != nil {
			return err
		}
	}
	idx := t.tool.anthIndex
	t.tool = nil
	return emit(out, "content_block_stop", anthropic.ContentBlockStop{Type: "content_block_stop", Index: idx})
}

func (t *OpenAIStreamTranslator) openSimple(out sink, kind string) error {
	if t.openKind == kind {
		return nil
	}
	if err := t.finishTool(out); err != nil {
		return err
	}
	if err := t.closeSimple(out); err != nil {
		return err
	}
	t.openIndex = t.nextIndex
	t.nextIndex++
	t.openKind = kind
	var block anthropic.ContentBlock
	switch kind {
	case "text":
		block = anthropic.ContentBlock{Type: "text", Text: ""}
	case "thinking":
		block = anthropic.ContentBlock{Type: "thinking", Thinking: ""}
	}
	return emit(out, "content_block_start", anthropic.ContentBlockStart{
		Type: "content_block_start", Index: t.openIndex, ContentBlock: block,
	})
}

func (t *OpenAIStreamTranslator) closeSimple(out sink) error {
	if t.openKind == "" {
		return nil
	}
	idx := t.openIndex
	t.openKind = ""
	t.openIndex = -1
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
	if err := t.finishTool(out); err != nil {
		return err
	}
	if err := t.closeSimple(out); err != nil {
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

// terminateWithError emits an error event, then closes any open block and emits
// message_stop so a strict SSE consumer sees a terminated stream (FR-14).
func (t *OpenAIStreamTranslator) terminateWithError(out sink, msg string) {
	_ = t.emitError(out, msg)
	_ = t.finishTool(out)
	_ = t.closeSimple(out)
	_ = emit(out, "message_stop", anthropic.MessageStop{Type: "message_stop"})
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
