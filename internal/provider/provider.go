// Package provider defines the adapter interface and provider implementations that
// freebie-frog routes requests to.
package provider

import (
	"context"
	"fmt"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
)

// Target names a provider and the model id to use on it.
type Target struct {
	Provider string
	Model    string
}

// Capabilities describes what an adapter supports.
type Capabilities struct {
	Streaming bool
	Tools     bool
	Images    bool
}

// Adapter translates an Anthropic request to a provider and back.
type Adapter interface {
	Name() string
	Capabilities() Capabilities
	// Complete performs a non-streaming request.
	Complete(ctx context.Context, req *anthropic.Request, t Target) (*anthropic.Response, error)
	// Stream performs a streaming request, emitting translated Anthropic SSE events
	// via sink. An adapter must not emit its first event until it is sure the request
	// has started successfully, so the router can still fall back before event one.
	Stream(ctx context.Context, req *anthropic.Request, t Target, sink func(anthropic.StreamEvent) error) error
}

// Error is an adapter error carrying an upstream status and whether a retry on
// another provider is appropriate (used by the router's fallback engine).
type Error struct {
	StatusCode int
	Type       string // Anthropic error type (rate_limit_error, api_error, ...)
	Message    string
	Retryable  bool
}

func (e *Error) Error() string {
	return fmt.Sprintf("provider error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}

// IsRetryable reports whether err is a retryable provider error.
func IsRetryable(err error) bool {
	pe, ok := err.(*Error)
	return ok && pe.Retryable
}
