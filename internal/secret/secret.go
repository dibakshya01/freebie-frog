// Package secret resolves provider API keys from references, keeping the key value
// out of config files. M1 supports env/literal; keychain and encrypted-file backends
// are added in the security milestone. Resolved values are never logged.
package secret

import (
	"fmt"
	"os"
	"strings"
)

// Store resolves secret references of the form "scheme:rest":
//   - env:NAME       -> value of environment variable NAME
//   - literal:VALUE  -> VALUE verbatim (discouraged)
//   - keychain:...   -> OS keychain (added in the security milestone)
//   - enc:...        -> AES-256-GCM encrypted file (added in the security milestone)
//
// A bare reference (no scheme) is treated as an environment variable name.
type Store struct{}

// New returns a Store.
func New() *Store { return &Store{} }

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
	case "keychain", "enc", "file":
		return "", fmt.Errorf("secret backend %q is not available in this build", scheme)
	default:
		// Unknown scheme: treat the whole reference as an env var name.
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
