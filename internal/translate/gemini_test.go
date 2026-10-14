package translate

import (
	"encoding/json"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/gemini"
)

func TestToGeminiRequest_SystemAndRoles(t *testing.T) {
	req := &anthropic.Request{
		Model:     "claude-x",
		MaxTokens: 100,
		System:    json.RawMessage(`"be nice"`),
		Messages: []anthropic.Message{
			userText("hi"),
			{Role: "assistant", Content: []anthropic.ContentBlock{{Type: "text", Text: "hello"}}},
		},
	}
	g, err := ToGeminiRequest(req, "gemini-test")
	if err != nil {
		t.Fatal(err)
	}
	if g.SystemInstruction == nil || len(g.SystemInstruction.Parts) != 1 || g.SystemInstruction.Parts[0].Text != "be nice" {
		t.Errorf("systemInstruction = %+v", g.SystemInstruction)
	}
	if len(g.Contents) != 2 {
		t.Fatalf("got %d contents, want 2", len(g.Contents))
	}
	if g.Contents[0].Role != "user" || g.Contents[0].Parts[0].Text != "hi" {
		t.Errorf("content[0] = %+v", g.Contents[0])
	}
	if g.Contents[1].Role != "model" || g.Contents[1].Parts[0].Text != "hello" {
		t.Errorf("content[1] (assistant must map to model) = %+v", g.Contents[1])
	}
	if g.GenerationConfig == nil || g.GenerationConfig.MaxOutputTokens != 100 {
		t.Errorf("generationConfig = %+v", g.GenerationConfig)
	}
}

func TestToGeminiRequest_ToolUseAndResultRoundTrip(t *testing.T) {
	req := &anthropic.Request{
		Model:     "m",
		MaxTokens: 10,
		Messages: []anthropic.Message{
			{Role: "assistant", Content: []anthropic.ContentBlock{
				{Type: "tool_use", ID: "toolu_1", Name: "get_weather", Input: json.RawMessage(`{"city":"SF"}`)},
			}},
			{Role: "user", Content: []anthropic.ContentBlock{
				{Type: "tool_result", ToolUseID: "toolu_1", Content: json.RawMessage(`"18C"`)},
			}},
		},
	}
	g, err := ToGeminiRequest(req, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Contents) != 2 {
		t.Fatalf("got %d contents, want 2", len(g.Contents))
	}
	// Assistant tool_use -> role:model functionCall with OBJECT args (passed through).
	modelC := g.Contents[0]
	if modelC.Role != "model" || len(modelC.Parts) != 1 || modelC.Parts[0].FunctionCall == nil {
		t.Fatalf("model content = %+v", modelC)
	}
	fc := modelC.Parts[0].FunctionCall
	if fc.Name != "get_weather" || string(fc.Args) != `{"city":"SF"}` {
		t.Errorf("functionCall = %+v (args %s)", fc, fc.Args)
	}
	// tool_result -> role:user functionResponse (response is an OBJECT) naming the fn.
	userC := g.Contents[1]
	if userC.Role != "user" || len(userC.Parts) != 1 || userC.Parts[0].FunctionResponse == nil {
		t.Fatalf("user content = %+v", userC)
	}
	fr := userC.Parts[0].FunctionResponse
	if fr.Name != "get_weather" {
		t.Errorf("functionResponse name = %q, want get_weather (resolved from tool_use id)", fr.Name)
	}
	var obj map[string]string
	if err := json.Unmarshal(fr.Response, &obj); err != nil {
		t.Fatalf("functionResponse.response is not an object: %s", fr.Response)
	}
	if obj["result"] != "18C" {
		t.Errorf("functionResponse.response = %v, want result=18C", obj)
	}
}

func TestToGeminiRequest_SchemaPassthrough(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)
	req := &anthropic.Request{
		Model:      "m",
		MaxTokens:  10,
		Messages:   []anthropic.Message{userText("weather?")},
		Tools:      []anthropic.Tool{{Name: "get_weather", Description: "gets weather", InputSchema: schema}},
		ToolChoice: &anthropic.ToolChoice{Type: "tool", Name: "get_weather"},
	}
	g, err := ToGeminiRequest(req, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Tools) != 1 || len(g.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %+v", g.Tools)
	}
	decl := g.Tools[0].FunctionDeclarations[0]
	if decl.Name != "get_weather" || decl.Description != "gets weather" {
		t.Errorf("decl = %+v", decl)
	}
	// The Anthropic input_schema must be passed through verbatim via parametersJsonSchema.
	if string(decl.ParametersJsonSchema) != string(schema) {
		t.Errorf("parametersJsonSchema = %s, want verbatim %s", decl.ParametersJsonSchema, schema)
	}
	if g.ToolConfig == nil || g.ToolConfig.FunctionCallingConfig == nil {
		t.Fatalf("toolConfig = %+v", g.ToolConfig)
	}
	fcc := g.ToolConfig.FunctionCallingConfig
	if fcc.Mode != "ANY" || len(fcc.AllowedFunctionNames) != 1 || fcc.AllowedFunctionNames[0] != "get_weather" {
		t.Errorf("functionCallingConfig = %+v", fcc)
	}
}

func TestToAnthropicResponseFromGemini_Text(t *testing.T) {
	g := &gemini.GenerateContentResponse{
		Candidates: []gemini.Candidate{{
			Content:      gemini.Content{Role: "model", Parts: []gemini.Part{{Text: "hello world"}}},
			FinishReason: "STOP",
		}},
		UsageMetadata: &gemini.UsageMetadata{PromptTokenCount: 3, CandidatesTokenCount: 2},
	}
	a, err := ToAnthropicResponseFromGemini(g, "claude-x")
	if err != nil {
		t.Fatal(err)
	}
	if a.Type != "message" || a.Role != "assistant" || a.Model != "claude-x" {
		t.Errorf("envelope = %+v", a)
	}
	if len(a.Content) != 1 || a.Content[0].Type != "text" || a.Content[0].Text != "hello world" {
		t.Errorf("content = %+v", a.Content)
	}
	if a.StopReason != "end_turn" {
		t.Errorf("stop_reason = %q, want end_turn", a.StopReason)
	}
	if a.Usage.InputTokens != 3 || a.Usage.OutputTokens != 2 {
		t.Errorf("usage = %+v", a.Usage)
	}
}

func TestToAnthropicResponseFromGemini_FunctionCall(t *testing.T) {
	g := &gemini.GenerateContentResponse{
		Candidates: []gemini.Candidate{{
			Content: gemini.Content{Role: "model", Parts: []gemini.Part{{
				FunctionCall: &gemini.FunctionCall{Name: "get_weather", Args: json.RawMessage(`{"city":"SF"}`)},
			}}},
			FinishReason: "STOP",
		}},
	}
	a, err := ToAnthropicResponseFromGemini(g, "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Content) != 1 || a.Content[0].Type != "tool_use" {
		t.Fatalf("content = %+v", a.Content)
	}
	tu := a.Content[0]
	if tu.Name != "get_weather" || string(tu.Input) != `{"city":"SF"}` {
		t.Errorf("tool_use = %+v (input %s)", tu, tu.Input)
	}
	if tu.ID == "" {
		t.Error("tool_use must have a generated id when Gemini omits one")
	}
	// functionCall present overrides STOP -> tool_use.
	if a.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", a.StopReason)
	}
}

func TestToAnthropicResponseFromGemini_BlockedPromptIsRefusal(t *testing.T) {
	g := &gemini.GenerateContentResponse{
		PromptFeedback: &gemini.PromptFeedback{BlockReason: "SAFETY"},
	}
	a, err := ToAnthropicResponseFromGemini(g, "m")
	if err != nil {
		t.Fatalf("blocked prompt must not error: %v", err)
	}
	if a.StopReason != "refusal" {
		t.Errorf("stop_reason = %q, want refusal", a.StopReason)
	}
	if a.Content == nil {
		t.Error("content must be a non-nil (empty) slice")
	}
}

func TestToAnthropicResponseFromGemini_NoCandidatesErrors(t *testing.T) {
	g := &gemini.GenerateContentResponse{}
	if _, err := ToAnthropicResponseFromGemini(g, "m"); err == nil {
		t.Error("empty response with no block reason should error")
	}
}

func TestMapGeminiFinishReason(t *testing.T) {
	cases := []struct {
		reason string
		hasFC  bool
		want   string
	}{
		{"STOP", false, "end_turn"},
		{"MAX_TOKENS", false, "max_tokens"},
		{"STOP", true, "tool_use"},
		{"SAFETY", false, "refusal"},
		{"RECITATION", false, "refusal"},
		{"BLOCKLIST", false, "refusal"},
		{"PROHIBITED_CONTENT", false, "refusal"},
		{"UNKNOWN", false, "end_turn"},
	}
	for _, c := range cases {
		if got := MapGeminiFinishReason(c.reason, c.hasFC); got != c.want {
			t.Errorf("MapGeminiFinishReason(%q, %v) = %q, want %q", c.reason, c.hasFC, got, c.want)
		}
	}
}
