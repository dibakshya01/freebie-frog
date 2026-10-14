// Package router builds provider adapters from configuration and routes incoming
// models/tiers to an ordered list of provider targets (with fallback in M4).
package router

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/provider"
	"github.com/dibakshya01/freebie-frog/internal/secret"
	"github.com/dibakshya01/freebie-frog/internal/security"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// Registry holds the configured adapters and the model routing table.
type Registry struct {
	adapters map[string]provider.Adapter
	models   map[string][]string // provider name -> advertised model ids
	routing  config.ModelRouting
}

// Build constructs adapters from config, resolving keys via the secret store and
// wiring an SSRF-guarded HTTP client for each provider.
func Build(cfg *config.Config, store *secret.Store) (*Registry, error) {
	reg := &Registry{
		adapters: make(map[string]provider.Adapter),
		models:   make(map[string][]string),
		routing:  cfg.Models,
	}
	for name, pc := range cfg.Providers {
		key := pc.APIKey
		if key == "" && pc.APIKeyRef != "" {
			k, err := store.Resolve(pc.APIKeyRef)
			if err != nil {
				return nil, fmt.Errorf("provider %q: resolve api key: %w", name, err)
			}
			key = k
		}
		// Register the resolved key so logs/error text scrub it by value, not just shape.
		if key != "" {
			security.RegisterSecrets(key)
		}
		client, err := buildClient(pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}

		var ad provider.Adapter
		switch pc.Type {
		case "openai":
			ad = provider.NewOpenAICompatible(name, pc.BaseURL, key, client, quirksOf(pc))
		case "gemini":
			ad = provider.NewGeminiAdapter(name, pc.BaseURL, key, client)
		case "anthropic":
			ad = provider.NewPassthroughAdapter(name, pc.BaseURL, key, client)
		default:
			return nil, fmt.Errorf("provider %q: unknown type %q", name, pc.Type)
		}
		reg.adapters[name] = ad
		reg.models[name] = pc.Models
	}
	return reg, nil
}

// Adapter returns the adapter registered under name.
func (r *Registry) Adapter(name string) (provider.Adapter, bool) {
	a, ok := r.adapters[name]
	return a, ok
}

// Route maps an incoming model id to an ordered list of targets: exact alias first,
// then tier match (opus/sonnet/haiku/fable by substring), then the default chain.
func (r *Registry) Route(model string) []provider.Target {
	if specs, ok := r.routing.Aliases[model]; ok {
		return parseTargets(specs)
	}
	lower := strings.ToLower(model)
	for _, tier := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.Contains(lower, tier) {
			if specs, ok := r.routing.Tiers[tier]; ok {
				return parseTargets(specs)
			}
		}
	}
	return parseTargets(r.routing.Default)
}

// AdvertisedModels returns the sorted set of model ids the proxy exposes.
func (r *Registry) AdvertisedModels() []string {
	seen := map[string]bool{}
	for _, ms := range r.models {
		for _, m := range ms {
			seen[m] = true
		}
	}
	for m := range r.routing.Aliases {
		seen[m] = true
	}
	out := make([]string, 0, len(seen))
	for m := range seen {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func parseTargets(specs []string) []provider.Target {
	out := make([]provider.Target, 0, len(specs))
	for _, s := range specs {
		prov, model, found := strings.Cut(s, "/")
		if !found || prov == "" || model == "" {
			continue
		}
		out = append(out, provider.Target{Provider: prov, Model: model})
	}
	return out
}

func quirksOf(pc config.Provider) translate.Quirks {
	return translate.Quirks{
		UseMaxCompletionTokens: pc.Quirks.UseMaxCompletionTokens,
		StreamUsage:            pc.Quirks.StreamUsage,
		DisableImages:          pc.Quirks.DisableImages,
	}
}

func buildClient(pc config.Provider) (*http.Client, error) {
	policy := security.DialPolicy{}
	if pc.AllowLocal && pc.BaseURL != "" {
		host, err := hostOf(pc.BaseURL)
		if err != nil {
			return nil, err
		}
		// Scope the opt-in to exactly this provider's configured host; every other
		// host is still fully SSRF-validated at dial time.
		policy.AllowHosts = map[string]bool{strings.ToLower(host): true}
	}
	return security.NewHTTPClient(policy), nil
}

func hostOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid base_url %q: %w", raw, err)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("base_url %q has no host", raw)
	}
	return u.Hostname(), nil
}
