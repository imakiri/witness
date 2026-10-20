package dynatrace

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/otlp"
	"github.com/imakiri/witness/record"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

type logRecorder struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (r *logRecorder) Export(_ context.Context, records []sdklog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, rec := range records {
		r.records = append(r.records, rec.Clone())
	}
	return nil
}

func (r *logRecorder) Shutdown(context.Context) error   { return nil }
func (r *logRecorder) ForceFlush(context.Context) error { return nil }

type testEnv struct {
	obs    *Observer
	spans  *tracetest.SpanRecorder
	logs   *logRecorder
	reader *sdkmetric.ManualReader
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	return newSampledTestEnv(t, sdktrace.AlwaysSample())
}

func newSampledTestEnv(t *testing.T, sampler sdktrace.Sampler) testEnv {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spans),
		sdktrace.WithSampler(sampler),
		sdktrace.WithIDGenerator(otlp.IDGenerator()),
	)
	logs := &logRecorder{}
	lp := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(logs)))
	reader := sdkmetric.NewManualReader(sdkmetric.WithTemporalitySelector(deltaTemporality))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	obs, err := newObserver(tp, sampler, lp, mp)
	if err != nil {
		t.Fatalf("newObserver: %v", err)
	}
	return testEnv{obs: obs, spans: spans, logs: logs, reader: reader}
}

// The point of the logs signal: a log line must name the span it was written
// in, by the very ids the trace exporter gave that span.
func TestLogCarriesSpanOfTrace(t *testing.T) {
	env := newTestEnv(t)
	ctx, finish := witness.Instance(context.Background(), env.obs, "svc", "1")
	ctx, finishSpan := witness.Span(ctx, "work")
	witness.Info(ctx, "inside", record.String("k", "v"))
	finishSpan()
	finish()

	var work sdktrace.ReadOnlySpan
	for _, s := range env.spans.Ended() {
		if s.Name() == "work" {
			work = s
		}
	}
	if work == nil {
		t.Fatalf("span work not exported")
	}
	if len(env.logs.records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(env.logs.records))
	}
	rec := env.logs.records[0]
	if rec.TraceID() != work.SpanContext().TraceID() {
		t.Fatalf("log trace_id %s, span trace_id %s", rec.TraceID(), work.SpanContext().TraceID())
	}
	if rec.SpanID() != work.SpanContext().SpanID() {
		t.Fatalf("log span_id %s, span span_id %s", rec.SpanID(), work.SpanContext().SpanID())
	}
	if rec.Body().AsString() != "inside" || rec.Severity() != log.SeverityInfo {
		t.Fatalf("unexpected record: body %q severity %v", rec.Body().AsString(), rec.Severity())
	}
	var k string
	rec.WalkAttributes(func(kv attribute.KeyValue) bool {
		if kv.Key == "k" {
			k = kv.Value.AsString()
		}
		return true
	})
	if k != "v" {
		t.Fatalf("record k not exported as attribute, got %q", k)
	}
	// span events stay too — the trace view shows them
	if len(work.Events()) != 1 {
		t.Fatalf("expected 1 span event, got %d", len(work.Events()))
	}
}

// Logs are exported whatever the sampler says, so the sampled flag on a log
// must be the span's own decision: a log claiming a dropped trace points the
// Dynatrace UI at a trace that never arrives.
func TestLogSampledFlagFollowsSampler(t *testing.T) {
	for _, tc := range []struct {
		name    string
		sampler sdktrace.Sampler
		sampled bool
	}{
		{"always", sdktrace.AlwaysSample(), true},
		{"never", sdktrace.NeverSample(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newSampledTestEnv(t, tc.sampler)
			ctx, finish := witness.Instance(context.Background(), env.obs, "svc", "1")
			witness.Info(ctx, "inside")
			finish()

			if len(env.logs.records) != 1 {
				t.Fatalf("expected 1 log record, got %d", len(env.logs.records))
			}
			if got := env.logs.records[0].TraceFlags().IsSampled(); got != tc.sampled {
				t.Fatalf("log sampled flag %v, want %v", got, tc.sampled)
			}
			if got := len(env.spans.Ended()) > 0; got != tc.sampled {
				t.Fatalf("span exported %v, want %v", got, tc.sampled)
			}
		})
	}
}

func TestErrorLogSeverityAndSpanStatus(t *testing.T) {
	env := newTestEnv(t)
	ctx, finish := witness.Instance(context.Background(), env.obs, "svc", "1")
	witness.Error(ctx, "boom", errors.New("cause"))
	finish()

	if len(env.logs.records) != 1 || env.logs.records[0].Severity() != log.SeverityError {
		t.Fatalf("expected one error log, got %+v", env.logs.records)
	}
	ended := env.spans.Ended()
	if len(ended) != 1 || ended[0].Status().Description != "boom" {
		t.Fatalf("expected span status error boom, got %+v", ended)
	}
}

func TestSpanEventsAreNotLogs(t *testing.T) {
	env := newTestEnv(t)
	_, finish := witness.Instance(context.Background(), env.obs, "svc", "1")
	finish()
	if len(env.logs.records) != 0 {
		t.Fatalf("span events must not become logs, got %d", len(env.logs.records))
	}
}

func TestMetrics(t *testing.T) {
	env := newTestEnv(t)
	emit := func(t witness.EventType, name string, recs ...witness.Record) {
		env.obs.Observe(witness.Event{EventType: t, EventMessage: name, Records: recs})
	}
	emit(witness.EventTypeMetricCounter(), "payments", record.Int("value", 2), record.String("status", "ok"))
	emit(witness.EventTypeMetricCounter(), "payments", record.Int("value", 3), record.String("status", "ok"))
	emit(witness.EventTypeMetricHistogram(), "latency", record.Float("value", 0.5))
	emit(witness.EventTypeMetricGauge(), "queue", record.Int("value", 7))
	emit(witness.EventTypeMetricCounter(), "no_value", record.String("status", "ok"))

	var rm metricdata.ResourceMetrics
	if err := env.reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect: %v", err)
	}
	got := map[string]metricdata.Aggregation{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			got[m.Name] = m.Data
		}
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 metrics, got %v", got)
	}
	sum, ok := got["payments"].(metricdata.Sum[float64])
	if !ok || sum.Temporality != metricdata.DeltaTemporality || sum.DataPoints[0].Value != 5 {
		t.Fatalf("payments: want delta sum 5, got %+v", got["payments"])
	}
	if status, _ := sum.DataPoints[0].Attributes.Value("status"); status.AsString() != "ok" {
		t.Fatalf("payments: dimension status lost")
	}
	if h, ok := got["latency"].(metricdata.Histogram[float64]); !ok || h.Temporality != metricdata.DeltaTemporality || h.DataPoints[0].Sum != 0.5 {
		t.Fatalf("latency: want delta histogram, got %+v", got["latency"])
	}
	if g, ok := got["queue"].(metricdata.Gauge[float64]); !ok || g.DataPoints[0].Value != 7 {
		t.Fatalf("queue: want gauge 7, got %+v", got["queue"])
	}
}

// NewObserver must hit {Endpoint}/v1/{signal} with the Api-Token header.
func TestNewObserverEndpointsAndToken(t *testing.T) {
	var mu sync.Mutex
	hits := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	obs, err := NewObserver(context.Background(), Config{
		Endpoint:    srv.URL + "/e/env/api/v2/otlp/",
		APIToken:    "dt0c01.test",
		ServiceName: "svc",
	})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}
	ctx, finish := witness.Instance(context.Background(), obs, "svc", "1")
	witness.Info(ctx, "hello")
	obs.Observe(witness.Event{EventType: witness.EventTypeMetricCounter(), EventMessage: "c", Records: []witness.Record{record.Int("value", 1)}})
	finish()
	if err := obs.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	for _, p := range []string{"/e/env/api/v2/otlp/v1/traces", "/e/env/api/v2/otlp/v1/logs", "/e/env/api/v2/otlp/v1/metrics"} {
		auth, ok := hits[p]
		if !ok {
			t.Errorf("%s not called; hits: %v", p, hits)
			continue
		}
		if auth != "Api-Token dt0c01.test" {
			t.Errorf("%s: Authorization %q", p, auth)
		}
	}
}
