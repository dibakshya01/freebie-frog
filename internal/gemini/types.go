// Package gemini holds the Google Generative Language API (v1beta) wire types that
// freebie-frog speaks on its egress side when routing to Gemini natively. Built from
// the public API specification only (see docs/SOURCES.md).
package gemini

import "encoding/json"

// GenerateContentRequest is an outgoing :generateContent / :streamGenerateContent
// request body (we build these).
type GenerateContentRequest struct {
	Contents          []Content         `json:"contents"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	Tools             []Tool            `json:"tools,omitempty"`
	ToolConfig        *ToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
}

// Content is a conversation turn (role user|model) or the system instruction
// (no role). Parts carry the typed payload.
type Content struct {
	Role  string `json:"role,omitempty"` // "user" | "model" (omitted for systemInstruction)
	Parts []Part `json:"parts,omitempty"`
}

// Part is a union over Gemini content part types. Unused fields stay zero and are
// omitted on marshal; presence is checked via nil/empty on decode.
type Part struct {
	Text             string            `json:"text,omitempty"`
	InlineData       *Blob             `json:"inlineData,omitempty"`
	FileData         *FileData         `json:"fileData,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`

	// Thinking signals (Gemini 2.5/3). We drop thinking on egress and guard these
	// on ingress; thoughtSignature is preserved only by being ignored safely.
	Thought          bool   `json:"thought,omitempty"`
	ThoughtSignature string `json:"thoughtSignature,omitempty"`
}

// Blob is inline binary data (e.g. an image) with its media type and base64 payload.
type Blob struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// FileData references binary data by URI.
type FileData struct {
	MimeType string `json:"mimeType,omitempty"`
	FileURI  string `json:"fileUri"`
}

// FunctionCall is a model-emitted tool call. Args is a JSON OBJECT (not a string).
type FunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// FunctionResponse carries a tool result back to the model. Response is a JSON OBJECT.
type FunctionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

// Tool groups function declarations (and, upstream, other tool types we don't use).
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

// FunctionDeclaration describes one callable function. We pass the Anthropic
// input_schema straight through ParametersJsonSchema (the escape hatch), avoiding
// OpenAPI-subset translation.
type FunctionDeclaration struct {
	Name                 string          `json:"name"`
	Description          string          `json:"description,omitempty"`
	ParametersJsonSchema json.RawMessage `json:"parametersJsonSchema,omitempty"`
}

// ToolConfig controls function calling behaviour.
type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

// FunctionCallingConfig sets the calling mode (AUTO|ANY|NONE) and optional allow-list.
type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

// GenerationConfig holds sampling and output controls.
type GenerationConfig struct {
	Temperature     *float64 `json:"temperature,omitempty"`
	TopP            *float64 `json:"topP,omitempty"`
	TopK            *int     `json:"topK,omitempty"`
	MaxOutputTokens int      `json:"maxOutputTokens,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

// GenerateContentResponse is a non-streaming or per-chunk response (we parse these).
type GenerateContentResponse struct {
	Candidates     []Candidate     `json:"candidates,omitempty"`
	PromptFeedback *PromptFeedback `json:"promptFeedback,omitempty"`
	UsageMetadata  *UsageMetadata  `json:"usageMetadata,omitempty"`
}

// Candidate is one generation candidate.
type Candidate struct {
	Content       Content        `json:"content"`
	FinishReason  string         `json:"finishReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
	Index         int            `json:"index,omitempty"`
}

// PromptFeedback reports that the prompt itself was blocked (zero candidates).
type PromptFeedback struct {
	BlockReason   string         `json:"blockReason,omitempty"`
	SafetyRatings []SafetyRating `json:"safetyRatings,omitempty"`
}

// SafetyRating is a per-category safety score.
type SafetyRating struct {
	Category    string `json:"category,omitempty"`
	Probability string `json:"probability,omitempty"`
	Blocked     bool   `json:"blocked,omitempty"`
}

// UsageMetadata is token accounting.
type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount,omitempty"`
	CandidatesTokenCount int `json:"candidatesTokenCount,omitempty"`
	TotalTokenCount      int `json:"totalTokenCount,omitempty"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}

// ErrorEnvelope is the Google API error body: {"error":{"code","message","status"}}.
type ErrorEnvelope struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}
