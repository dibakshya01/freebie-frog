package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("default config is invalid: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	cfg := Default()
	cfg.Server.Addr = ""
	cfg.Log.Level = "loud"
	cfg.Log.Format = "xml"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"server":{"addr":":9999","read_timeout":"5s"},"log":{"level":"debug","format":"json"}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":9999" {
		t.Errorf("addr = %q, want :9999", cfg.Server.Addr)
	}
	if cfg.Server.ReadTimeout.Std() != 5*time.Second {
		t.Errorf("read_timeout = %v, want 5s", cfg.Server.ReadTimeout.Std())
	}
	if cfg.Log.Level != "debug" || cfg.Log.Format != "json" {
		t.Errorf("log = %+v, want {debug json}", cfg.Log)
	}
}

func TestLoadFileUnknownFieldFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"server":{"nope":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected error for unknown field, got nil")
	}
}

func TestLoadMissingExplicitFileFails(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv("FREEBIE_SERVER_ADDR", ":7777")
	t.Setenv("FREEBIE_LOG_LEVEL", "warn")
	t.Setenv("FREEBIE_SERVER_SHUTDOWN_TIMEOUT", "3s")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Addr != ":7777" {
		t.Errorf("addr = %q, want :7777", cfg.Server.Addr)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("level = %q, want warn", cfg.Log.Level)
	}
	if cfg.Server.ShutdownTimeout.Std() != 3*time.Second {
		t.Errorf("shutdown_timeout = %v, want 3s", cfg.Server.ShutdownTimeout.Std())
	}
}

func TestEnvInvalidDurationFails(t *testing.T) {
	t.Setenv("FREEBIE_SERVER_READ_TIMEOUT", "not-a-duration")
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for invalid duration env, got nil")
	}
}

func TestDurationUnmarshalNumberIsSeconds(t *testing.T) {
	var d Duration
	if err := d.UnmarshalJSON([]byte("12")); err != nil {
		t.Fatal(err)
	}
	if d.Std() != 12*time.Second {
		t.Errorf("got %v, want 12s", d.Std())
	}
}
