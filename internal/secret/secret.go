// Package secret resolves provider API keys from references, keeping key values out
// of config files and out of logs. Supported reference schemes:
//
//	env:NAME            environment variable NAME
//	literal:VALUE       VALUE verbatim (discouraged)
//	file:/path          trimmed contents of a file
//	keychain:svc/acct   OS keychain entry (service/account; "freebie-frog" default service)
//	enc:NAME            NAME from an AES-256-GCM encrypted secrets file
//	                    (path/passphrase from FREEBIE_SECRETS_FILE / FREEBIE_SECRETS_PASSPHRASE)
//
// A bare reference (no scheme) is treated as an environment variable name.
// Resolved values are never logged.
package secret

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// Store resolves secret references.
type Store struct {
	encPath       string
	encPassphrase string

	mu        sync.Mutex
	encCache  map[string]string
	encLoaded bool
}

// New returns a Store, reading the encrypted-file location/passphrase from the
// environment (used only by "enc:" references).
func New() *Store {
	return &Store{
		encPath:       os.Getenv("FREEBIE_SECRETS_FILE"),
		encPassphrase: os.Getenv("FREEBIE_SECRETS_PASSPHRASE"),
	}
}

// Resolve returns the secret value for ref, or an error.
func (s *Store) Resolve(ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	scheme, rest, found := strings.Cut(ref, ":")
	if !found {
		return envValue(ref)
	}
	switch scheme {
	case "env":
		return envValue(rest)
	case "literal":
		return rest, nil
	case "file":
		return fileValue(rest)
	case "keychain":
		return keychainValue(rest)
	case "enc":
		return s.encValue(rest)
	default:
		return envValue(ref)
	}
}

func envValue(name string) (string, error) {
	v, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set", name)
	}
	return v, nil
}

func fileValue(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read secret file: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

func keychainValue(rest string) (string, error) {
	service, account := "freebie-frog", rest
	if svc, acct, ok := strings.Cut(rest, "/"); ok {
		service, account = svc, acct
	}
	v, err := keyring.Get(service, account)
	if err != nil {
		return "", fmt.Errorf("keychain %q: %w", rest, err)
	}
	return v, nil
}

func (s *Store) encValue(name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.encLoaded {
		if s.encPath == "" {
			return "", fmt.Errorf("enc: FREEBIE_SECRETS_FILE is not set")
		}
		if s.encPassphrase == "" {
			return "", fmt.Errorf("enc: FREEBIE_SECRETS_PASSPHRASE is not set")
		}
		m, err := OpenEncryptedFile(s.encPath, s.encPassphrase)
		if err != nil {
			return "", err
		}
		s.encCache = m
		s.encLoaded = true
	}
	v, ok := s.encCache[name]
	if !ok {
		return "", fmt.Errorf("enc: secret %q not found in %s", name, s.encPath)
	}
	return v, nil
}
