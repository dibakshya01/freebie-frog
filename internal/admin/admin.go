// Package admin serves the local admin dashboard: a read-only view of the proxy's
// configured providers (keys masked), model routing, server info, and live request
// metrics. It is mounted only on loopback and protected by security.AdminGuard.
//
// Optional management endpoints (editing providers and routing from the browser) are
// enabled only when a *Manager is supplied — i.e. when server.admin_write is set.
// Those writes are still loopback-only and CSRF-guarded, never carry secret values,
// and require a restart to take effect.
package admin

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/metrics"
	"github.com/dibakshya01/freebie-frog/internal/version"
)

//go:embed assets/*
var assets embed.FS

// State is the redacted snapshot the dashboard renders. It never contains secrets.
type State struct {
	Version      string         `json:"version"`
	WriteEnabled bool           `json:"write_enabled"`
	Server       ServerInfo     `json:"server"`
	Providers    []ProviderInfo `json:"providers"`
	Routing      RoutingInfo    `json:"routing"`
}

// ServerInfo describes the listener.
type ServerInfo struct {
	Addr         string `json:"addr"`
	AuthRequired bool   `json:"auth_required"`
}

// ProviderInfo is a provider with its key presence (never its value).
type ProviderInfo struct {
	Name          string                `json:"name"`
	Type          string                `json:"type"`
	BaseURL       string                `json:"base_url"`
	Models        []string              `json:"models"`
	KeyConfigured bool                  `json:"key_configured"`
	KeyRef        string                `json:"key_ref,omitempty"` // the reference (e.g. env:NAME), not the secret
	AllowLocal    bool                  `json:"allow_local"`
	Quirks        config.ProviderQuirks `json:"quirks,omitempty"`
}

// RoutingInfo is the model routing table.
type RoutingInfo struct {
	Default []string            `json:"default"`
	Tiers   map[string][]string `json:"tiers"`
	Aliases map[string][]string `json:"aliases"`
}

// BuildState builds a redacted State from config. Secrets are reduced to a boolean
// plus their (non-secret) reference string.
func BuildState(cfg *config.Config) State {
	s := State{
		Version: version.Version,
		Server:  ServerInfo{Addr: cfg.Server.Addr, AuthRequired: cfg.Server.AuthToken != ""},
		Routing: RoutingInfo{Default: cfg.Models.Default, Tiers: cfg.Models.Tiers, Aliases: cfg.Models.Aliases},
	}
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		p := cfg.Providers[n]
		s.Providers = append(s.Providers, ProviderInfo{
			Name:          n,
			Type:          p.Type,
			BaseURL:       p.BaseURL,
			Models:        p.Models,
			KeyConfigured: p.APIKey != "" || p.APIKeyRef != "",
			KeyRef:        p.APIKeyRef,
			AllowLocal:    p.AllowLocal,
			Quirks:        p.Quirks,
		})
	}
	return s
}

// Options configures the admin handler.
type Options struct {
	Config  *config.Config     // read-only snapshot source (required)
	Metrics *metrics.Collector // request metrics (nil-safe)
	Manager *Manager           // non-nil enables the write endpoints
}

// Handler returns the admin routes (to be mounted under AdminGuard).
func Handler(opts Options) http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic("admin: embedded assets: " + err.Error())
	}
	mux := http.NewServeMux()
	mux.Handle("GET /admin/assets/", http.StripPrefix("/admin/assets/", http.FileServer(http.FS(sub))))

	mux.HandleFunc("GET /admin/api/state", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, currentState(opts))
	})
	mux.HandleFunc("GET /admin/api/stats", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, opts.Metrics.Snapshot())
	})

	if opts.Manager != nil {
		mux.HandleFunc("PUT /admin/api/providers/{name}", opts.Manager.handlePutProvider)
		mux.HandleFunc("DELETE /admin/api/providers/{name}", opts.Manager.handleDeleteProvider)
		mux.HandleFunc("PUT /admin/api/routing", opts.Manager.handlePutRouting)
	}

	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, sub, "index.html")
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	return mux
}

func currentState(opts Options) State {
	if opts.Manager != nil {
		return opts.Manager.State()
	}
	return BuildState(opts.Config)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
