package router

import (
	"context"
	"log/slog"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/provider"
)

// Dispatcher routes a request across its ordered targets, falling back on retryable
// errors. For streaming, fallback is only possible before the first event is emitted.
type Dispatcher struct {
	reg *Registry
	log *slog.Logger
}

// NewDispatcher builds a Dispatcher.
func NewDispatcher(reg *Registry, log *slog.Logger) *Dispatcher {
	return &Dispatcher{reg: reg, log: log}
}

func noRoute(model string) *provider.Error {
	return &provider.Error{StatusCode: 404, Type: "not_found_error", Message: "no route configured for model " + model}
}

// Complete tries each target in order, falling back on retryable errors.
func (d *Dispatcher) Complete(ctx context.Context, req *anthropic.Request) (*anthropic.Response, error) {
	targets := d.reg.Route(req.Model)
	if len(targets) == 0 {
		return nil, noRoute(req.Model)
	}
	var lastErr error
	for _, t := range targets {
		ad, ok := d.reg.Adapter(t.Provider)
		if !ok {
			lastErr = &provider.Error{StatusCode: 500, Type: "api_error", Message: "unknown provider " + t.Provider}
			continue
		}
		resp, err := ad.Complete(ctx, req, t)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !provider.IsRetryable(err) {
			return nil, err
		}
		d.log.Warn("provider failed; falling back", "provider", t.Provider, "model", t.Model, "error", err.Error())
	}
	return nil, lastErr
}

// Stream tries each target in order. It reports whether any event was emitted; once
// an event is emitted, no further fallback is attempted (the client already holds a
// partial Anthropic message). Returns (emitted, error).
func (d *Dispatcher) Stream(ctx context.Context, req *anthropic.Request, sink func(anthropic.StreamEvent) error) (bool, error) {
	targets := d.reg.Route(req.Model)
	if len(targets) == 0 {
		return false, noRoute(req.Model)
	}
	emitted := false
	wrapped := func(ev anthropic.StreamEvent) error {
		emitted = true
		return sink(ev)
	}
	var lastErr error
	for _, t := range targets {
		ad, ok := d.reg.Adapter(t.Provider)
		if !ok {
			lastErr = &provider.Error{StatusCode: 500, Type: "api_error", Message: "unknown provider " + t.Provider}
			continue
		}
		err := ad.Stream(ctx, req, t, wrapped)
		if err == nil {
			return emitted, nil
		}
		lastErr = err
		if emitted {
			return true, err // already streaming — cannot transparently retry
		}
		if !provider.IsRetryable(err) {
			return false, err
		}
		d.log.Warn("provider stream failed before first event; falling back", "provider", t.Provider, "model", t.Model, "error", err.Error())
	}
	return emitted, lastErr
}
