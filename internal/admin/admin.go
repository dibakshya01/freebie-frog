// Package admin serves a read-only local dashboard showing the proxy's configured
// providers (with keys masked), model routing, and server info. It is mounted only
// on loopback and protected by security.AdminGuard. Configuration edits are done in
// config.json (see README); editing from the UI is a documented future enhancement.
package admin

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"sort"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/version"
)

//go:embed assets/*
var assets embed.FS

// State is the redacted snapshot the dashboard renders. It never contains secrets.
type State struct {
	Version   string         `json:"version"`
	Server    ServerInfo     `json:"server"`
	Providers []ProviderInfo `json:"providers"`
	Routing   RoutingInfo    `json:"routing"`
}

// ServerInfo describes the listener.
type ServerInfo struct {
	Addr         string `json:"addr"`
	AuthRequired bool   `json:"auth_required"`
}

// ProviderInfo is a provider with its key presence (never its value).
type ProviderInfo struct {
	Name          string   `json:"name"`
	Type          string   `json:"type"`
	BaseURL       string   `json:"base_url"`
	Models        []string `json:"models"`
	KeyConfigured bool     `json:"key_configured"`
	AllowLocal    bool     `json:"allow_local"`
}

// RoutingInfo is the model routing table.
type RoutingInfo struct {
	Default []string            `json:"default"`
	Tiers   map[string][]string `json:"tiers"`
	Aliases map[string][]string `json:"aliases"`
}

// BuildState builds a redacted State from config. Secrets are reduced to a boolean.
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
			AllowLocal:    p.AllowLocal,
		})
	}
	return s
}

// Handler returns the admin routes (to be mounted under AdminGuard). cfg is captured
// for the read-only snapshot.
func Handler(cfg *config.Config) http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic("admin: embedded assets: " + err.Error())
	}
	mux := http.NewServeMux()
	mux.Handle("GET /admin/assets/", http.StripPrefix("/admin/assets/", http.FileServer(http.FS(sub))))
	mux.HandleFunc("GET /admin/api/state", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(BuildState(cfg))
	})
	mux.HandleFunc("GET /admin/{$}", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, sub, "index.html")
	})
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	return mux
}
