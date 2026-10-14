package admin_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dibakshya01/freebie-frog/internal/admin"
	"github.com/dibakshya01/freebie-frog/internal/config"
	"github.com/dibakshya01/freebie-frog/internal/security"
)

func mgrServer(t *testing.T) (*httptest.Server, *config.Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	mgr := admin.NewManager(&cfg, path)
	ts := httptest.NewServer(security.AdminGuard(admin.Handler(admin.Options{Config: &cfg, Manager: mgr})))
	t.Cleanup(ts.Close)
	return ts, &cfg, path
}

// doReq issues a same-origin (X-Requested-With) request, matching the dashboard.
func doReq(t *testing.T, method, url string, body any) *http.Response {
	t.Helper()
	var req *http.Request
	var err error
	if body != nil {
		b, _ := json.Marshal(body)
		req, err = http.NewRequest(method, url, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Requested-With", "fetch")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestManagePutAndDeleteProvider(t *testing.T) {
	ts, cfg, path := mgrServer(t)

	resp := doReq(t, http.MethodPut, ts.URL+"/admin/api/providers/groq", map[string]any{
		"type": "openai", "base_url": "https://api.groq.com/openai/v1",
		"api_key_ref": "env:GROQ_API_KEY", "models": []string{"m"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put provider status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if _, ok := cfg.Providers["groq"]; !ok {
		t.Fatal("provider not added to in-memory config")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("api.groq.com")) {
		t.Fatalf("config.json not persisted: %s", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("config.json mode = %v, want 0600", fi.Mode().Perm())
	}

	resp = doReq(t, http.MethodDelete, ts.URL+"/admin/api/providers/groq", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete provider status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if _, ok := cfg.Providers["groq"]; ok {
		t.Fatal("provider not deleted from in-memory config")
	}
}

func TestManageRejectsRawKeyAndBadType(t *testing.T) {
	ts, _, _ := mgrServer(t)

	resp := doReq(t, http.MethodPut, ts.URL+"/admin/api/providers/x", map[string]any{
		"type": "openai", "base_url": "https://x/y", "api_key": "sk_raw_secret",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("raw api_key status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()

	resp = doReq(t, http.MethodPut, ts.URL+"/admin/api/providers/x", map[string]any{
		"type": "bogus", "base_url": "https://x/y",
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid type status = %d, want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestManagePutRouting(t *testing.T) {
	ts, cfg, _ := mgrServer(t)
	resp := doReq(t, http.MethodPut, ts.URL+"/admin/api/routing", map[string]any{
		"default": []string{"groq/m"},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put routing status = %d", resp.StatusCode)
	}
	resp.Body.Close()
	if len(cfg.Models.Default) != 1 || cfg.Models.Default[0] != "groq/m" {
		t.Fatalf("routing not applied: %+v", cfg.Models)
	}
}

func TestManageDisabledWithoutManager(t *testing.T) {
	cfg := config.Default()
	ts := httptest.NewServer(security.AdminGuard(admin.Handler(admin.Options{Config: &cfg})))
	defer ts.Close()
	resp := doReq(t, http.MethodPut, ts.URL+"/admin/api/providers/x", map[string]any{
		"type": "openai", "base_url": "https://x/y",
	})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("write with management disabled = %d, want 404", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestManageCSRFBlocksCrossSiteWrite(t *testing.T) {
	ts, _, _ := mgrServer(t)
	b, _ := json.Marshal(map[string]any{"type": "openai", "base_url": "https://x/y"})
	req, _ := http.NewRequest(http.MethodPut, ts.URL+"/admin/api/providers/x", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site") // no X-Requested-With
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site write status = %d, want 403", resp.StatusCode)
	}
}
