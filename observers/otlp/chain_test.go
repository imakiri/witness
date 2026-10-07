package otlp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// newChainObserver builds the observer the way NewTraceProvider does: with
// IDGenerator, so OTel ids are the witness ones.
func newChainObserver(t *testing.T) (*Observer, *tracetest.SpanRecorder) {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec), sdktrace.WithIDGenerator(IDGenerator()))
	o, err := NewObserver(Config{Provider: tp})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}
	return o, rec
}

func endedByName(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range rec.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("span %q not ended", name)
	return nil
}

// hop carries the current witness span over a wire the way a producer and a
// consumer would, and returns what the receiver feeds into InstanceContinue.
func hop(ctx context.Context) (traceID, parentSpanID uuid.UUID) {
	c := witness.From(ctx)
	h := http.Header{}
	propagation.Inject(h, c.TraceID(), c.CurrentSpanID())
	traceID, parentSpanID, _ = propagation.Extract(h)
	return traceID, parentSpanID
}

func TestInstanceRootTakesWitnessIDs(t *testing.T) {
	o, rec := newChainObserver(t)
	ctx, finish := witness.Instance(context.Background(), o, "svc", "v1")
	c := witness.From(ctx)
	finish()

	s := endedByName(t, rec, "svc")
	if s.SpanContext().TraceID() != traceIDFromUUID(c.TraceID()) {
		t.Fatalf("trace_id %s, want %s", s.SpanContext().TraceID(), traceIDFromUUID(c.TraceID()))
	}
	if s.SpanContext().SpanID() != spanIDFromUUID(c.CurrentSpanID()) {
		t.Fatalf("span_id %s, want %s", s.SpanContext().SpanID(), spanIDFromUUID(c.CurrentSpanID()))
	}
	if s.Parent().IsValid() {
		t.Fatalf("instance root has parent %s, want none", s.Parent().SpanID())
	}
}

func TestSpanNestsUnderInstance(t *testing.T) {
	o, rec := newChainObserver(t)
	ctx, finishInstance := witness.Instance(context.Background(), o, "svc", "v1")
	ctx, finishSpan := witness.Span(ctx, "work")
	c := witness.From(ctx)
	finishSpan()
	finishInstance()

	instance := endedByName(t, rec, "svc")
	work := endedByName(t, rec, "work")
	if work.SpanContext().SpanID() != spanIDFromUUID(c.CurrentSpanID()) {
		t.Fatalf("span_id %s, want %s", work.SpanContext().SpanID(), spanIDFromUUID(c.CurrentSpanID()))
	}
	if work.Parent().SpanID() != instance.SpanContext().SpanID() {
		t.Fatalf("parent %s, want instance %s", work.Parent().SpanID(), instance.SpanContext().SpanID())
	}
}

// The point of the fix: a receiver continued from traceparent nests under the
// sender's span instead of pointing at an id OTel never produced.
func TestHopNestsUnderSender(t *testing.T) {
	o, rec := newChainObserver(t)
	ctx, finishInstance := witness.Instance(context.Background(), o, "http", "v1")
	ctx, finishPublish := witness.Span(ctx, "publish")
	traceID, parentSpanID := hop(ctx)
	finishPublish()
	finishInstance()

	_, finishWorker := witness.InstanceContinue(context.Background(), o, "worker", "v1", traceID, parentSpanID)
	finishWorker()

	publish := endedByName(t, rec, "publish")
	worker := endedByName(t, rec, "worker")
	if worker.SpanContext().TraceID() != publish.SpanContext().TraceID() {
		t.Fatalf("worker trace_id %s, want %s", worker.SpanContext().TraceID(), publish.SpanContext().TraceID())
	}
	if worker.Parent().SpanID() != publish.SpanContext().SpanID() {
		t.Fatalf("worker parent %s, want publish %s", worker.Parent().SpanID(), publish.SpanContext().SpanID())
	}
}

func TestRetryNestsUnderFirstAttempt(t *testing.T) {
	o, rec := newChainObserver(t)
	ctx, finishInstance := witness.Instance(context.Background(), o, "http", "v1")
	traceID, parentSpanID := hop(ctx)
	finishInstance()

	first, finishFirst := witness.InstanceContinue(context.Background(), o, "attempt 1", "v1", traceID, parentSpanID)
	traceID, parentSpanID = hop(first)
	finishFirst()

	_, finishRetry := witness.InstanceContinue(context.Background(), o, "attempt 2", "v1", traceID, parentSpanID)
	finishRetry()

	attempt1 := endedByName(t, rec, "attempt 1")
	attempt2 := endedByName(t, rec, "attempt 2")
	if attempt2.Parent().SpanID() != attempt1.SpanContext().SpanID() {
		t.Fatalf("retry parent %s, want first attempt %s", attempt2.Parent().SpanID(), attempt1.SpanContext().SpanID())
	}
	if attempt2.SpanContext().TraceID() != attempt1.SpanContext().TraceID() {
		t.Fatalf("retry trace_id %s, want %s", attempt2.SpanContext().TraceID(), attempt1.SpanContext().TraceID())
	}
}

// After InstanceContinue SpanIDs()[0] is the local root; Transport must send
// the upstream trace_id, or the second hop lands in a foreign trace.
func TestTransportSecondHopKeepsTraceID(t *testing.T) {
	o, _ := newChainObserver(t)
	upstream, finishUpstream := witness.Instance(context.Background(), o, "upstream", "v1")
	traceID, parentSpanID := hop(upstream)
	finishUpstream()

	ctx, finish := witness.InstanceContinue(context.Background(), o, "middle", "v1", traceID, parentSpanID)
	defer finish()
	ctx, finishCall := witness.Span(ctx, "call")
	defer finishCall()

	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := (&http.Client{Transport: Transport(nil)}).Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	_ = resp.Body.Close()

	gotTrace, gotSpan, ok := propagation.Extract(got)
	if !ok {
		t.Fatalf("no traceparent on the second hop")
	}
	if gotTrace != witness.From(upstream).TraceID() {
		t.Fatalf("second hop trace_id %s, want upstream %s", gotTrace, witness.From(upstream).TraceID())
	}
	if spanIDFromUUID(gotSpan) != spanIDFromUUID(witness.From(ctx).CurrentSpanID()) {
		t.Fatalf("second hop parent %s, want current span %s", spanIDFromUUID(gotSpan), spanIDFromUUID(witness.From(ctx).CurrentSpanID()))
	}
}

func TestTraceSwitchStartsLinkedRoot(t *testing.T) {
	o, rec := newChainObserver(t)
	ctx, finishInstance := witness.Instance(context.Background(), o, "svc", "v1")
	ctx, finishTrace := witness.Trace(ctx, "request")
	c := witness.From(ctx)
	finishTrace()
	finishInstance()

	instance := endedByName(t, rec, "svc")
	request := endedByName(t, rec, "request")
	if request.SpanContext().TraceID() != traceIDFromUUID(c.TraceID()) {
		t.Fatalf("trace_id %s, want switched %s", request.SpanContext().TraceID(), traceIDFromUUID(c.TraceID()))
	}
	if request.Parent().IsValid() {
		t.Fatalf("switched trace has parent %s, want new root", request.Parent().SpanID())
	}
	links := request.Links()
	if len(links) != 1 || links[0].SpanContext.SpanID() != instance.SpanContext().SpanID() {
		t.Fatalf("links %+v, want one to instance %s", links, instance.SpanContext().SpanID())
	}
}

func TestIDGeneratorFallsBackToRandom(t *testing.T) {
	g := IDGenerator()
	t1, s1 := g.NewIDs(context.Background())
	t2, s2 := g.NewIDs(context.Background())
	if !t1.IsValid() || !s1.IsValid() || !t2.IsValid() || !s2.IsValid() {
		t.Fatalf("invalid ids: %s %s %s %s", t1, s1, t2, s2)
	}
	if t1 == t2 || s1 == s2 {
		t.Fatalf("repeated ids: %s %s / %s %s", t1, s1, t2, s2)
	}
	if s := g.NewSpanID(context.Background(), t1); !s.IsValid() {
		t.Fatalf("invalid span id %s", s)
	}
}
