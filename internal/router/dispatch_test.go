package router_test

import (
	"context"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/mockprovider"
	"github.com/dibakshya01/freebie-frog/internal/obs"
	"github.com/dibakshya01/freebie-frog/internal/router"
	"github.com/dibakshya01/freebie-frog/internal/secret"
)

func dispatcher(t *testing.T, cfg config.Config) *router.Dispatcher {
	t.Helper()
	reg, err := router.Build(&cfg, secret.New())
	if err != nil {
		t.Fatalf("router.Build: %v", err)
	}
	return router.NewDispatcher(reg, obs.NewLogger("error", "text"))
}

func req() *anthropic.Request {
	return &anthropic.Request{
		Model:     "claude-test",
		MaxTokens: 10,
		Messages:  []anthropic.Message{{Role: "user", Content: []anthropic.ContentBlock{{Type: "text", Text: "hi"}}}},
	}
}

func openaiProvider(url string) config.Provider {
	return config.Provider{Type: "openai", BaseURL: url, APIKey: "k", AllowLocal: true}
}

func TestComplete_FallbackOnRetryable(t *testing.T) {
	bad := mockprovider.New()
	bad.StatusCode = 503 // retryable
	defer bad.Close()
	good := mockprovider.New()
	defer good.Close()

	cfg := config.Default()
	cfg.Providers = map[string]config.Provider{"bad": openaiProvider(bad.URL()), "good": openaiProvider(good.URL())}
	cfg.Models = config.ModelRouting{Default: []string{"bad/m", "good/m"}}

	resp, err := dispatcher(t, cfg).Complete(context.Background(), req())
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if len(resp.Content) == 0 || resp.Content[0].Text != "Hello from the mock." {
		t.Errorf("unexpected response: %+v", resp.Content)
	}
	if bad.Requests() != 1 || good.Requests() != 1 {
		t.Errorf("request counts: bad=%d good=%d (want 1,1)", bad.Requests(), good.Requests())
	}
}

func TestComplete_NoFallbackOnNonRetryable(t *testing.T) {
	bad := mockprovider.New()
	bad.StatusCode = 400 // not retryable
	defer bad.Close()
	good := mockprovider.New()
	defer good.Close()

	cfg := config.Default()
	cfg.Providers = map[string]config.Provider{"bad": openaiProvider(bad.URL()), "good": openaiProvider(good.URL())}
	cfg.Models = config.ModelRouting{Default: []string{"bad/m", "good/m"}}

	_, err := dispatcher(t, cfg).Complete(context.Background(), req())
	if err == nil {
		t.Fatal("expected error (400 is not retryable)")
	}
	if good.Requests() != 0 {
		t.Errorf("good should not have been tried, got %d requests", good.Requests())
	}
}

func TestComplete_NoRoute(t *testing.T) {
	good := mockprovider.New()
	defer good.Close()
	cfg := config.Default()
	cfg.Providers = map[string]config.Provider{"good": openaiProvider(good.URL())}
	// no Models routing configured
	_, err := dispatcher(t, cfg).Complete(context.Background(), req())
	if err == nil {
		t.Fatal("expected no-route error")
	}
}

func TestStream_FallbackBeforeFirstEvent(t *testing.T) {
	bad := mockprovider.New()
	bad.StatusCode = 503
	defer bad.Close()
	good := mockprovider.New()
	good.Chunks = []string{
		`{"choices":[{"index":0,"delta":{"content":"ok"}}]}`,
		`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
	}
	defer good.Close()

	cfg := config.Default()
	cfg.Providers = map[string]config.Provider{"bad": openaiProvider(bad.URL()), "good": openaiProvider(good.URL())}
	cfg.Models = config.ModelRouting{Default: []string{"bad/m", "good/m"}}

	r := req()
	r.Stream = true
	var events []anthropic.StreamEvent
	emitted, err := dispatcher(t, cfg).Stream(context.Background(), r, func(ev anthropic.StreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if !emitted || len(events) == 0 || events[0].Event != "message_start" {
		t.Fatalf("expected a streamed message starting with message_start, got %v", events)
	}
}
