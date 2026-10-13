// Package config loads freebie-frog configuration from defaults, an optional
// JSON file, and environment overrides, then validates the result.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// envPrefix namespaces all environment overrides.
const envPrefix = "FREEBIE_"

// Config is the top-level configuration.
type Config struct {
	Server    ServerConfig        `json:"server"`
	Log       LogConfig           `json:"log"`
	Providers map[string]Provider `json:"providers,omitempty"`
	Models    ModelRouting        `json:"models,omitempty"`
}

// ServerConfig controls the HTTP listener.
type ServerConfig struct {
	Addr            string   `json:"addr"`
	ReadTimeout     Duration `json:"read_timeout"`
	WriteTimeout    Duration `json:"write_timeout"`
	ShutdownTimeout Duration `json:"shutdown_timeout"`
}

// LogConfig controls structured logging.
type LogConfig struct {
	Level  string `json:"level"`  // debug | info | warn | error
	Format string `json:"format"` // text | json
}

// Default returns the built-in configuration.
//
// WriteTimeout defaults to 0 (no timeout) on purpose: the proxy's main job is
// long-lived SSE streaming, which a non-zero WriteTimeout would cut off.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Addr:            ":4000",
			ReadTimeout:     Duration(30 * time.Second),
			WriteTimeout:    Duration(0),
			ShutdownTimeout: Duration(15 * time.Second),
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
	}
}

// Load builds configuration from defaults, then the file at path (if non-empty),
// then environment overrides, and validates the result. An empty path skips the
// file step; a non-empty path that cannot be read is an error.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}

	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(cfg); err != nil {
		return fmt.Errorf("parse config file %s: %w", path, err)
	}
	return nil
}

func applyEnv(cfg *Config) error {
	if v, ok := os.LookupEnv(envPrefix + "SERVER_ADDR"); ok {
		cfg.Server.Addr = v
	}
	if v, ok := os.LookupEnv(envPrefix + "LOG_LEVEL"); ok {
		cfg.Log.Level = v
	}
	if v, ok := os.LookupEnv(envPrefix + "LOG_FORMAT"); ok {
		cfg.Log.Format = v
	}
	for _, e := range []struct {
		key string
		dst *Duration
	}{
		{"SERVER_READ_TIMEOUT", &cfg.Server.ReadTimeout},
		{"SERVER_WRITE_TIMEOUT", &cfg.Server.WriteTimeout},
		{"SERVER_SHUTDOWN_TIMEOUT", &cfg.Server.ShutdownTimeout},
	} {
		if v, ok := os.LookupEnv(envPrefix + e.key); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return fmt.Errorf("%s%s: %w", envPrefix, e.key, err)
			}
			*e.dst = Duration(d)
		}
	}
	return nil
}

var (
	validLogLevels  = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validLogFormats = map[string]bool{"text": true, "json": true}
)

// Validate reports every problem it finds, joined into a single error.
func (c Config) Validate() error {
	var errs []error
	if c.Server.Addr == "" {
		errs = append(errs, errors.New("server.addr must not be empty"))
	}
	if !validLogLevels[c.Log.Level] {
		errs = append(errs, fmt.Errorf("log.level %q is invalid (want debug|info|warn|error)", c.Log.Level))
	}
	if !validLogFormats[c.Log.Format] {
		errs = append(errs, fmt.Errorf("log.format %q is invalid (want text|json)", c.Log.Format))
	}
	errs = append(errs, c.validateProviders()...)
	return errors.Join(errs...)
}
