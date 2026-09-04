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

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const TracerName = "github.com/imakiri/witness"

type Config struct {
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

	case witness.EventTypeSpanLink(),
		witness.EventTypeSpanInternalMessageSent(),
		witness.EventTypeSpanExternalMessageSent(),
		witness.EventTypeSpanInternalMessageReceived(),
		witness.EventTypeSpanExternalMessageReceived():
		o.linkEvent(event)
	}
}

// byFlag returns the first span_id carrying flag. Event.SpanIDs holds the
// emitter's chain followed by referenced link spans, so position is not
// enough: without the flags a link appended at the tail would be mistaken
// for the current span, registering the otel span under an id its finish
// event never uses.
//
// fallback is the positional answer, used for events with no SpanFlags at
// all — hand-built ones from tests or third-party producers, which never
// carry links either.
func byFlag(event witness.Event, flag witness.SpanFlags, fallback int) (uuid.UUID, bool) {
	if len(event.SpanFlags) == len(event.SpanIDs) {
		for i, f := range event.SpanFlags {
			if f&flag != 0 {
				return event.SpanIDs[i], true
			}
		}
		return uuid.Nil, false
	}
	if fallback < 0 || fallback >= len(event.SpanIDs) {
		return uuid.Nil, false
	}
	return event.SpanIDs[fallback], true
}

func currentSpanID(event witness.Event) (uuid.UUID, bool) {
	return byFlag(event, witness.SpanFlagOwn, len(event.SpanIDs)-1)
}

func parentSpanID(event witness.Event) (uuid.UUID, bool) {
	return byFlag(event, witness.SpanFlagParent, len(event.SpanIDs)-2)
}

// linkedSpanIDs are the spans this event references without being inside
// them — the shared point of a hand-off. OTel models that as a span link,
// not as parentage: the peer's half is the same span seen from the other
// side, not one above ours.
func linkedSpanIDs(event witness.Event) []uuid.UUID {
	if len(event.SpanFlags) != len(event.SpanIDs) {
		return nil
	}
	var out []uuid.UUID
	for i, f := range event.SpanFlags {
		if f&witness.SpanFlagLink != 0 {
			out = append(out, event.SpanIDs[i])
		}
	}
	return out
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
	opts := []trace.SpanStartOption{
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	}
	for _, linkID := range linkedSpanIDs(event) {
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceIDFromUUID(linkID),
			SpanID:     spanIDFromUUID(linkID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		})
		if sc.IsValid() {
			opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
		}
	}
	_, span := o.tracer.Start(parentCtx, event.EventMessage, opts...)
	o.reg.Set(curID, span)
}

// parentContext nests the new span under its registered parent if any.
//
// When the parent is not in the registry the span is grafted onto a
// synthesized remote SpanContext built from rootID — the first span_id of
// the chain, always this process's instance root — so every span of one
// instance shares one OTel trace_id.
func (o *Observer) parentContext(event witness.Event, rootID uuid.UUID) context.Context {
	ctx := context.Background()
	if parent, ok := parentSpanID(event); ok {
		if parentSpan, found := o.reg.Get(parent); found {
			return trace.ContextWithSpan(ctx, parentSpan)
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

// linkEvent records a reference to a shared span_id — a Link / LinkTo or
// either half of a message hand-off — on the span the caller was in.
//
// Each referenced id becomes an OTel span link, which is the primitive for
// exactly this: the peer's half of a shared span is not above ours, it is
// the same span seen from the other side, so it is a link and not a parent.
// The link arrives after the span started, hence Span.AddLink rather than
// trace.WithLinks. An event carrying the same ids is added too, so the
// reference is visible in backends that do not render links.
func (o *Observer) linkEvent(event witness.Event) {
	ownID, ok := currentSpanID(event)
	if !ok {
		return
	}
	span, found := o.reg.Get(ownID)
	if !found {
		return
	}
	attrs := recordsToAttributes(event.Records)
	for _, linkID := range linkedSpanIDs(event) {
		attrs = append(attrs, attribute.String("witness.link_span_id", linkID.String()))
		sc := trace.NewSpanContext(trace.SpanContextConfig{
			TraceID:    traceIDFromUUID(linkID),
			SpanID:     spanIDFromUUID(linkID),
			TraceFlags: trace.FlagsSampled,
			Remote:     true,
		})
		if sc.IsValid() {
			span.AddLink(trace.Link{SpanContext: sc, Attributes: []attribute.KeyValue{
				attribute.String("witness.event_type", event.EventType.String()),
			}})
		}
	}
	attrs = append(attrs,
		attribute.String("witness.event_type", event.EventType.String()),
	)
	span.AddEvent(event.EventMessage,
		trace.WithTimestamp(event.EventDate),
		trace.WithAttributes(attrs...),
	)
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
