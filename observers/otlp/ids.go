package otlp

import (
	"context"
	crand "crypto/rand"

	"github.com/gofrs/uuid/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type keyIDs struct{}

type witnessIDs struct {
	traceID trace.TraceID
	spanID  trace.SpanID
}

// withIDs hands the witness ids of the span about to start to the IDGenerator.
// The SDK passes the ctx given to tracer.Start into the generator, so this is
// the only way to make the OTel span_id equal the witness one.
func withIDs(ctx context.Context, traceID, spanID uuid.UUID) context.Context {
	return context.WithValue(ctx, keyIDs{}, witnessIDs{
		traceID: traceIDFromUUID(traceID),
		spanID:  spanIDFromUUID(spanID),
	})
}

// IDGenerator returns the trace and span ids the Observer put into ctx, and
// random ones otherwise, so the provider stays usable for plain OTel spans.
//
// Without it OTel span ids are random, a traceparent built from witness ids
// points at a span that does not exist, and every hop starts a detached
// subtree. NewTraceProvider wires it in; a provider passed to Config.Provider
// must be built with sdktrace.WithIDGenerator(otlp.IDGenerator()).
func IDGenerator() sdktrace.IDGenerator {
	return idGenerator{}
}

type idGenerator struct{}

func (idGenerator) NewIDs(ctx context.Context) (trace.TraceID, trace.SpanID) {
	if ids, ok := ctx.Value(keyIDs{}).(witnessIDs); ok && ids.traceID.IsValid() && ids.spanID.IsValid() {
		return ids.traceID, ids.spanID
	}
	var traceID trace.TraceID
	var spanID trace.SpanID
	_, _ = crand.Read(traceID[:])
	_, _ = crand.Read(spanID[:])
	return traceID, spanID
}

func (idGenerator) NewSpanID(ctx context.Context, _ trace.TraceID) trace.SpanID {
	if ids, ok := ctx.Value(keyIDs{}).(witnessIDs); ok && ids.spanID.IsValid() {
		return ids.spanID
	}
	var spanID trace.SpanID
	_, _ = crand.Read(spanID[:])
	return spanID
}
