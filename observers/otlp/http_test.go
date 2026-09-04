package otlp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/imakiri/witness"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// Sender opens a span, fires an HTTP request through the transport, and the
// downstream instance must be parented to the sender's current span.
//
// The two sides do NOT share an OTel trace_id — witness has none to
// propagate. The traceparent carries one span_id, the receiver enters it
// with SpanStart, and both processes then write events into that same span.
// In the OTel export that shows up as a span *link* rather than parentage:
// the peer's half of the span is not above ours, it is the same span seen
// from the other side.
func TestCrossServiceSpanLink(t *testing.T) {
	senderRec := tracetest.NewSpanRecorder()
	receiverRec := tracetest.NewSpanRecorder()

	senderObs, _ := NewObserver(Config{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(senderRec))})
	receiverObs, _ := NewObserver(Config{Provider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(receiverRec))})

	receiverCtx, finishB := witness.Instance(context.Background(), receiverObs, "service_b", "v1")
	defer finishB()

	srv := httptest.NewServer(Middleware(receiverCtx)(
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
	receiverTrace := receiver[0].SpanContext().TraceID()
	for _, s := range receiver {
		if s.SpanContext().TraceID() != receiverTrace {
			t.Fatalf("receiver spans have divergent trace_ids")
		}
	}

	// The request span the receiver entered is the sender's span_id, so it
	// must carry a link. That link is the cross-service reference; there is
	// no shared trace_id to carry it instead.
	var linked bool
	for _, s := range receiver {
		if len(s.Links()) > 0 {
			linked = true
			for _, l := range s.Links() {
				if !l.SpanContext.IsRemote() {
					t.Fatalf("span link %s must be remote", l.SpanContext.SpanID())
				}
			}
		}
	}
	if !linked {
		t.Fatalf("no receiver span carries a link — cross-service reference missing")
	}
}
