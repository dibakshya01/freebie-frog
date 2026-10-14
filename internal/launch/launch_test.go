package launch

import (
	"slices"
	"testing"
)

func TestHTTPBaseURL(t *testing.T) {
	cases := map[string]string{
		"127.0.0.1:4000": "http://127.0.0.1:4000",
		":4000":          "http://127.0.0.1:4000",
		"0.0.0.0:4000":   "http://127.0.0.1:4000",
		"localhost:9":    "http://localhost:9",
	}
	for in, want := range cases {
		if got := HTTPBaseURL(in); got != want {
			t.Errorf("HTTPBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolve(t *testing.T) {
	if Resolve("claude") != "claude" {
		t.Error("claude alias")
	}
	if Resolve("my-custom-agent") != "my-custom-agent" {
		t.Error("unknown agent should pass through")
	}
}

func TestEnv(t *testing.T) {
	env := Env("http://127.0.0.1:4000", "tok")
	if !slices.Contains(env, "ANTHROPIC_BASE_URL=http://127.0.0.1:4000") {
		t.Error("missing ANTHROPIC_BASE_URL")
	}
	if !slices.Contains(env, "ANTHROPIC_AUTH_TOKEN=tok") {
		t.Error("missing ANTHROPIC_AUTH_TOKEN")
	}
	// empty token -> "local"
	env2 := Env("http://x", "")
	if !slices.Contains(env2, "ANTHROPIC_AUTH_TOKEN=local") {
		t.Error("empty token should default to local")
	}
}
