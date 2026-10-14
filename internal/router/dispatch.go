package router

import (
	"context"
	"log/slog"
	"time"

	"github.com/dibakshya01/freebie-frog/internal/anthropic"
	"github.com/dibakshya01/freebie-frog/internal/metrics"
	"github.com/dibakshya01/freebie-frog/internal/provider"
	"github.com/dibakshya01/freebie-frog/internal/security"
	"github.com/dibakshya01/freebie-frog/internal/translate"
)

// completeTimeout bounds each non-streaming upstream attempt so a hung provider
// can't tie up the request indefinitely. Streaming calls are long-lived and instead
// rely on client-disconnect cancellation via the request context.
const completeTimeout = 120 * time.Second

// Dispatcher routes a request across its ordered targets, falling back on retryable
// errors. For streaming, fallback is only possible before the first event is emitted.
type Dispatcher struct {
	reg *Registry
	log *slog.Logger
	mx  *metrics.Collector // optional; nil disables metrics (no-op)
}

// NewDispatcher builds a Dispatcher.
func NewDispatcher(reg *Registry, log *slog.Logger) *Dispatcher {
	return &Dispatcher{reg: reg, log: log}
}

// SetMetrics attaches a metrics collector. A nil collector (or never calling this)
// leaves metrics disabled.
func (d *Dispatcher) SetMetrics(c *metrics.Collector) { d.mx = c }

func noRoute(model string) *provider.Error {
	return &provider.Error{StatusCode: 404, Type: "not_found_error", Message: "no route configured for model " + model}
}

// Complete tries each target in order, falling back on retryable errors.
func (d *Dispatcher) Complete(ctx context.Context, req *anthropic.Request) (*anthropic.Response, error) {
	start := time.Now()
	targets := d.reg.Route(req.Model)
	if len(targets) == 0 {
		d.record(req, provider.Target{}, false, 404, 0, 0, 0, start, "no route configured")
		return nil, noRoute(req.Model)
	}
	var lastErr error
	for i, t := range targets {
		ad, ok := d.reg.Adapter(t.Provider)
		if !ok {
			lastErr = &provider.Error{StatusCode: 500, Type: "api_error", Message: "unknown provider " + t.Provider}
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, completeTimeout)
		resp, err := ad.Complete(cctx, req, t)
		cancel()
		if err == nil {
			d.record(req, t, true, 200, resp.Usage.InputTokens, resp.Usage.OutputTokens, i, start, "")
			return resp, nil
		}
		lastErr = err
		if !provider.IsRetryable(err) {
			d.record(req, t, false, statusOf(err), 0, 0, i, start, security.Scrub(err.Error()))
			return nil, err
		}
		d.log.Warn("provider failed; falling back", "provider", t.Provider, "model", t.Model, "error", security.Scrub(err.Error()))
	}
	last := targets[len(targets)-1]
	d.record(req, last, false, statusOf(lastErr), 0, 0, len(targets)-1, start, scrubErr(lastErr))
	return nil, lastErr
}

// Stream tries each target in order. It reports whether any event was emitted; once
// an event is emitted, no further fallback is attempted (the client already holds a
// partial Anthropic message). Returns (emitted, error).
func (d *Dispatcher) Stream(ctx context.Context, req *anthropic.Request, sink func(anthropic.StreamEvent) error) (bool, error) {
	start := time.Now()
	targets := d.reg.Route(req.Model)
	if len(targets) == 0 {
		d.record(req, provider.Target{}, false, 404, 0, 0, 0, start, "no route configured")
		return false, noRoute(req.Model)
	}
	// Output tokens aren't reliably available from a stream; input tokens are
	// estimated (see docs/CAPABILITIES.md) so the dashboard still shows a signal.
	estIn := translate.EstimateRequestTokens(req)
	emitted := false
	wrapped := func(ev anthropic.StreamEvent) error {
		emitted = true
		return sink(ev)
	}
	var lastErr error
	for i, t := range targets {
		ad, ok := d.reg.Adapter(t.Provider)
		if !ok {
			lastErr = &provider.Error{StatusCode: 500, Type: "api_error", Message: "unknown provider " + t.Provider}
			continue
		}
		err := ad.Stream(ctx, req, t, wrapped)
		if err == nil {
			d.record(req, t, true, 200, estIn, 0, i, start, "")
			return emitted, nil
		}
		lastErr = err
		if emitted {
			d.record(req, t, false, statusOf(err), estIn, 0, i, start, "stream error after first event")
			return true, err // already streaming — cannot transparently retry
		}
		if !provider.IsRetryable(err) {
			d.record(req, t, false, statusOf(err), 0, 0, i, start, security.Scrub(err.Error()))
			return false, err
		}
		d.log.Warn("provider stream failed before first event; falling back", "provider", t.Provider, "model", t.Model, "error", security.Scrub(err.Error()))
	}
	last := targets[len(targets)-1]
	d.record(req, last, false, statusOf(lastErr), estIn, 0, len(targets)-1, start, scrubErr(lastErr))
	return emitted, lastErr
}

// record emits a metrics event for one completed request. No-op when metrics are off.
func (d *Dispatcher) record(req *anthropic.Request, t provider.Target, ok bool, status, in, out, fallbacks int, start time.Time, errMsg string) {
	if d.mx == nil {
		return
	}
	d.mx.Record(metrics.Event{
		Model:        req.Model,
		Provider:     t.Provider,
		TargetModel:  t.Model,
		Stream:       req.Stream,
		OK:           ok,
		Status:       status,
		InputTokens:  in,
		OutputTokens: out,
		DurationMs:   time.Since(start).Milliseconds(),
		Fallbacks:    fallbacks,
		Err:          errMsg,
	})
}

func statusOf(err error) int {
	if pe, ok := err.(*provider.Error); ok {
		return pe.StatusCode
	}
	return 500
}

func scrubErr(err error) string {
	if err == nil {
		return ""
	}
	return security.Scrub(err.Error())
}
