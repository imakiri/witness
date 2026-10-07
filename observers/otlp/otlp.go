// Package otlp exports witness events as OpenTelemetry spans over OTLP
// (gRPC or HTTP).
//
// Span/instance start events open an OTel span; the matching finish ends it.
// Log events become AddEvent on the current span; log:error events also set
// the span status to Error and record the err record (if present).
// Message events (internal/external) become AddEvent with the msg_id attached
// as an attribute. Metric events are dropped — use the prometheus observer.
//
// trace_id is the witness trace_id (the root span_id, or the upstream one after
// InstanceContinue); span_id is the last 8 bytes of the current witness span_id.
// Pure byte copies, no string parsing. Both reach OTel through IDGenerator, so a
// traceparent built by propagation.Inject names a span that exists in the
// backend and the receiver nests under it.
package otlp

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const TracerName = "github.com/imakiri/witness"

type Config struct {
	// Provider must be built with sdktrace.WithIDGenerator(IDGenerator()), as
	// NewTraceProvider does. Without it span ids are random and traces do not
	// chain across hops.
	Provider *sdktrace.TracerProvider
}

// Observer holds one live otel trace.Span per open witness span, in reg.
// A span whose finish event never arrives (process killed mid-span, an
// observer dropped the finish, a Finish closure never called) stays in reg
// for the lifetime of the Observer — witness has no span timeout, and
// force-ending a span would invent a duration the data model does not have.
// If a service leaks spans faster than it closes them, that is a bug in the
// service, and reg growing is how you see it.
type Observer struct {
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
	reg      *registry
}

func NewObserver(cfg Config) (*Observer, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("otlp: Config.Provider is required")
	}
	return &Observer{
		tracer:   cfg.Provider.Tracer(TracerName),
		provider: cfg.Provider,
		reg:      &registry{},
	}, nil
}

func (o *Observer) Shutdown(ctx context.Context) error {
	return o.provider.Shutdown(ctx)
}

func (o *Observer) Observe(event witness.Event) {
	switch event.EventType {
	case witness.EventTypeSpanStart(),
		witness.EventTypeSpanServiceStart(),
		witness.EventTypeSpanWorkerStart(),
		witness.EventTypeSpanInstanceOnline():
		o.startSpan(event)

	case witness.EventTypeSpanFinish(),
		witness.EventTypeSpanServiceFinish(),
		witness.EventTypeSpanWorkerFinish(),
		witness.EventTypeSpanInstanceOffline():
		o.finishSpan(event)

	case witness.EventTypeLogInfo(),
		witness.EventTypeLogWarn(),
		witness.EventTypeLogDebug():
		o.addEvent(event)

	case witness.EventTypeLogError(),
		witness.EventTypeLogErrorStorage(),
		witness.EventTypeLogErrorNetwork(),
		witness.EventTypeLogErrorExternal(),
		witness.EventTypeLogErrorInternal():
		o.recordError(event)

	case witness.EventTypeSpanInternalMessageSent(),
		witness.EventTypeSpanExternalMessageSent():
		o.messageSent(event)

	case witness.EventTypeSpanInternalMessageReceived(),
		witness.EventTypeSpanExternalMessageReceived():
		o.messageReceived(event)
	}
}

func currentSpanID(event witness.Event) (uuid.UUID, bool) {
	if len(event.SpanIDs) == 0 {
		return uuid.Nil, false
	}
	return event.SpanIDs[len(event.SpanIDs)-1], true
}

func parentSpanID(event witness.Event) (uuid.UUID, bool) {
	if len(event.SpanIDs) < 2 {
		return uuid.Nil, false
	}
	return event.SpanIDs[len(event.SpanIDs)-2], true
}

func rootSpanID(event witness.Event) (uuid.UUID, bool) {
	if len(event.SpanIDs) == 0 {
		return uuid.Nil, false
	}
	return event.SpanIDs[0], true
}

func (o *Observer) startSpan(event witness.Event) {
	curID, ok := currentSpanID(event)
	if !ok {
		return
	}
	traceID := event.TraceID
	if traceID == uuid.Nil {
		traceID, _ = rootSpanID(event)
	}
	parentCtx, opts := o.parentContext(event, traceID)
	parentCtx = withIDs(parentCtx, traceID, curID)
	attrs := recordsToAttributes(event.Records)
	attrs = append(attrs,
		attribute.String("witness.event_caller", event.EventCaller),
		attribute.String("witness.event_type", event.EventType.String()),
	)
	opts = append(opts,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	)
	_, span := o.tracer.Start(parentCtx, event.EventMessage, opts...)
	o.reg.Set(curID, span)
}

// parentContext picks the parent of the span about to start; the span's own
// ids come from IDGenerator via withIDs.
//
//   - Registered parent: nest under the live span. If witness.Trace switched
//     the trace_id mid-chain, start a new root linked to that parent instead —
//     OTel would otherwise keep the parent's trace_id while traceparent carries
//     the new one.
//   - Cross-service continuation (ParentTraceID set): pin the parent to the
//     upstream SpanContext from traceparent.
//   - Instance root (one span in the chain): no parent at all. A synthesized
//     parent would carry the span's own id and make it its own parent.
//   - Parent never started (NewContext root, lost start event): reference the
//     direct parent's id remotely so the span stays in its trace.
func (o *Observer) parentContext(event witness.Event, traceID uuid.UUID) (context.Context, []trace.SpanStartOption) {
	ctx := context.Background()
	parent, hasParent := parentSpanID(event)
	if hasParent {
		if parentSpan, found := o.reg.Get(parent); found {
			// witness.Trace mints the new trace_id from the span it opens; no other
			// constructor makes a child its own trace.
			if cur, _ := currentSpanID(event); traceID == cur {
				return ctx, []trace.SpanStartOption{trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: parentSpan.SpanContext()})}
			}
			return trace.ContextWithSpan(ctx, parentSpan), nil
		}
	}
	if event.ParentTraceID != uuid.Nil {
		return remoteParent(ctx, event.ParentTraceID, event.ParentSpanID), nil
	}
	if !hasParent {
		return ctx, nil
	}
	return remoteParent(ctx, traceID, parent), nil
}

func remoteParent(ctx context.Context, traceID, spanID uuid.UUID) context.Context {
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceIDFromUUID(traceID),
		SpanID:     spanIDFromUUID(spanID),
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	if !sc.IsValid() {
		return ctx
	}
	return trace.ContextWithSpanContext(ctx, sc)
}

func (o *Observer) finishSpan(event witness.Event) {
	curID, ok := currentSpanID(event)
	if !ok {
		return
	}
	span, found := o.reg.Get(curID)
	if !found {
		return
	}
	attrs := recordsToAttributes(event.Records)
	if len(attrs) > 0 {
		span.SetAttributes(attrs...)
	}
	span.End(trace.WithTimestamp(event.EventDate))
	o.reg.Delete(curID)
}

func (o *Observer) addEvent(event witness.Event) {
	curID, ok := currentSpanID(event)
	if !ok {
		return
	}
	span, found := o.reg.Get(curID)
	if !found {
		return
	}
	span.AddEvent(event.EventMessage,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(recordsToAttributes(event.Records)...),
	)
}

func (o *Observer) recordError(event witness.Event) {
	curID, ok := currentSpanID(event)
	if !ok {
		return
	}
	span, found := o.reg.Get(curID)
	if !found {
		return
	}
	attrs := recordsToAttributes(event.Records)
	span.SetStatus(codes.Error, event.EventMessage)
	span.SetAttributes(attrs...)
	if errStr, ok := findRecord(event.Records, "err"); ok {
		span.RecordError(fmt.Errorf("%s", errStr),
			trace.WithTimestamp(event.EventDate),
			trace.WithAttributes(attrs...),
		)
	}
	span.AddEvent(event.EventMessage,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	)
}

func (o *Observer) messageSent(event witness.Event) {
	msgID, ok := currentSpanID(event)
	if !ok {
		return
	}
	carrierID, ok := parentSpanID(event)
	if !ok {
		return
	}
	span, found := o.reg.Get(carrierID)
	if !found {
		return
	}
	attrs := recordsToAttributes(event.Records)
	attrs = append(attrs,
		attribute.String("witness.message_id", msgID.String()),
		attribute.String("witness.event_type", event.EventType.String()),
	)
	span.AddEvent(event.EventMessage,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	)
}

func (o *Observer) messageReceived(event witness.Event) {
	o.messageSent(event)
}

func recordsToAttributes(records []witness.Record) []attribute.KeyValue {
	if len(records) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(records))
	for _, r := range records {
		key := string(r.AppendKey(nil))
		value := string(r.AppendValue(nil))
		attrs = append(attrs, attribute.String(key, value))
	}
	return attrs
}

func findRecord(records []witness.Record, key string) (string, bool) {
	for _, r := range records {
		if r.KeyEqual(key) {
			return string(r.AppendValue(nil)), true
		}
	}
	return "", false
}
