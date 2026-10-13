package security

import (
	"strings"
	"testing"
)

func TestRedactor(t *testing.T) {
	r := NewRedactor("gsk_supersecretvalue", "x") // "x" too short, ignored
	in := "key gsk_supersecretvalue and sk-abcdef123456 and Bearer tok12345 and AIzaSyA1234567890xyz done"
	out := r.Redact(in)
	for _, leaked := range []string{"gsk_supersecretvalue", "sk-abcdef123456", "AIzaSyA1234567890xyz"} {
		if strings.Contains(out, leaked) {
			t.Errorf("redacted output still contains %q: %s", leaked, out)
		}
	}
	if !strings.Contains(out, "***") {
		t.Errorf("expected redaction marker in %q", out)
	}
	if !strings.Contains(out, "done") {
		t.Errorf("non-secret text should survive: %q", out)
	}
}

func TestIsSensitiveHeader(t *testing.T) {
	for _, h := range []string{"Authorization", "x-api-key", "X-API-KEY", "x-goog-api-key", "Cookie"} {
		if !IsSensitiveHeader(h) {
			t.Errorf("%q should be sensitive", h)
		}
	}
	for _, h := range []string{"Accept", "Content-Type", "User-Agent"} {
		if IsSensitiveHeader(h) {
			t.Errorf("%q should not be sensitive", h)
		}
	}
}

func TestNeutralizeNewlines(t *testing.T) {
	got := NeutralizeNewlines("line1\r\nline2\nline3")
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("newlines not neutralized: %q", got)
	}
}

func TestIsLoopbackBind(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:4000": true,
		"localhost:4000": true,
		"[::1]:4000":     true,
		":4000":          false,
		"0.0.0.0:4000":   false,
		"1.2.3.4:4000":   false,
	}
	for addr, want := range cases {
		if got := IsLoopbackBind(addr); got != want {
			t.Errorf("IsLoopbackBind(%q) = %v, want %v", addr, got, want)
		}
	}
}
