package witness

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofrs/uuid/v5"
	"slices"
	"time"
)

func appendError(records []Record, err error) []Record {
	if err == nil {
		return records
	}
	return append(records, record{
		key:   "err",
		value: err.Error(),
	})
}

func Observe(ctx context.Context, eventID uuid.UUID, eventDate time.Time, eventType EventType, eventName string, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(eventID, eventDate, eventType, eventName, caller(1), records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), msg, caller(1), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogWarn(), msg, caller(1), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogDebug(), msg, caller(1), records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1), appendError(records, err)...)
}

func ErrorRF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorOrInfo(ctx context.Context, okMsg, errMsg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	if err != nil {
		c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), errMsg, caller(1), appendError(records, err)...)
	} else {

		c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), okMsg, caller(1), records...)
	}
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorStorage(), msg, caller(1), appendError(records, err)...)
}

func ErrorStorageF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorStorage(), msg, caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorNetwork(), msg, caller(1), appendError(records, err)...)
}

func ErrorNetworkF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorNetwork(), msg, caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorExternal(), msg, caller(1), appendError(records, err)...)
}

func ErrorExternalF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorExternal(), msg, caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorInternal(), msg, caller(1), appendError(records, err)...)
}

func ErrorInternalF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = From(ctx)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorInternal(), msg, caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

// withChildSpan returns a Context whose chain has one more span_id but the
// same observer, trace_id and service_name as the parent. Used by Span /
// Service / Worker and by any other helper that opens a new span under
// the current request.
func (c Context) withChildSpan(spanID uuid.UUID) Context {
	return Context{
		t:           c.t,
		observer:    c.observer,
		spanIDs:     append(slices.Clone(c.spanIDs), spanID),
		traceID:     c.traceID,
		serviceName: c.serviceName,
	}
}

// Trace opens a child span like Span, but also mints a fresh trace_id and
// scopes it to that span and its descendants. Use this at request-entry
// boundaries (HTTP handler, queue consumer) when you want a per-request
// trace_id distinct from the surrounding Instance's trace_id. Inside the
// returned context, every Span/Info/Error inherits this fresh trace_id;
// propagation.Inject(req.Header, c.TraceID(), c.CurrentSpanID()) carries
// it across to downstream services, where InstanceContinue adopts it.
func Trace(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var at = caller(1)
	parent := From(ctx)
	newSpan := uuid.Must(uuid.NewV7())
	nc := Context{
		t:           parent.t,
		observer:    parent.observer,
		spanIDs:     append(slices.Clone(parent.spanIDs), newSpan),
		traceID:     newSpan,
		serviceName: parent.serviceName,
	}
	if nc.t != nil {
		nc.t.Helper()
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanStart(), spanName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if nc.t != nil {
			nc.t.Helper()
		}
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanFinish(), spanName, at, records...)
	}
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var at = caller(1)
	var nc = From(ctx).withChildSpan(uuid.Must(uuid.NewV7()))
	if nc.t != nil {
		nc.t.Helper()
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanStart(), spanName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if nc.t != nil {
			nc.t.Helper()
		}
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanFinish(), spanName, at, records...)
	}
}

func SpanStart(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = From(ctx).withChildSpan(spanID)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanStart(), spanName, caller(1), records...)
}

func SpanFinish(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = From(ctx).withChildSpan(spanID)
	if c.t != nil {
		c.t.Helper()
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanFinish(), spanName, caller(1), records...)
}

// Service is Span with span:service:start/finish event types.
func Service(ctx context.Context, serviceName string, records ...Record) (context.Context, Finish) {
	var at = caller(1)
	nc := From(ctx).withChildSpan(uuid.Must(uuid.NewV7()))
	if nc.t != nil {
		nc.t.Helper()
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanServiceStart(), serviceName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if nc.t != nil {
			nc.t.Helper()
		}
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanServiceFinish(), serviceName, at, records...)
	}
}

// Worker is Span with span:wait_group:start/finish event types.
func Worker(ctx context.Context, workerName string, records ...Record) (context.Context, Finish) {
	var at = caller(1)
	nc := From(ctx).withChildSpan(uuid.Must(uuid.NewV7()))
	if nc.t != nil {
		nc.t.Helper()
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanWorkerStart(), workerName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if nc.t != nil {
			nc.t.Helper()
		}
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanWorkerFinish(), workerName, at, records...)
	}
}

// InternalMessageSent emits a span:internal_message:sent event carrying msgID
// in span_ids. Pair it with InternalMessageReceived on the recipient side; a
// query for the shared msgID reconnects both sides of the hand-off.
func InternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	From(ctx).withChildSpan(msgID).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInternalMessageSent(), msgName, caller(1), records...)
}

func InternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	From(ctx).withChildSpan(msgID).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInternalMessageReceived(), msgName, caller(1), records...)
}

// ExternalMessageSent is InternalMessageSent across a witness-system boundary
// (outbound HTTP, third-party RPC, etc).
func ExternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	From(ctx).withChildSpan(msgID).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanExternalMessageSent(), msgName, caller(1), records...)
}

func ExternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	From(ctx).withChildSpan(msgID).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanExternalMessageReceived(), msgName, caller(1), records...)
}

// InstanceContinue is Instance for the receiving side of a cross-service
// boundary. It mints a fresh root span_id (so the receiver's own spans live
// under a distinct subtree from the caller's) but *adopts* the upstream
// trace_id so that every span this Context emits is grouped with the
// caller's spans into one logical trace. The span:instance:online event is
// additionally stamped with (parentTraceID, parentSpanID) for observers
// that materialize a cross-service-edges view (postgres, otel).
//
// parentTraceID/parentSpanID are typically the values returned by
// propagation.Extract. If parentTraceID is uuid.Nil the call collapses to
// a plain Instance — there is no upstream to attach to.
func InstanceContinue(ctx context.Context, observer Observer, instanceName, instanceVersion string,
	parentTraceID, parentSpanID uuid.UUID, records ...Record) (context.Context, Finish) {
	var at = caller(1)
	if observer == nil {
		observer = NilObserver{}
	}
	if parentTraceID == uuid.Nil {
		return instance(ctx, observer, instanceName, instanceVersion, at, records...)
	}
	c := Context{
		// Instance overrides the trace state of any Context already on ctx,
		// but t is test plumbing, not trace state — carry it through.
		t:           From(ctx).t,
		observer:    observer,
		spanIDs:     []uuid.UUID{uuid.Must(uuid.NewV7())},
		traceID:     parentTraceID,
		serviceName: instanceName,
	}
	recordVersion := record{key: "version", value: instanceVersion}
	observer.Observe(Event{
		SpanIDs:       c.spanIDs,
		EventID:       uuid.Must(uuid.NewV7()),
		EventDate:     time.Now(),
		EventType:     EventTypeSpanInstanceOnline(),
		EventMessage:  instanceName,
		EventCaller:   at,
		Records:       append(records, recordVersion),
		TraceID:       c.traceID,
		ParentTraceID: parentTraceID,
		ParentSpanID:  parentSpanID,
		ServiceName:   c.serviceName,
	})
	return With(ctx, c), func(records ...Record) {
		c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInstanceOffline(), instanceName, at, append(records, recordVersion)...)
	}
}

// Instance starts a new originating trace. Mints a fresh root span_id and
// uses it as both the local root and the trace_id. Overrides any existing
// witness Context already stored on ctx.
func Instance(ctx context.Context, observer Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, Finish) {
	return instance(ctx, observer, instanceName, instanceVersion, caller(1), records...)
}

// instance is Instance with the call site passed in, so that a delegating
// caller (InstanceContinue with no upstream trace) still reports its own
// caller rather than the delegation itself.
func instance(ctx context.Context, observer Observer, instanceName, instanceVersion, at string, records ...Record) (context.Context, Finish) {
	if observer == nil {
		observer = NilObserver{}
	}
	rootSpan := uuid.Must(uuid.NewV7())
	c := Context{
		t:           From(ctx).t,
		observer:    observer,
		spanIDs:     []uuid.UUID{rootSpan},
		traceID:     rootSpan,
		serviceName: instanceName,
	}
	recordVersion := record{key: "version", value: instanceVersion}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInstanceOnline(), instanceName, at, append(records, recordVersion)...)
	return With(ctx, c), func(records ...Record) {
		c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInstanceOffline(), instanceName, at, append(records, recordVersion)...)
	}
}
