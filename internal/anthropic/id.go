package anthropic

import (
	"crypto/rand"
	"encoding/hex"
)

// NewMessageID returns a fresh Anthropic-style message id ("msg_" + random hex).
func NewMessageID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// rand.Read never fails on supported platforms; fall back to a fixed suffix.
		return "msg_000000000000000000000000"
	}
	return "msg_" + hex.EncodeToString(b)
}
