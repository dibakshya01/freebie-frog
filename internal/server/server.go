// Package server implements the freebie-frog HTTP server.
//
// M0 exposes only operational endpoints (health). The Anthropic-compatible
// Messages API surface is added in later milestones.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/router"
	"github.com/dibakshya01/freebie-frog/internal/version"
)

// Deps are the runtime dependencies the ingress handlers need. They may be zero
// (e.g. in the M0 health-only configuration or tests), in which case the Messages
// API endpoints report that no providers are configured.
type Deps struct {
	Dispatcher *router.Dispatcher
	Registry   *router.Registry
}

// Server wraps an *http.Server with freebie-frog's routes and middleware.
type Server struct {
	cfg  config.ServerConfig
	log  *slog.Logger
	deps Deps
	http *http.Server
}

// New constructs a Server ready to Run.
func New(cfg config.ServerConfig, log *slog.Logger, deps Deps) *Server {
	s := &Server{cfg: cfg, log: log, deps: deps}
	s.http = &http.Server{
		Addr:         cfg.Addr,
		Handler:      s.Handler(),
		ReadTimeout:  cfg.ReadTimeout.Std(),
		WriteTimeout: cfg.WriteTimeout.Std(),
	}
	return s
}

// Handler builds the HTTP handler (routes + middleware). Exposed for testing.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /v1/messages", s.handleMessages)
	mux.HandleFunc("POST /v1/messages/count_tokens", s.handleCountTokens)
	mux.HandleFunc("GET /v1/models", s.handleModels)
	return s.withRequestLogging(mux)
}

// Run starts listening and blocks until ctx is cancelled, then shuts down
// gracefully within the configured shutdown timeout.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.log.Info("server listening", "addr", s.cfg.Addr)
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout.Std())
		defer cancel()
		return s.http.Shutdown(shutdownCtx)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "freebie-frog",
		"version": version.Version,
	})
}

func (s *Server) withRequestLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", r.RemoteAddr,
		)
	})
}

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
