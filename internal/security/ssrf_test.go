package security

import (
	"net"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "127.1.2.3", // loopback
		"10.0.0.1", "172.16.5.4", "192.168.1.1", // RFC1918
		"169.254.169.254",        // IPv4 cloud metadata (link-local)
		"0.0.0.0",                // unspecified
		"100.64.0.1",             // CGNAT
		"224.0.0.1",              // multicast
		"::1",                    // IPv6 loopback
		"fe80::1",                // IPv6 link-local
		"fc00::1",                // IPv6 ULA
		"fd00:ec2::254",          // IPv6 cloud metadata (ULA)
		"::ffff:127.0.0.1",       // IPv4-mapped loopback
		"::ffff:169.254.169.254", // IPv4-mapped metadata
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if !IsBlockedIP(ip) {
			t.Errorf("IsBlockedIP(%s) = false, want true (should be blocked)", s)
		}
	}

	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34", // public IPv4
		"2606:4700:4700::1111", // public IPv6 (Cloudflare)
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test IP %q", s)
		}
		if IsBlockedIP(ip) {
			t.Errorf("IsBlockedIP(%s) = true, want false (public address)", s)
		}
	}

	if !IsBlockedIP(nil) {
		t.Error("IsBlockedIP(nil) should be true")
	}
}

func TestDialPolicyAllowHosts(t *testing.T) {
	p := DialPolicy{AllowHosts: map[string]bool{"localhost": true}}
	if !p.allowed("localhost") || !p.allowed("LOCALHOST") {
		t.Error("allowed() should be case-insensitive and match configured host")
	}
	if p.allowed("evil.example") {
		t.Error("allowed() should not match unconfigured host")
	}
}
