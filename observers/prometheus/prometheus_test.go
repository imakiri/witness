package prometheus

import (
	"strconv"
	"testing"

	"github.com/imakiri/witness/core"
)

// rec is a minimal core.Record: the tests need one, and the public toolkit
// lives in a module this one does not depend on.
type rec struct {
	key   string
	value string
}

func (r rec) AppendKey(dst []byte) []byte   { return append(dst, r.key...) }
func (r rec) AppendValue(dst []byte) []byte { return append(dst, r.value...) }
func (r rec) KeyEqual(target string) bool   { return r.key == target }

func value(v float64) rec   { return rec{key: "value", value: strconv.FormatFloat(v, 'g', -1, 64)} }
func label(k, v string) rec { return rec{key: k, value: v} }

func event(t core.EventType, name string, records ...core.Record) core.Event {
	return core.Event{EventType: t, EventMessage: name, Records: records}
}

// gather returns the value of the single sample of metric name, or fails.
func gather(t *testing.T, o *Observer, name string) float64 {
	t.Helper()
	families, err := o.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		if len(f.GetMetric()) != 1 {
			t.Fatalf("%s has %d series, want 1", name, len(f.GetMetric()))
		}
		m := f.GetMetric()[0]
		switch {
		case m.Counter != nil:
			return m.Counter.GetValue()
		case m.Gauge != nil:
			return m.Gauge.GetValue()
		case m.Histogram != nil:
			return m.Histogram.GetSampleSum()
		}
	}
	t.Fatalf("metric %s not exposed", name)
	return 0
}

// TestObserveRoutesByType checks the three metric shapes end to end: a
// counter adds, a gauge takes the last value, a histogram observes.
func TestObserveRoutesByType(t *testing.T) {
	o, err := NewObserver(Config{
		Counters:   []CounterDef{{Name: "jobs_total", LabelKeys: []string{"queue"}}},
		Gauges:     []GaugeDef{{Name: "queue_depth"}},
		Histograms: []HistogramDef{{Name: "job_seconds"}},
	})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}

	o.Observe(event(core.EventTypeMetricCounter(), "jobs_total", value(2), label("queue", "settle")))
	o.Observe(event(core.EventTypeMetricCounter(), "jobs_total", value(3), label("queue", "settle")))
	if got := gather(t, o, "jobs_total"); got != 5 {
		t.Errorf("counter = %v, want 5 — deltas add", got)
	}

	// A gauge is absolute: the second event replaces the first.
	o.Observe(event(core.EventTypeMetricGauge(), "queue_depth", value(7)))
	o.Observe(event(core.EventTypeMetricGauge(), "queue_depth", value(4)))
	if got := gather(t, o, "queue_depth"); got != 4 {
		t.Errorf("gauge = %v, want 4 — the last value wins", got)
	}

	o.Observe(event(core.EventTypeMetricHistogram(), "job_seconds", value(0.25)))
	o.Observe(event(core.EventTypeMetricHistogram(), "job_seconds", value(0.75)))
	if got := gather(t, o, "job_seconds"); got != 1 {
		t.Errorf("histogram sum = %v, want 1", got)
	}
}

// TestObserveDropsUnknown covers the two ways an event is ignored: a metric
// nobody declared, and an event with no value record.
func TestObserveDropsUnknown(t *testing.T) {
	o, err := NewObserver(Config{Counters: []CounterDef{{Name: "jobs_total"}}})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}

	o.Observe(event(core.EventTypeMetricCounter(), "not_declared", value(1)))
	o.Observe(event(core.EventTypeMetricCounter(), "jobs_total", label("queue", "settle")))

	families, err := o.Registry().Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if len(families) != 0 {
		t.Errorf("nothing should have been recorded, got %d families", len(families))
	}
}
