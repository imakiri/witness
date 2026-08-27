// Package otlp exports witness events as OpenTelemetry spans over OTLP
// (gRPC or HTTP).
//
// Span/instance start events open an OTel span; the matching finish ends it.
// Log events become AddEvent on the current span; log:error events also set
// the span status to Error and record the err record (if present).
// Message events (internal/external) become AddEvent with the msg_id attached
// as an attribute. Metric events are dropped — use the prometheus observer.
//
// trace_id is the first 16 bytes of the root witness span_id; span_id is the
// last 8 bytes of the current one. Pure byte copies, no string parsing.
package otlp

import (
	"context"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const TracerName = "github.com/imakiri/witness"

type Config struct {
	Provider *sdktrace.TracerProvider
	// SpanTTL force-ends spans that never received a finish event. Zero disables.
	SpanTTL time.Duration
}

type Observer struct {
	tracer   trace.Tracer
	provider *sdktrace.TracerProvider
	reg      *registry
	ttl      time.Duration
}

func NewObserver(cfg Config) (*Observer, error) {
	if cfg.Provider == nil {
		return nil, fmt.Errorf("otlp: Config.Provider is required")
	}
	return &Observer{
		tracer:   cfg.Provider.Tracer(TracerName),
		provider: cfg.Provider,
		reg:      &registry{},
		ttl:      cfg.SpanTTL,
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
	rootID, _ := rootSpanID(event)
	parentCtx := o.parentContext(event, rootID)
	attrs := recordsToAttributes(event.Records)
	attrs = append(attrs,
		attribute.String("witness.event_caller", event.EventCaller),
		attribute.String("witness.event_type", event.EventType.String()),
	)
	_, span := o.tracer.Start(parentCtx, event.EventMessage,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	)
	o.reg.Set(curID, span)
}

// parentContext nests the new span under its registered parent if any. For
// cross-service continuation (ParentTraceID set) it pins parent to the
// upstream SpanContext. Otherwise it synthesizes a remote SpanContext from
// the root span_id so every span in the same witness instance shares one
// trace_id.
func (o *Observer) parentContext(event witness.Event, rootID uuid.UUID) context.Context {
	ctx := context.Background()
	if parent, ok := parentSpanID(event); ok {
		if parentSpan, found := o.reg.Get(parent); found {
			return trace.ContextWithSpan(ctx, parentSpan)
		}
	}
	if event.ParentTraceID != uuid.Nil {
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceIDFromUUID(event.ParentTraceID),
			SpanID:     spanIDFromUUID(event.ParentSpanID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		})
		if sc.IsValid() {
			return trace.ContextWithSpanContext(ctx, sc)
		}
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceIDFromUUID(rootID),
		SpanID:     spanIDFromUUID(rootID),
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
