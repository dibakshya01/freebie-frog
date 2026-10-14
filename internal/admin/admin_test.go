package admin_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/admin"
	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/security"
)

func sampleConfig() config.Config {
	return config.Config{
		Server: config.ServerConfig{Addr: "127.0.0.1:4000"},
		Providers: map[string]config.Provider{
			"groq": {Type: "openai", BaseURL: "https://api.groq.com/openai/v1", APIKey: "gsk_SUPERSECRET", Models: []string{"m"}},
		},
		Models: config.ModelRouting{Default: []string{"groq/m"}},
	}
}

func TestBuildStateRedactsKeys(t *testing.T) {
	cfg := sampleConfig()
	s := admin.BuildState(&cfg)
	if len(s.Providers) != 1 || !s.Providers[0].KeyConfigured {
		t.Fatalf("provider state = %+v", s.Providers)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "gsk_SUPERSECRET") {
		t.Fatalf("admin state leaked the API key: %s", b)
	}
}

func TestAdminServesStateAndIndex(t *testing.T) {
	cfg := sampleConfig()
	ts := httptest.NewServer(security.AdminGuard(admin.Handler(&cfg)))
	defer ts.Close()

	// state endpoint (loopback Host from httptest) -> 200, no secret
	resp, err := http.Get(ts.URL + "/admin/api/state")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("state status = %d", resp.StatusCode)
	}
	if strings.Contains(string(body), "SUPERSECRET") {
		t.Fatalf("state leaked secret: %s", body)
	}

	// index page
	resp2, err := http.Get(ts.URL + "/admin/")
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK || !strings.Contains(string(b2), "freebie-frog admin") {
		t.Fatalf("index status=%d body=%.80s", resp2.StatusCode, b2)
	}
}

func TestAdminRejectsNonLoopbackHost(t *testing.T) {
	cfg := sampleConfig()
	ts := httptest.NewServer(security.AdminGuard(admin.Handler(&cfg)))
	defer ts.Close()

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/admin/api/state", nil)
	req.Host = "evil.example.com" // simulate a DNS-rebinding Host header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-loopback Host status = %d, want 403", resp.StatusCode)
	}
}
