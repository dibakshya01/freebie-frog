package translate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/gemini"
)

// GeminiStreamTranslator reconstructs the Anthropic SSE event sequence from a Gemini
// streamGenerateContent?alt=sse stream. It mirrors OpenAIStreamTranslator's contract:
// message_start+ping are emitted lazily on the first chunk, text parts become
// text_delta events, and a functionCall part (delivered whole by Gemini) becomes a
// tool_use content_block_start plus a single input_json_delta carrying the complete
// JSON-marshaled args. There is no [DONE] sentinel; the stream ends on EOF.
type GeminiStreamTranslator struct {
	model       string
	inputTokens int

	started      bool
	nextIndex    int    // next Anthropic content-block index to assign
	openIndex    int    // currently open block index, or -1
	openKind     string // "", "text", "tool"
	sawTool      bool
	finishReason string
	blocked      bool
	outputTokens int
	outChars     int
}

// NewGeminiStreamTranslator prepares a translator; inputTokens is estimated from the
// request (used for message_start.usage, which must never be 0).
func NewGeminiStreamTranslator(req *anthropic.Request, model string) *GeminiStreamTranslator {
	return &GeminiStreamTranslator{
		model:       model,
		inputTokens: EstimateRequestTokens(req),
		openIndex:   -1,
	}
}

// Started reports whether any event has been emitted. If Run returns an error while
// Started() is false, the caller may safely fall back to another provider.
func (t *GeminiStreamTranslator) Started() bool { return t.started }

// Run consumes the Gemini SSE body and emits Anthropic events via out. If the stream
// fails after the first event was emitted, Run emits an Anthropic error event and
// returns nil (the request is handled, no fallback). If it fails before any event,
// Run returns a non-nil error so the caller can fall back.
func (t *GeminiStreamTranslator) Run(r io.Reader, out sink) error {
	reader := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if werr := t.processLine(trimmed, out); werr != nil {
				return werr // sink write failed (e.g. client disconnected)
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

// processLine handles one non-empty SSE line.
func (t *GeminiStreamTranslator) processLine(line []byte, out sink) error {
	if line[0] == ':' { // SSE comment / keep-alive
		return nil
	}
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return nil // ignore event:/id: lines
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}

	var chunk gemini.GenerateContentResponse
	if err := json.Unmarshal(data, &chunk); err != nil {
		return nil // tolerate malformed/partial keep-alive lines
	}
	if err := t.ensureStart(out); err != nil {
		return err
	}
	if chunk.UsageMetadata != nil && chunk.UsageMetadata.CandidatesTokenCount > 0 {
		t.outputTokens = chunk.UsageMetadata.CandidatesTokenCount
	}
	if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" {
		t.blocked = true
	}
	if len(chunk.Candidates) == 0 {
		return nil
	}
	cand := chunk.Candidates[0]
	if cand.FinishReason != "" {
		t.finishReason = cand.FinishReason
	}
	for _, p := range cand.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			if err := t.handleFunctionCall(out, p.FunctionCall); err != nil {
				return err
			}
		case p.Thought:
			// Thinking is dropped (we drop it when converting back anyway).
		case p.Text != "":
			if err := t.openBlock(out, "text"); err != nil {
				return err
			}
			t.outChars += len(p.Text)
			if err := emit(out, "content_block_delta", anthropic.ContentBlockDelta{
				Type: "content_block_delta", Index: t.openIndex,
				Delta: anthropic.StreamDelta{Type: "text_delta", Text: p.Text},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// handleFunctionCall emits a tool_use block for a whole functionCall part: a
// content_block_start followed by a single input_json_delta carrying the complete
// args (Gemini never fragments function arguments).
func (t *GeminiStreamTranslator) handleFunctionCall(out sink, fc *gemini.FunctionCall) error {
	if err := t.closeCurrent(out); err != nil {
		return err
	}
	idx := t.nextIndex
	t.nextIndex++
	t.openIndex = idx
	t.openKind = "tool"
	t.sawTool = true

	id := fc.ID
	if id == "" {
		id = newToolUseID()
	}
	name := fc.Name
	if name == "" {
		name = "unknown"
	}
	if err := emit(out, "content_block_start", anthropic.ContentBlockStart{
		Type: "content_block_start", Index: idx,
		ContentBlock: anthropic.ContentBlock{
			Type:  "tool_use",
			ID:    id,
			Name:  name,
			Input: json.RawMessage("{}"),
		},
	}); err != nil {
		return err
	}

	args := bytes.TrimSpace([]byte(fc.Args))
	if len(args) == 0 || !json.Valid(args) {
		args = []byte("{}")
	}
	t.outChars += len(args)
	return emit(out, "content_block_delta", anthropic.ContentBlockDelta{
		Type: "content_block_delta", Index: t.openIndex,
		Delta: anthropic.StreamDelta{Type: "input_json_delta", PartialJSON: string(args)},
	})
}

// openBlock ensures a text block is open.
func (t *GeminiStreamTranslator) openBlock(out sink, kind string) error {
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
	return emit(out, "content_block_start", anthropic.ContentBlockStart{
		Type: "content_block_start", Index: idx,
		ContentBlock: anthropic.ContentBlock{Type: "text", Text: ""},
	})
}

func (t *GeminiStreamTranslator) closeCurrent(out sink) error {
	if t.openKind == "" {
		return nil
	}
	idx := t.openIndex
	t.openKind = ""
	t.openIndex = -1
	return emit(out, "content_block_stop", anthropic.ContentBlockStop{Type: "content_block_stop", Index: idx})
}

func (t *GeminiStreamTranslator) ensureStart(out sink) error {
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

func (t *GeminiStreamTranslator) finalize(out sink) error {
	if !t.started {
		if err := t.ensureStart(out); err != nil {
			return err
		}
	}
	if err := t.closeCurrent(out); err != nil {
		return err
	}
	var stop string
	if t.blocked && !t.sawTool && t.finishReason == "" {
		stop = "refusal"
	} else {
		stop = MapGeminiFinishReason(t.finishReason, t.sawTool)
	}
	outTok := t.outputTokens
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

func (t *GeminiStreamTranslator) emitError(out sink, msg string) error {
	return emit(out, "error", anthropic.ErrorEvent{
		Type:  "error",
		Error: anthropic.ErrorDetail{Type: "api_error", Message: msg},
	})
}

// terminateWithError emits an error event, then closes any open block and emits
// message_stop so a strict SSE consumer sees a terminated stream (FR-14).
func (t *GeminiStreamTranslator) terminateWithError(out sink, msg string) {
	_ = t.emitError(out, msg)
	_ = t.closeCurrent(out)
	_ = emit(out, "message_stop", anthropic.MessageStop{Type: "message_stop"})
}
