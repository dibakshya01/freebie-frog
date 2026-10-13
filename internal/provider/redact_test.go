package provider

import (
	"strings"
	"testing"
)

// An upstream that echoes a key in its error body must not surface it verbatim.
func TestProviderMessageScrubsKeys(t *testing.T) {
	body := []byte(`{"error":{"message":"invalid key sk-abcdef1234567890 supplied"}}`)
	msg := providerMessage(body, 400)
	if strings.Contains(msg, "sk-abcdef1234567890") {
		t.Fatalf("provider error message leaked a key: %s", msg)
	}
	if !strings.Contains(msg, "***") {
		t.Errorf("expected redaction marker, got %q", msg)
	}
}

func TestGeminiMessageScrubsKeys(t *testing.T) {
	body := []byte(`{"error":{"message":"bad AIzaSyA1234567890abcdef key"}}`)
	msg := geminiMessage(body, 401)
	if strings.Contains(msg, "AIzaSyA1234567890abcdef") {
		t.Fatalf("gemini error message leaked a key: %s", msg)
	}
}
