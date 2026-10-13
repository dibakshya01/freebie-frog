package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// statusRecorder must expose Flush so SSE events are delivered in real time.
var _ http.Flusher = (*statusRecorder)(nil)

type flushCounter struct {
	http.ResponseWriter
	flushes int
}

func (f *flushCounter) Flush() { f.flushes++ }

func TestStatusRecorderForwardsFlush(t *testing.T) {
	fc := &flushCounter{ResponseWriter: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: fc, status: http.StatusOK}

	rec.Flush()
	rec.Flush()
	if fc.flushes != 2 {
		t.Fatalf("Flush forwarded %d times, want 2 (streaming would be buffered otherwise)", fc.flushes)
	}
	if rec.Unwrap() != fc {
		t.Error("Unwrap should return the underlying ResponseWriter")
	}
	// http.ResponseController must be able to flush through the wrapper.
	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Errorf("ResponseController.Flush through recorder failed: %v", err)
	}
}
