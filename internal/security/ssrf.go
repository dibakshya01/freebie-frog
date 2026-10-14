// Package security provides SSRF protection, secret redaction, and the guarded
// HTTP client used for all outbound provider calls (OWASP API7). See
// docs/THREAT_MODEL.md.
package security

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// extraBlockedCIDRs covers ranges not already caught by net.IP's classifiers
// (loopback, private incl. ULA fc00::/7, link-local incl. 169.254/16 & fe80::/10,
// multicast, unspecified). Carrier-grade NAT is included defensively.
var extraBlockedCIDRs = []string{
	"0.0.0.0/8",
	"100.64.0.0/10",  // CGNAT
	"192.0.0.0/24",   // IETF protocol assignments
	"240.0.0.0/4",    // reserved
	"::/96",          // deprecated IPv4-compatible IPv6
	"64:ff9b::/96",   // NAT64 (could map to internal IPv4, incl. metadata)
	"64:ff9b:1::/48", // NAT64 local-use
}

var extraBlockedNets = mustParseCIDRs(extraBlockedCIDRs)

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("security: bad CIDR " + c + ": " + err.Error())
		}
		nets = append(nets, n)
	}
	return nets
}

// IsBlockedIP reports whether connecting to ip would be an SSRF risk and is refused
// by default. Covers IPv4 and IPv6 (incl. IPv4-mapped, which To4 normalizes, and the
// cloud-metadata addresses 169.254.169.254 / fd00:ec2::254, caught as link-local / ULA).
func IsBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() ||
		ip.IsPrivate() || // RFC1918 + ULA fc00::/7
		ip.IsLinkLocalUnicast() || // 169.254/16 + fe80::/10 (cloud metadata)
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() {
		return true
	}
	for _, n := range extraBlockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// DialPolicy decides which hosts may be dialed. By default every resolved IP is
// validated against IsBlockedIP; hosts in AllowHosts (a lowercased host without
// port, e.g. "localhost" or "127.0.0.1") may resolve to otherwise-blocked
// addresses, for local models. Validation still happens at dial time against the
// resolved IP, so a rebinding to a *different* blocked IP is still refused unless
// the host was explicitly allowed.
type DialPolicy struct {
	AllowHosts map[string]bool
	Resolver   *net.Resolver // nil = net.DefaultResolver
}

func (p DialPolicy) allowed(host string) bool {
	return p.AllowHosts[strings.ToLower(host)]
}

// DialContext resolves the host, validates every candidate IP at dial time (which
// defeats DNS rebinding), and connects only to a permitted, validated address.
func (p DialPolicy) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	allowed := p.allowed(host)

	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}

	var dialer net.Dialer
	var lastErr error
	for _, ipAddr := range ips {
		if IsBlockedIP(ipAddr.IP) && !allowed {
			lastErr = fmt.Errorf("ssrf guard: refusing to connect to blocked address %s (host %q)", ipAddr.IP, host)
			continue
		}
		conn, derr := dialer.DialContext(ctx, network, net.JoinHostPort(ipAddr.IP.String(), port))
		if derr == nil {
			return conn, nil
		}
		lastErr = derr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("ssrf guard: no permitted address for host %q", host)
	}
	return nil, lastErr
}
