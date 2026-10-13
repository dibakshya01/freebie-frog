package translate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/gemini"
)

// ToGeminiRequest converts an Anthropic request into a Gemini GenerateContentRequest
// targeting the given model id. Roles collapse to user|model, the Anthropic system
// prompt becomes systemInstruction, tool_result blocks become role:user
// functionResponse parts, and tool definitions are passed through verbatim via
// functionDeclarations[].parametersJsonSchema (the JSON-Schema escape hatch).
func ToGeminiRequest(a *anthropic.Request, modelID string) (*gemini.GenerateContentRequest, error) {
	g := &gemini.GenerateContentRequest{}

	if sys := anthropic.SystemText(a.System); sys != "" {
		g.SystemInstruction = &gemini.Content{Parts: []gemini.Part{{Text: sys}}}
	}

	// Resolve tool_use ids to function names so a later tool_result (which only
	// carries tool_use_id) can name its functionResponse.
	idToName := map[string]string{}
	for _, m := range a.Messages {
		for _, b := range m.Content {
			if b.Type == "tool_use" {
				idToName[b.ID] = b.Name
			}
		}
	}

	for _, m := range a.Messages {
		c, err := convertToGeminiContent(m, idToName)
		if err != nil {
			return nil, err
		}
		if c != nil {
			g.Contents = append(g.Contents, *c)
		}
	}

	if len(a.Tools) > 0 {
		decls := make([]gemini.FunctionDeclaration, 0, len(a.Tools))
		for _, t := range a.Tools {
			decls = append(decls, gemini.FunctionDeclaration{
				Name:                 t.Name,
				Description:          t.Description,
				ParametersJsonSchema: t.InputSchema,
			})
		}
		g.Tools = []gemini.Tool{{FunctionDeclarations: decls}}
	}
	if tc := convertGeminiToolConfig(a.ToolChoice); tc != nil {
		g.ToolConfig = tc
	}

	cfg := &gemini.GenerationConfig{}
	setCfg := false
	if a.MaxTokens > 0 {
		cfg.MaxOutputTokens = a.MaxTokens
		setCfg = true
	}
	if a.Temperature != nil {
		cfg.Temperature = a.Temperature
		setCfg = true
	}
	if a.TopP != nil {
		cfg.TopP = a.TopP
		setCfg = true
	}
	if a.TopK != nil {
		cfg.TopK = a.TopK
		setCfg = true
	}
	if len(a.StopSequences) > 0 {
		cfg.StopSequences = a.StopSequences
		setCfg = true
	}
	if setCfg {
		g.GenerationConfig = cfg
	}

	return g, nil
}

// convertToGeminiContent maps one Anthropic message to a single Gemini Content. An
// assistant turn becomes role:model; everything else (including tool_result blocks)
// becomes role:user. Returns nil when the message yields no parts.
func convertToGeminiContent(m anthropic.Message, idToName map[string]string) (*gemini.Content, error) {
	role := "user"
	if m.Role == "assistant" {
		role = "model"
	}

	var parts []gemini.Part
	for _, b := range m.Content {
		switch b.Type {
		case "text":
			if b.Text != "" {
				parts = append(parts, gemini.Part{Text: b.Text})
			}
		case "image":
			part, err := imageToGeminiPart(b.Source)
			if err != nil {
				return nil, err
			}
			parts = append(parts, part)
		case "tool_use":
			// Anthropic tool_use.input is already a JSON object; pass through as args.
			args := json.RawMessage(strings.TrimSpace(string(b.Input)))
			if len(args) == 0 || !json.Valid(args) {
				args = json.RawMessage("{}")
			}
			parts = append(parts, gemini.Part{FunctionCall: &gemini.FunctionCall{
				ID:   b.ID,
				Name: b.Name,
				Args: args,
			}})
		case "tool_result":
			name := idToName[b.ToolUseID]
			if name == "" {
				name = b.ToolUseID
			}
			parts = append(parts, gemini.Part{FunctionResponse: &gemini.FunctionResponse{
				ID:       b.ToolUseID,
				Name:     name,
				Response: toolResultObject(b.Content, b.IsError),
			}})
		case "thinking", "redacted_thinking":
			// Dropped on egress (documented in CAPABILITIES.md).
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return &gemini.Content{Role: role, Parts: parts}, nil
}

// toolResultObject wraps an Anthropic tool_result content (string or blocks) in a
// JSON object, as Gemini's functionResponse.response must be an object.
func toolResultObject(raw json.RawMessage, isError bool) json.RawMessage {
	text := toolResultText(raw)
	key := "result"
	if isError {
		key = "error"
	}
	b, err := json.Marshal(map[string]string{key: text})
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func imageToGeminiPart(s *anthropic.ImageSource) (gemini.Part, error) {
	if s == nil {
		return gemini.Part{}, fmt.Errorf("image block missing source")
	}
	switch s.Type {
	case "base64":
		if s.MediaType == "" || s.Data == "" {
			return gemini.Part{}, fmt.Errorf("base64 image missing media_type or data")
		}
		return gemini.Part{InlineData: &gemini.Blob{MimeType: s.MediaType, Data: s.Data}}, nil
	case "url":
		return gemini.Part{FileData: &gemini.FileData{MimeType: s.MediaType, FileURI: s.URL}}, nil
	default:
		return gemini.Part{}, fmt.Errorf("unsupported image source type %q", s.Type)
	}
}

func convertGeminiToolConfig(tc *anthropic.ToolChoice) *gemini.ToolConfig {
	if tc == nil {
		return nil
	}
	fc := &gemini.FunctionCallingConfig{}
	switch tc.Type {
	case "auto":
		fc.Mode = "AUTO"
	case "any":
		fc.Mode = "ANY"
	case "none":
		fc.Mode = "NONE"
	case "tool":
		fc.Mode = "ANY"
		if tc.Name != "" {
			fc.AllowedFunctionNames = []string{tc.Name}
		}
	default:
		return nil
	}
	return &gemini.ToolConfig{FunctionCallingConfig: fc}
}

// ToAnthropicResponseFromGemini converts a non-streaming Gemini response into an
// Anthropic Message. A prompt blocked by safety (zero candidates + promptFeedback)
// is reported as a clean refusal response, never a crash.
func ToAnthropicResponseFromGemini(g *gemini.GenerateContentResponse, requestedModel string) (*anthropic.Response, error) {
	resp := &anthropic.Response{
		ID:      anthropic.NewMessageID(),
		Type:    "message",
		Role:    "assistant",
		Model:   requestedModel,
		Content: []anthropic.ContentBlock{},
	}
	if g.UsageMetadata != nil {
		resp.Usage = anthropic.Usage{
			InputTokens:  g.UsageMetadata.PromptTokenCount,
			OutputTokens: g.UsageMetadata.CandidatesTokenCount,
		}
	}

	if len(g.Candidates) == 0 {
		if g.PromptFeedback != nil && g.PromptFeedback.BlockReason != "" {
			resp.StopReason = "refusal"
			return resp, nil
		}
		return nil, fmt.Errorf("provider response has no candidates")
	}

	cand := g.Candidates[0]
	var content []anthropic.ContentBlock
	hasFunctionCall := false
	for _, p := range cand.Content.Parts {
		switch {
		case p.FunctionCall != nil:
			hasFunctionCall = true
			input := json.RawMessage(strings.TrimSpace(string(p.FunctionCall.Args)))
			if len(input) == 0 || !json.Valid(input) {
				input = json.RawMessage("{}")
			}
			id := p.FunctionCall.ID
			if id == "" {
				id = newToolUseID()
			}
			content = append(content, anthropic.ContentBlock{
				Type:  "tool_use",
				ID:    id,
				Name:  p.FunctionCall.Name,
				Input: input,
			})
		case p.Thought:
			// Thinking is dropped on egress.
		case p.Text != "":
			content = append(content, anthropic.ContentBlock{Type: "text", Text: p.Text})
		}
	}
	if content != nil {
		resp.Content = content
	}
	resp.StopReason = MapGeminiFinishReason(cand.FinishReason, hasFunctionCall)
	return resp, nil
}

// MapGeminiFinishReason maps a Gemini finishReason (UPPERCASE) to an Anthropic
// stop_reason. The presence of a functionCall part is authoritative for a tool stop.
func MapGeminiFinishReason(reason string, hasFunctionCall bool) string {
	if hasFunctionCall {
		return "tool_use"
	}
	switch reason {
	case "STOP":
		return "end_turn"
	case "MAX_TOKENS":
		return "max_tokens"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII":
		return "refusal"
	default:
		return "end_turn"
	}
}

// newToolUseID returns a fresh Anthropic-style tool_use id ("toolu_" + random hex),
// used when Gemini omits a functionCall id.
func newToolUseID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "toolu_000000000000000000000000"
	}
	return "toolu_" + hex.EncodeToString(b)
}
