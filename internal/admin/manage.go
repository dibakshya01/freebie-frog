package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dibakshya01/freebie-frog/internal/config"
)

// Manager guards in-memory edits to the configuration and persists them to the
// config file. It never accepts or stores secret values: providers reference keys
// by api_key_ref only. Edits take effect on the next restart of `freebie serve`.
type Manager struct {
	mu   sync.RWMutex
	cfg  *config.Config
	path string
}

// NewManager returns a Manager that persists to path (defaulting to ./config.json).
func NewManager(cfg *config.Config, path string) *Manager {
	if strings.TrimSpace(path) == "" {
		path = "config.json"
	}
	return &Manager{cfg: cfg, path: path}
}

// State returns the redacted snapshot under a read lock.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s := BuildState(m.cfg)
	s.WriteEnabled = true
	return s
}

// apiError carries an HTTP status with a client-safe message.
type apiError struct {
	code int
	msg  string
}

func (e *apiError) Error() string    { return e.msg }
func badRequest(m string) *apiError  { return &apiError{http.StatusBadRequest, m} }
func notFoundErr(m string) *apiError { return &apiError{http.StatusNotFound, m} }
func serverError(m string) *apiError { return &apiError{http.StatusInternalServerError, m} }

// --- mutations ---

func (m *Manager) putProvider(name string, p config.Provider) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return badRequest("provider name is required")
	}
	if p.APIKey != "" {
		return badRequest("do not send a raw api_key; use api_key_ref (env:NAME, keychain:NAME, or enc:NAME) and store the secret out of band")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	trial := m.clone()
	trial.Providers[name] = p
	if err := trial.Validate(); err != nil {
		return badRequest(err.Error())
	}
	if m.cfg.Providers == nil {
		m.cfg.Providers = map[string]config.Provider{}
	}
	m.cfg.Providers[name] = p
	return m.persist()
}

func (m *Manager) deleteProvider(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.cfg.Providers[name]; !ok {
		return notFoundErr("provider " + name + " not found")
	}
	delete(m.cfg.Providers, name)
	return m.persist()
}

func (m *Manager) putRouting(r config.ModelRouting) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	trial := m.clone()
	trial.Models = r
	if err := trial.Validate(); err != nil {
		return badRequest(err.Error())
	}
	m.cfg.Models = r
	return m.persist()
}

// clone returns a copy of the config with the Providers map duplicated, so a
// validation trial never mutates the live config.
func (m *Manager) clone() config.Config {
	c := *m.cfg
	c.Providers = make(map[string]config.Provider, len(m.cfg.Providers))
	for k, v := range m.cfg.Providers {
		c.Providers[k] = v
	}
	return c
}

// persist atomically writes the config to disk, mode 0600.
func (m *Manager) persist() error {
	data, err := json.MarshalIndent(m.cfg, "", "  ")
	if err != nil {
		return serverError("marshal config: " + err.Error())
	}
	data = append(data, '\n')
	dir := filepath.Dir(m.path)
	tmp, err := os.CreateTemp(dir, ".freebie-config-*.tmp")
	if err != nil {
		return serverError("write config: " + err.Error())
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return serverError("chmod config: " + err.Error())
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return serverError("write config: " + err.Error())
	}
	if err := tmp.Close(); err != nil {
		return serverError("write config: " + err.Error())
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return serverError("replace config: " + err.Error())
	}
	return nil
}

// --- HTTP handlers (registered only when management is enabled) ---

func (m *Manager) handlePutProvider(w http.ResponseWriter, r *http.Request) {
	var p config.Provider
	if !decodeJSON(w, r, &p) {
		return
	}
	if err := m.putProvider(r.PathValue("name"), p); err != nil {
		writeAPIError(w, err)
		return
	}
	m.writeOK(w)
}

func (m *Manager) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	if err := m.deleteProvider(r.PathValue("name")); err != nil {
		writeAPIError(w, err)
		return
	}
	m.writeOK(w)
}

func (m *Manager) handlePutRouting(w http.ResponseWriter, r *http.Request) {
	var rt config.ModelRouting
	if !decodeJSON(w, r, &rt) {
		return
	}
	if err := m.putRouting(rt); err != nil {
		writeAPIError(w, err)
		return
	}
	m.writeOK(w)
}

func (m *Manager) writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"restart_required": true,
		"state":            m.State(),
	})
}

const maxConfigBody = 1 << 20 // 1 MiB — ample for a provider/routing fragment

// decodeJSON decodes the request body into dst, writing an error response and
// returning false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxConfigBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeAPIError(w, badRequest("invalid JSON: "+err.Error()))
		return false
	}
	return true
}

func writeAPIError(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	var ae *apiError
	if errors.As(err, &ae) {
		code = ae.code
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
