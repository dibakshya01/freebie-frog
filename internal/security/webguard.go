package security

import (
	"net/http"
	"strings"
)

// loopbackHostnames are the Host header values accepted by the admin UI. A browser
// tricked by DNS rebinding still sends the attacker's original hostname in Host, so
// a port-stripped allowlist of loopback names blocks it (cf. CVE-2025-66414).
var loopbackHostnames = map[string]bool{
	"localhost": true,
	"127.0.0.1": true,
	"::1":       true,
}

func hostWithoutPort(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") { // [::1]:port
		if i := strings.Index(host, "]"); i >= 0 {
			return host[1:i]
		}
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		// Only treat as host:port if what follows is a port (no extra colons).
		if !strings.Contains(host[i+1:], ":") {
			return host[:i]
		}
	}
	return host
}

// AdminGuard protects the local admin UI: it rejects requests whose Host header is
// not a loopback name (DNS-rebinding defense) and, for state-changing methods,
// requires a same-site fetch plus a custom header (CSRF defense). Safe methods on a
// loopback host pass through.
func AdminGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHostnames[hostWithoutPort(r.Host)] {
			http.Error(w, "forbidden: admin UI is loopback-only", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !csrfOK(r) {
				http.Error(w, "forbidden: cross-site request blocked", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func csrfOK(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "cross-site":
		return false
	case "same-origin", "same-site", "none":
		// fall through to the custom-header requirement
	case "":
		// Older browsers omit Sec-Fetch-Site; rely on the custom header below.
	default:
		return false
	}
	// A custom header can't be set cross-origin without a CORS preflight the server
	// never grants, so its presence proves a same-origin request.
	return r.Header.Get("X-Requested-With") != ""
}
