package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func TestHostWithoutPort(t *testing.T) {
	cases := map[string]string{
		"localhost:4000": "localhost",
		"127.0.0.1:4000": "127.0.0.1",
		"[::1]:4000":     "::1",
		"localhost":      "localhost",
		"evil.com:8080":  "evil.com",
	}
	for in, want := range cases {
		if got := hostWithoutPort(in); got != want {
			t.Errorf("hostWithoutPort(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAdminGuardHostAllowlist(t *testing.T) {
	h := AdminGuard(okHandler())

	for _, host := range []string{"localhost:4000", "127.0.0.1:4000", "[::1]:4000"} {
		r := httptest.NewRequest(http.MethodGet, "/admin/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("host %q: code = %d, want 200", host, w.Code)
		}
	}
	for _, host := range []string{"evil.com", "attacker.example:4000"} {
		r := httptest.NewRequest(http.MethodGet, "/admin/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("host %q: code = %d, want 403", host, w.Code)
		}
	}
}

func TestAdminGuardCSRF(t *testing.T) {
	h := AdminGuard(okHandler())

	// Cross-site POST -> blocked
	r := httptest.NewRequest(http.MethodPost, "/admin/api/x", nil)
	r.Host = "localhost:4000"
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-site POST code = %d, want 403", w.Code)
	}

	// Same-origin POST without custom header -> blocked
	r2 := httptest.NewRequest(http.MethodPost, "/admin/api/x", nil)
	r2.Host = "localhost:4000"
	r2.Header.Set("Sec-Fetch-Site", "same-origin")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusForbidden {
		t.Errorf("same-origin POST w/o custom header code = %d, want 403", w2.Code)
	}

	// Same-origin POST with custom header -> allowed
	r3 := httptest.NewRequest(http.MethodPost, "/admin/api/x", nil)
	r3.Host = "localhost:4000"
	r3.Header.Set("Sec-Fetch-Site", "same-origin")
	r3.Header.Set("X-Requested-With", "fetch")
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, r3)
	if w3.Code != http.StatusOK {
		t.Errorf("same-origin POST w/ custom header code = %d, want 200", w3.Code)
	}
}
