package otlp

import (
	"net/http"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func newTestObserver(t *testing.T) (*Observer, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	o, err := NewObserver(Config{Provider: tp})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}
	return o, rec
}

func TestStartFinishProducesEndedSpan(t *testing.T) {
	o, rec := newTestObserver(t)
	root := uuid.Must(uuid.NewV7())
	span := uuid.Must(uuid.NewV7())

	o.Observe(witness.Event{
		SpanIDs:      []uuid.UUID{root, span},
		EventID:      uuid.Must(uuid.NewV7()),
		EventType:    witness.EventTypeSpanStart(),
		EventMessage: "do_work",
	})
	o.Observe(witness.Event{
		SpanIDs:      []uuid.UUID{root, span},
		EventID:      uuid.Must(uuid.NewV7()),
		EventType:    witness.EventTypeSpanFinish(),
		EventMessage: "do_work",
	})

	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(ended))
	}
	if ended[0].Name() != "do_work" {
		t.Fatalf("expected span name do_work, got %s", ended[0].Name())
	}
}

func TestLogAddEventToOpenSpan(t *testing.T) {
	o, rec := newTestObserver(t)
	root := uuid.Must(uuid.NewV7())
	span := uuid.Must(uuid.NewV7())

	o.Observe(witness.Event{
		SpanIDs:   []uuid.UUID{root, span},
		EventID:   uuid.Must(uuid.NewV7()),
		EventType: witness.EventTypeSpanStart(),
	})
	o.Observe(witness.Event{
		SpanIDs:      []uuid.UUID{root, span},
		EventID:      uuid.Must(uuid.NewV7()),
		EventType:    witness.EventTypeLogInfo(),
		EventMessage: "checkpoint",
	})
	o.Observe(witness.Event{
		SpanIDs:   []uuid.UUID{root, span},
		EventID:   uuid.Must(uuid.NewV7()),
		EventType: witness.EventTypeSpanFinish(),
	})

	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(ended))
	}
	events := ended[0].Events()
	found := false
	for _, e := range events {
		if e.Name == "checkpoint" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected AddEvent(checkpoint), got events: %+v", events)
	}
}

func TestErrorSetsStatus(t *testing.T) {
	o, rec := newTestObserver(t)
	root := uuid.Must(uuid.NewV7())
	span := uuid.Must(uuid.NewV7())

	o.Observe(witness.Event{
		SpanIDs:   []uuid.UUID{root, span},
		EventID:   uuid.Must(uuid.NewV7()),
		EventType: witness.EventTypeSpanStart(),
	})
	o.Observe(witness.Event{
		SpanIDs:      []uuid.UUID{root, span},
		EventID:      uuid.Must(uuid.NewV7()),
		EventType:    witness.EventTypeLogErrorNetwork(),
		EventMessage: "upstream unreachable",
		Records:      []witness.Record{errRecord{msg: "connection refused"}},
	})
	o.Observe(witness.Event{
		SpanIDs:   []uuid.UUID{root, span},
		EventID:   uuid.Must(uuid.NewV7()),
		EventType: witness.EventTypeSpanFinish(),
	})

	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 span, got %d", len(ended))
	}
	if ended[0].Status().Code != codes.Error {
		t.Fatalf("expected Error status, got %v", ended[0].Status().Code)
	}
}

func TestTraceIDStableAcrossSpans(t *testing.T) {
	o, rec := newTestObserver(t)
	root := uuid.Must(uuid.NewV7())
	a := uuid.Must(uuid.NewV7())
	b := uuid.Must(uuid.NewV7())

	for _, s := range []uuid.UUID{a, b} {
		o.Observe(witness.Event{
			SpanIDs:      []uuid.UUID{root, s},
			EventID:      uuid.Must(uuid.NewV7()),
			EventType:    witness.EventTypeSpanStart(),
			EventMessage: "x",
		})
		o.Observe(witness.Event{
			SpanIDs:   []uuid.UUID{root, s},
			EventID:   uuid.Must(uuid.NewV7()),
			EventType: witness.EventTypeSpanFinish(),
		})
	}

	ended := rec.Ended()
	if len(ended) != 2 {
		t.Fatalf("expected 2 ended spans, got %d", len(ended))
	}
	t0 := ended[0].SpanContext().TraceID()
	t1 := ended[1].SpanContext().TraceID()
	if t0 != t1 {
		t.Fatalf("expected same trace_id for siblings under same root, got %s vs %s", t0, t1)
	}
}

func TestPropagationInjectExtractRoundtrip(t *testing.T) {
	root := uuid.Must(uuid.NewV7())
	msg := uuid.Must(uuid.NewV7())

	header := http.Header{}
	Inject(headerCarrier{header}, root, msg)

	tid, sid, ok := Extract(headerCarrier{header})
	if !ok {
		t.Fatalf("Extract failed; header=%q", header.Get(TraceparentHeader))
	}
	// trace_id round-trips fully (16 bytes from root)
	wantTID := traceIDFromUUID(root)
	gotTID := traceIDFromUUID(tid)
	if wantTID != gotTID {
		t.Fatalf("trace_id mismatch: want %x got %x", wantTID, gotTID)
	}
	// span_id round-trips for the 8 byte portion
	wantSID := spanIDFromUUID(msg)
	gotSID := spanIDFromUUID(sid)
	if wantSID != gotSID {
		t.Fatalf("span_id mismatch: want %x got %x", wantSID, gotSID)
	}
}

type errRecord struct {
	msg string
}

func (e errRecord) AppendKey(dst []byte) []byte   { return append(dst, "err"...) }
func (e errRecord) AppendValue(dst []byte) []byte { return append(dst, e.msg...) }
func (e errRecord) KeyEqual(target string) bool   { return target == "err" }

type headerCarrier struct{ h http.Header }

func (c headerCarrier) Get(k string) string   { return c.h.Get(k) }
func (c headerCarrier) Set(k, v string)       { c.h.Set(k, v) }
