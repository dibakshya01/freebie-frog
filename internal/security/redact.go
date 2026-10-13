package security

import (
	"regexp"
	"strings"
)

// sensitiveHeaders are never logged by value.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"x-api-key":           true,
	"api-key":             true,
	"x-goog-api-key":      true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
}

// IsSensitiveHeader reports whether a header name must be redacted in logs.
func IsSensitiveHeader(name string) bool {
	return sensitiveHeaders[strings.ToLower(name)]
}

// keyPatterns matches common provider key / bearer shapes so they can be scrubbed
// from bodies even when their exact value isn't known to the redactor.
var keyPatterns = regexp.MustCompile(
	`(?i)(sk-[a-z0-9_-]{6,}|gsk_[a-z0-9]{6,}|nvapi-[a-z0-9_-]{6,}|csk-[a-z0-9]{6,}|sk-or-[a-z0-9_-]{6,}|AIza[0-9A-Za-z_-]{10,}|Bearer\s+[a-z0-9._-]{6,})`,
)

// Redactor scrubs known secret values and key-shaped patterns from strings.
type Redactor struct {
	secrets []string
}

// NewRedactor builds a redactor that also scrubs the given known secret values
// (short/empty values are ignored to avoid over-redacting).
func NewRedactor(secrets ...string) *Redactor {
	kept := make([]string, 0, len(secrets))
	for _, s := range secrets {
		if len(s) >= 6 {
			kept = append(kept, s)
		}
	}
	return &Redactor{secrets: kept}
}

// Redact replaces known secret values and key-shaped substrings with "***".
func (r *Redactor) Redact(s string) string {
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, "***")
	}
	return keyPatterns.ReplaceAllString(s, "***")
}

// NeutralizeNewlines strips CR/LF so attacker-controlled text can't forge log lines.
func NeutralizeNewlines(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}
