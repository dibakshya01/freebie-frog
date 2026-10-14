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

	"github.com/dibakshya01/freebie-frog/internal/admin"
	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/metrics"
	"github.com/dibakshya01/freebie-frog/internal/router"
	"github.com/dibakshya01/freebie-frog/internal/security"
	"github.com/dibakshya01/freebie-frog/internal/version"
)

// Deps are the runtime dependencies the ingress handlers need. They may be zero
// (e.g. in the M0 health-only configuration or tests), in which case the Messages
// API endpoints report that no providers are configured.
type Deps struct {
	Dispatcher   *router.Dispatcher
	Registry     *router.Registry
	Config       *config.Config     // full config, for the read-only admin dashboard
	Metrics      *metrics.Collector // request metrics for the dashboard (nil-safe)
	AdminManager *admin.Manager     // non-nil enables admin write endpoints
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
	if !s.cfg.DisableAdmin && s.deps.Config != nil {
		adminH := security.AdminGuard(admin.Handler(admin.Options{
			Config:  s.deps.Config,
			Metrics: s.deps.Metrics,
			Manager: s.deps.AdminManager,
		}))
		mux.Handle("/admin/", adminH)
		mux.Handle("/admin", adminH)
	}
	return s.withRequestLogging(s.withAuth(mux))
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
			"path", security.NeutralizeNewlines(r.URL.Path),
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

// Flush forwards to the underlying ResponseWriter so SSE events are delivered in
// real time. Without this, embedding ResponseWriter as an interface hides Flush and
// the stream is buffered (breaking live streaming). See also Unwrap.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap exposes the underlying ResponseWriter to http.ResponseController and to
// any interface checks (e.g. http.Flusher) that look through wrappers.
func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
