// Package translate converts between the Anthropic Messages API and provider wire
// formats (OpenAI Chat Completions, and Gemini in a later milestone), including the
// streaming SSE reconstruction. This is the correctness-critical core of the proxy.
package translate

// Quirks captures per-provider deviations from the OpenAI spec that the translator
// must accommodate (see docs/SOURCES.md §"provider deviations").
type Quirks struct {
	// UseMaxCompletionTokens sends max_completion_tokens instead of max_tokens
	// (OpenAI o-series / newest models). Default false = send max_tokens (broadest
	// provider support).
	UseMaxCompletionTokens bool
	// StreamUsage asks the provider for usage in the stream (stream_options).
	StreamUsage bool
	// DisableImages drops image content for text-only providers.
	DisableImages bool
}
