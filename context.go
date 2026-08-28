package witness

import (
	"bytes"
	"context"
	"github.com/gofrs/uuid/v5"
	"slices"
	"testing"
	"time"
)

// Context carries the per-goroutine witness state: the observer that
// receives events and the span chain identifying where in the call graph
// those events originate.
//
// traceID is a *logical* request identifier shared by every span produced
// while handling that request, including across cross-service hops. It is
// set once at the entry of a request (witness.Instance for the originating
// service, witness.InstanceContinue for a receiving service that took the
// trace_id off the wire) and inherited unchanged by witness.Span and its
// kin. RootSpanID is the span's own root, which differs across services;
// TraceID is the same across all services for one request — that is what
// observers (postgres, otlp, stdlog) use to group spans into one trace.
//
// serviceName is the local instance's name (the value passed to Instance /
// InstanceContinue). Every event emitted by this Context carries it so
// observers can group / filter / colour by service without reconstructing
// the span chain at query time. Inherited unchanged by every child Context.
type Context struct {
	t           *testing.T
	observer    Observer
	spanIDs     []uuid.UUID
	traceID     uuid.UUID
	serviceName string
}

func (c Context) IsNil() bool {
	return c.observer == nil || c.spanIDs == nil
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanIDs() []uuid.UUID {
	return c.spanIDs
}

// TraceID returns the logical request identifier shared by every span in
// this trace, across services. Returns uuid.Nil if the context was built
// without a trace_id (e.g. From() on a context that has no witness state).
func (c Context) TraceID() uuid.UUID {
	return c.traceID
}

// ServiceName returns the local instance's name (whatever was passed as
// instanceName to Instance / InstanceContinue). Empty for contexts that
// never went through an Instance constructor.
func (c Context) ServiceName() string {
	return c.serviceName
}

// RootSpanID returns the first span_id in this Context's own chain — the
// span minted at the local Instance/InstanceContinue boundary. Distinct
// from TraceID once a request has hopped to another service.
func (c Context) RootSpanID() uuid.UUID {
	if len(c.spanIDs) == 0 {
		return uuid.Nil
	}
	return c.spanIDs[0]
}

// CurrentSpanID returns the deepest span_id in the chain — the span the
// caller is currently inside. Returns uuid.Nil for empty contexts.
func (c Context) CurrentSpanID() uuid.UUID {
	if len(c.spanIDs) == 0 {
		return uuid.Nil
	}
	return c.spanIDs[len(c.spanIDs)-1]
}

// NewContext builds a Context with a fresh root span_id. The trace_id is
// set equal to that root — i.e. this is the *originating* service in the
// trace. Receivers should construct via InstanceContinue instead so they
// adopt the caller's trace_id.
func NewContext(observer Observer) Context {
	rootSpan := uuid.Must(uuid.NewV7())
	return Context{
		observer: observer,
		spanIDs:  []uuid.UUID{rootSpan},
		traceID:  rootSpan,
	}
}

func NewTestContext(t *testing.T, observer Observer) Context {
	var c = NewContext(observer)
	c.t = t
	return c
}

// Join merges span chains from other contexts. The trace_id and
// service_name are preserved from the receiver — joining does not change
// which trace or which service this Context belongs to.
func (c Context) Join(cts ...Context) Context {
	var spanIDs = make([]uuid.UUID, len(c.spanIDs), len(c.spanIDs)+len(cts))
	copy(spanIDs, c.spanIDs)
	for _, ctx := range cts {
		spanIDs = append(spanIDs, ctx.SpanIDs()...)
	}
	slices.SortFunc(spanIDs, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return Context{
		observer:    c.observer,
		spanIDs:     slices.Clone(slices.Compact(spanIDs)),
		traceID:     c.traceID,
		serviceName: c.serviceName,
	}
}

func (c Context) Observe(eventID uuid.UUID, eventDate time.Time, eventType EventType, eventName string, eventCaller string, records ...Record) {
	if c.observer == nil {
		return
	}
	if c.t != nil {
		c.t.Helper()
	}
	c.observer.Observe(Event{
		SpanIDs:      c.spanIDs,
		EventID:      eventID,
		EventDate:    eventDate,
		EventType:    eventType,
		EventMessage: eventName,
		EventCaller:  eventCaller,
		Records:      records,
		TraceID:      c.traceID,
		ServiceName:  c.serviceName,
	})
}

func (c Context) Info(msg string, records ...Record) {
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), msg, caller(1), records...)
}

func (c Context) Warn(msg string, records ...Record) {
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogWarn(), msg, caller(1), records...)
}

func (c Context) Debug(msg string, records ...Record) {
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogDebug(), msg, caller(1), records...)
}

func (c Context) Error(msg string, err error, records ...Record) {
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1), appendError(records, err)...)
}

type Finish func(records ...Record)

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, keyContext, c)
}

func (c Context) To(ctx context.Context) context.Context {
	return With(ctx, c)
}

func From(ctx context.Context) Context {
	cs, ok := ctx.Value(keyContext).(Context)
	if ok {
		return cs
	}
	return Context{observer: NilObserver{}}
}

func Join(ctx context.Context, cts ...context.Context) context.Context {
	var contexts = make([]Context, len(cts))
	for i := range cts {
		contexts[i] = From(cts[i])
	}
	return From(ctx).Join(contexts...).To(ctx)
}
