package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/mockprovider"
	"github.com/dibakshya01/freebie-frog/internal/obs"
	"github.com/dibakshya01/freebie-frog/internal/router"
	"github.com/dibakshya01/freebie-frog/internal/secret"
	"github.com/dibakshya01/freebie-frog/internal/server"
)

func buildServerWithToken(t *testing.T, mock *mockprovider.Mock, token string) http.Handler {
	t.Helper()
	cfg := config.Default()
	cfg.Server.AuthToken = token
	cfg.Providers = map[string]config.Provider{
		"mock": {Type: "openai", BaseURL: mock.URL(), APIKey: "k", AllowLocal: true},
	}
	cfg.Models = config.ModelRouting{Default: []string{"mock/m"}}
	reg, err := router.Build(&cfg, secret.New())
	if err != nil {
		t.Fatal(err)
	}
	log := obs.NewLogger("error", "text")
	return server.New(cfg.Server, log, server.Deps{Registry: reg, Dispatcher: router.NewDispatcher(reg, log)}).Handler()
}

func TestIngressAuth(t *testing.T) {
	mock := mockprovider.New()
	defer mock.Close()
	ts := httptest.NewServer(buildServerWithToken(t, mock, "secret-token"))
	defer ts.Close()

	body := `{"model":"claude-test","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`

	// No token -> 401
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: status = %d, want 401", resp.StatusCode)
	}

	// Correct token via x-api-key -> 200
	req1, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages", strings.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("x-api-key", "secret-token")
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp1.Body)
	resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("x-api-key token: status = %d, want 200", resp1.StatusCode)
	}

	// Correct token via Authorization: Bearer -> 200
	req2, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages", strings.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer secret-token")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("bearer token: status = %d, want 200", resp2.StatusCode)
	}

	// Wrong token -> 401
	req3, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages", strings.NewReader(body))
	req3.Header.Set("x-api-key", "nope")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: status = %d, want 401", resp3.StatusCode)
	}

	// /healthz is always open
	resph, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resph.Body.Close()
	if resph.StatusCode != http.StatusOK {
		t.Errorf("healthz: status = %d, want 200", resph.StatusCode)
	}
}
