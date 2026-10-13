package config

import "fmt"

// Provider configures one upstream model provider.
type Provider struct {
	Type       string         `json:"type"`                  // openai | gemini | anthropic
	BaseURL    string         `json:"base_url"`              // API root, e.g. https://api.groq.com/openai/v1
	APIKey     string         `json:"api_key,omitempty"`     // literal key (discouraged; prefer api_key_ref)
	APIKeyRef  string         `json:"api_key_ref,omitempty"` // secret reference (env:NAME, keychain:NAME, file, ...)
	Models     []string       `json:"models,omitempty"`      // advertised model ids (for /v1/models & discovery)
	AllowLocal bool           `json:"allow_local,omitempty"` // permit this provider's host to resolve to a private/loopback IP
	Quirks     ProviderQuirks `json:"quirks,omitempty"`
}

// ProviderQuirks captures per-provider deviations from the OpenAI spec.
type ProviderQuirks struct {
	UseMaxCompletionTokens bool `json:"use_max_completion_tokens,omitempty"`
	StreamUsage            bool `json:"stream_usage,omitempty"`
	DisableImages          bool `json:"disable_images,omitempty"`
}

// ModelRouting maps an incoming model id/tier to an ordered list of
// "provider/model" targets (tried in order; fallback stops at the first success).
type ModelRouting struct {
	Default []string            `json:"default,omitempty"` // fallback chain for unmatched models
	Tiers   map[string][]string `json:"tiers,omitempty"`   // opus|sonnet|haiku|fable -> targets
	Aliases map[string][]string `json:"aliases,omitempty"` // exact model id -> targets
}

var validProviderTypes = map[string]bool{"openai": true, "gemini": true, "anthropic": true}

func (c Config) validateProviders() []error {
	var errs []error
	for name, p := range c.Providers {
		if !validProviderTypes[p.Type] {
			errs = append(errs, fmt.Errorf("providers.%s.type %q is invalid (want openai|gemini|anthropic)", name, p.Type))
		}
		if p.BaseURL == "" && p.Type != "anthropic" {
			errs = append(errs, fmt.Errorf("providers.%s.base_url must not be empty", name))
		}
	}
	return errs
}
