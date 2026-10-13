package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// withAuth enforces the ingress auth token on API routes when one is configured.
// /healthz is always open. When no token is set (loopback local trust), it is a
// no-op.
func (s *Server) withAuth(next http.Handler) http.Handler {
	token := s.cfg.AuthToken
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" || r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		if !tokenMatches(r, token) {
			writeAnthropicError(w, http.StatusUnauthorized, "authentication_error", "missing or invalid auth token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenMatches(r *http.Request, token string) bool {
	if v := r.Header.Get("x-api-key"); v != "" && constantEqual(v, token) {
		return true
	}
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		if constantEqual(strings.TrimPrefix(v, "Bearer "), token) {
			return true
		}
	}
	return false
}

func constantEqual(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
