package witness

import (
	"context"
	"errors"
	"fmt"
	"github.com/gofrs/uuid/v5"
	"slices"
	"time"
)

var debug = false

func EnableDebug() {
	debug = true
}

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
	From(ctx).Observe(eventID, eventDate, eventType, eventName, caller(1, 0), records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), msg, caller(1, 0), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogWarn(), msg, caller(1, 0), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogDebug(), msg, caller(1, 0), records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorF(ctx context.Context, msg string, err error, records ...Record) error {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1, 0), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorOrInfo(ctx context.Context, okMsg, errMsg string, err error, records ...Record) {
	if err != nil {
		From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), errMsg, caller(1, 0), appendError(records, err)...)
	} else {
		From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), okMsg, caller(1, 0), records...)
	}
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorStorage(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorStorageF(ctx context.Context, msg string, err error, records ...Record) error {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorStorage(), msg, caller(1, 0), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorNetwork(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorNetworkF(ctx context.Context, msg string, err error, records ...Record) error {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorNetwork(), msg, caller(1, 0), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorExternal(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorExternalF(ctx context.Context, msg string, err error, records ...Record) error {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorExternal(), msg, caller(1, 0), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorInternal(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorInternalF(ctx context.Context, msg string, err error, records ...Record) error {
	From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogErrorInternal(), msg, caller(1, 0), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var c = From(ctx)
	var nc = Context{
		observer: c.observer,
		spanIDs:  append(slices.Clone(c.spanIDs), uuid.Must(uuid.NewV7())),
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanStart(), spanName, caller(1, 0), records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanFinish(), spanName, caller(0, 1), records...)
	}
}

func SpanStart(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), spanID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanStart(),
		EventMessage: spanName,
		EventCaller:  caller(1, 0),
		Records:      records,
	})
}

func SpanFinish(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), spanID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanFinish(),
		EventMessage: spanName,
		EventCaller:  caller(0, 1),
		Records:      records,
	})
}

// Service is Span with span:service:start/finish event types.
func Service(ctx context.Context, serviceName string, records ...Record) (context.Context, Finish) {
	var c = From(ctx)
	var nc = Context{
		observer: c.observer,
		spanIDs:  append(slices.Clone(c.spanIDs), uuid.Must(uuid.NewV7())),
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanServiceStart(), serviceName, caller(1, 0), records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanServiceFinish(), serviceName, caller(0, 1), records...)
	}
}

// Worker is Span with span:wait_group:start/finish event types.
func Worker(ctx context.Context, workerName string, records ...Record) (context.Context, Finish) {
	var c = From(ctx)
	var nc = Context{
		observer: c.observer,
		spanIDs:  append(slices.Clone(c.spanIDs), uuid.Must(uuid.NewV7())),
	}
	nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanWorkerStart(), workerName, caller(1, 0), records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanWorkerFinish(), workerName, caller(0, 1), records...)
	}
}

// InternalMessageSent emits a span:internal_message:sent event carrying msgID
// in span_ids. Pair it with InternalMessageReceived on the recipient side; a
// query for the shared msgID reconnects both sides of the hand-off.
func InternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), msgID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanInternalMessageSent(),
		EventMessage: msgName,
		EventCaller:  caller(1, 0),
		Records:      records,
	})
}

func InternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), msgID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanInternalMessageReceived(),
		EventMessage: msgName,
		EventCaller:  caller(1, 0),
		Records:      records,
	})
}

// ExternalMessageSent is InternalMessageSent across a witness-system boundary
// (outbound HTTP, third-party RPC, etc).
func ExternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), msgID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanExternalMessageSent(),
		EventMessage: msgName,
		EventCaller:  caller(1, 0),
		Records:      records,
	})
}

func ExternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	if c.observer == nil {
		return
	}
	c.observer.Observe(Event{
		SpanIDs:      append(slices.Clone(c.spanIDs), msgID),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    EventTypeSpanExternalMessageReceived(),
		EventMessage: msgName,
		EventCaller:  caller(1, 0),
		Records:      records,
	})
}

// Instance overrides any existing witness context within ctx with a new one
func Instance(ctx context.Context, observer Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, Finish) {
	if observer == nil {
		observer = NilObserver{}
	}
	var c = Context{
		observer: observer,
		spanIDs:  []uuid.UUID{uuid.Must(uuid.NewV7())},
	}
	var recordVersion = record{
		key:   "version",
		value: instanceVersion,
	}
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInstanceOnline(), instanceName, caller(1, 0), append(records, recordVersion)...)
	return With(ctx, c), func(records ...Record) {
		c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeSpanInstanceOffline(), instanceName, caller(1, 0), append(records, recordVersion)...)
	}
}

