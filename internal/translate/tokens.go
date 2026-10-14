package translate

import (
	"github.com/dibakshya01/freebie-frog/internal/anthropic"
)

// EstimateTokens is a rough, provider-agnostic token estimate (~4 chars/token).
// It is explicitly approximate — see docs/CAPABILITIES.md.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 3) / 4
}

// EstimateRequestTokens estimates the input tokens of an Anthropic request by
// summing its textual content. Used when a provider omits usage.
func EstimateRequestTokens(a *anthropic.Request) int {
	total := EstimateTokens(anthropic.SystemText(a.System))
	for _, m := range a.Messages {
		for _, b := range m.Content {
			switch b.Type {
			case "text":
				total += EstimateTokens(b.Text)
			case "tool_use":
				total += EstimateTokens(string(b.Input)) + EstimateTokens(b.Name)
			case "tool_result":
				total += EstimateTokens(toolResultText(b.Content))
			case "thinking":
				total += EstimateTokens(b.Thinking)
			}
		}
	}
	for _, t := range a.Tools {
		total += EstimateTokens(t.Name) + EstimateTokens(t.Description) + EstimateTokens(string(t.InputSchema))
	}
	if total < 1 {
		total = 1
	}
	return total
}
