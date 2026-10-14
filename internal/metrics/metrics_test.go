package metrics

import (
	"fmt"
	"testing"
)

func TestRecordAndSnapshot(t *testing.T) {
	c := New(10)
	c.Record(Event{Provider: "a", OK: true, InputTokens: 10, OutputTokens: 5, DurationMs: 100})
	c.Record(Event{Provider: "a", OK: false, Status: 500, DurationMs: 300, Err: "boom"})
	c.Record(Event{Provider: "b", OK: true, Stream: true, DurationMs: 50})

	s := c.Snapshot()
	if s.Totals.Requests != 3 || s.Totals.OK != 2 || s.Totals.Failed != 1 || s.Totals.Streamed != 1 {
		t.Fatalf("totals = %+v", s.Totals)
	}
	if s.Totals.InputTokens != 10 || s.Totals.OutputTokens != 5 {
		t.Fatalf("token totals = %+v", s.Totals)
	}
	a := s.Providers["a"]
	if a == nil || a.Requests != 2 || a.OK != 1 || a.Failed != 1 {
		t.Fatalf("provider a = %+v", a)
	}
	if a.AvgMs != 200 { // (100+300)/2
		t.Fatalf("provider a avg = %d, want 200", a.AvgMs)
	}
	if a.LastErr != "boom" {
		t.Fatalf("provider a last error = %q", a.LastErr)
	}
	if len(s.Recent) != 3 || s.Recent[0].Provider != "b" {
		t.Fatalf("recent newest-first broken: %+v", s.Recent)
	}
}

func TestRingBufferWrap(t *testing.T) {
	c := New(3)
	for i := 0; i < 5; i++ {
		c.Record(Event{Provider: "p", TargetModel: fmt.Sprintf("m%d", i)})
	}
	s := c.Snapshot()
	if len(s.Recent) != 3 {
		t.Fatalf("recent len = %d, want 3 (capacity)", len(s.Recent))
	}
	want := []string{"m4", "m3", "m2"} // newest first; m0/m1 evicted
	for i, w := range want {
		if s.Recent[i].TargetModel != w {
			t.Fatalf("recent[%d] = %s, want %s", i, s.Recent[i].TargetModel, w)
		}
	}
	if s.Totals.Requests != 5 {
		t.Fatalf("aggregates should count all events: got %d, want 5", s.Totals.Requests)
	}
}

func TestNilCollectorIsNoOp(t *testing.T) {
	var c *Collector
	c.Record(Event{Provider: "x"}) // must not panic
	s := c.Snapshot()
	if s.Totals.Requests != 0 || s.Providers == nil {
		t.Fatalf("nil snapshot = %+v", s)
	}
}
