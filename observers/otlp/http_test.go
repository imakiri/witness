package otlp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Sender opens a span, fires an HTTP request through the transport, and the
// downstream handler should see the same trace_id and a parent_span_id
// matching the sender's current span. Both spans must end up under one trace.
func TestCrossServiceTraceContinuity(t *testing.T) {
	senderRec := tracetest.NewSpanRecorder()
	receiverRec := tracetest.NewSpanRecorder()

	senderObs, _ := NewObserver(Config{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(senderRec))})
	receiverObs, _ := NewObserver(Config{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(receiverRec))})

	srv := httptest.NewServer(Middleware(receiverObs, "service_b", "v1")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, finish := witness.Span(r.Context(), "handle_request")
			defer finish()
			witness.Info(ctx, "handled")
			w.WriteHeader(http.StatusOK)
		}),
	))
	defer srv.Close()

	ctx, finishA := witness.Instance(context.Background(), senderObs, "service_a", "v1")
	defer finishA()
	ctx, finishCall := witness.Span(ctx, "call_b")

	client := &http.Client{Transport: Transport(nil)}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	resp.Body.Close()
	finishCall()

	// Force-flush spans by ending instance scopes.
	finishA()

	sender := senderRec.Ended()
	receiver := receiverRec.Ended()
	if len(sender) == 0 || len(receiver) == 0 {
		t.Fatalf("expected spans on both sides, got sender=%d receiver=%d", len(sender), len(receiver))
	}

	senderTrace := sender[0].SpanContext().TraceID()
	for _, s := range sender {
		if s.SpanContext().TraceID() != senderTrace {
			t.Fatalf("sender spans have divergent trace_ids")
		}
	}
	for _, s := range receiver {
		if s.SpanContext().TraceID() != senderTrace {
			t.Fatalf("receiver trace_id %s != sender trace_id %s", s.SpanContext().TraceID(), senderTrace)
		}
	}

	// The receiver's instance:online span must point to a parent that lives
	// in the sender's trace (the call_b span_id, last 8 bytes).
	var instanceOnline *sdktrace.ReadOnlySpan
	for i := range receiver {
		if receiver[i].Name() == "service_b" {
			instanceOnline = &receiver[i]
			break
		}
	}
	if instanceOnline == nil {
		t.Fatalf("receiver instance:online span not found")
	}
	if !(*instanceOnline).Parent().IsValid() {
		t.Fatalf("receiver instance:online has no parent — cross-service link missing")
	}
}

// InstanceContinue with uuid.Nil parent must degrade to plain Instance.
func TestInstanceContinueFallsBackOnNil(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	obs, _ := NewObserver(Config{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))})
	_, finish := witness.InstanceContinue(context.Background(), obs, "svc", "v1", uuid.Nil, uuid.Nil)
	finish()

	ended := rec.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 span, got %d", len(ended))
	}
}
