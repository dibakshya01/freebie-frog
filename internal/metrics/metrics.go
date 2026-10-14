// Package metrics collects a thread-safe, in-memory, bounded view of recent proxy
// requests for the admin dashboard. It holds no secrets: only model and provider
// names, status codes, token counts, and timings. The collector is optional — a nil
// *Collector is a valid no-op, so the proxy runs identically without it.
package metrics

import (
	"sync"
	"time"
)

// defaultCapacity is the number of recent events retained in the ring buffer.
const defaultCapacity = 200

// Event summarizes one completed request as seen by the dispatcher.
type Event struct {
	Time         time.Time `json:"time"`
	Model        string    `json:"model"`         // model the client requested
	Provider     string    `json:"provider"`      // provider that served (or last tried); "" if unrouted
	TargetModel  string    `json:"target_model"`  // upstream model id
	Stream       bool      `json:"stream"`        // streaming request
	OK           bool      `json:"ok"`            // succeeded
	Status       int       `json:"status"`        // HTTP/Anthropic status code
	InputTokens  int       `json:"input_tokens"`  // 0 when unknown
	OutputTokens int       `json:"output_tokens"` // 0 when unknown (e.g. streaming)
	DurationMs   int64     `json:"duration_ms"`
	Fallbacks    int       `json:"fallbacks"`       // providers tried before this outcome
	Err          string    `json:"error,omitempty"` // scrubbed error summary, if failed
}

// ProviderStat is the running tally for one provider.
type ProviderStat struct {
	Requests  int64     `json:"requests"`
	OK        int64     `json:"ok"`
	Failed    int64     `json:"failed"`
	InTokens  int64     `json:"input_tokens"`
	OutTokens int64     `json:"output_tokens"`
	AvgMs     int64     `json:"avg_ms"`
	LastErr   string    `json:"last_error,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	totalMs   int64     // internal: sum of durations, for AvgMs
}

// Totals are the aggregate counters across all providers.
type Totals struct {
	Requests     int64 `json:"requests"`
	OK           int64 `json:"ok"`
	Failed       int64 `json:"failed"`
	Streamed     int64 `json:"streamed"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// Snapshot is the JSON the dashboard renders.
type Snapshot struct {
	Totals    Totals                   `json:"totals"`
	Providers map[string]*ProviderStat `json:"providers"`
	Recent    []Event                  `json:"recent"` // newest first
	SinceUnix int64                    `json:"since_unix"`
}

// Collector is a bounded ring buffer of recent events plus running aggregates.
// All methods are safe for concurrent use; a nil *Collector is a no-op.
type Collector struct {
	mu         sync.Mutex
	cap        int
	events     []Event
	start      int // index of the oldest retained event
	size       int
	totals     Totals
	byProvider map[string]*ProviderStat
	started    time.Time
}

// New returns a Collector retaining up to capacity recent events (defaultCapacity
// when capacity <= 0).
func New(capacity int) *Collector {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Collector{
		cap:        capacity,
		events:     make([]Event, capacity),
		byProvider: make(map[string]*ProviderStat),
		started:    time.Now(),
	}
}

// Record adds an event and updates the aggregates. Safe on a nil receiver.
func (c *Collector) Record(e Event) {
	if c == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.size < c.cap {
		c.events[(c.start+c.size)%c.cap] = e
		c.size++
	} else {
		c.events[c.start] = e
		c.start = (c.start + 1) % c.cap
	}

	c.totals.Requests++
	if e.OK {
		c.totals.OK++
	} else {
		c.totals.Failed++
	}
	if e.Stream {
		c.totals.Streamed++
	}
	c.totals.InputTokens += int64(e.InputTokens)
	c.totals.OutputTokens += int64(e.OutputTokens)

	ps := c.byProvider[e.Provider]
	if ps == nil {
		ps = &ProviderStat{}
		c.byProvider[e.Provider] = ps
	}
	ps.Requests++
	if e.OK {
		ps.OK++
	} else {
		ps.Failed++
		if e.Err != "" {
			ps.LastErr = e.Err
		}
	}
	ps.InTokens += int64(e.InputTokens)
	ps.OutTokens += int64(e.OutputTokens)
	ps.totalMs += e.DurationMs
	ps.LastSeen = e.Time
}

// Snapshot returns a deep copy of the current state, with Recent newest-first.
// Safe on a nil receiver (returns an empty snapshot).
func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{Providers: map[string]*ProviderStat{}}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	recent := make([]Event, 0, c.size)
	for i := c.size - 1; i >= 0; i-- {
		recent = append(recent, c.events[(c.start+i)%c.cap])
	}
	provs := make(map[string]*ProviderStat, len(c.byProvider))
	for name, v := range c.byProvider {
		cp := *v
		if cp.Requests > 0 {
			cp.AvgMs = cp.totalMs / cp.Requests
		}
		provs[name] = &cp
	}
	return Snapshot{
		Totals:    c.totals,
		Providers: provs,
		Recent:    recent,
		SinceUnix: c.started.Unix(),
	}
}
