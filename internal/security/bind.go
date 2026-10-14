package security

import "net"

// IsLoopbackBind reports whether a listen address binds only to the loopback
// interface. A wildcard bind (e.g. ":4000" or "0.0.0.0:4000") is NOT loopback.
func IsLoopbackBind(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false // wildcard bind
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
