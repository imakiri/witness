package witness

import (
	"context"
	"fmt"
	"github.com/gofrs/uuid/v5"
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

func Observe(ctx context.Context, eventType EventType, eventName string, records ...Record) {
	From(ctx).Observe(ctx, eventType, eventName, caller(1, 0), records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogInfo(), msg, caller(1, 0), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogWarn(), msg, caller(1, 0), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogDebug(), msg, caller(1, 0), records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogError(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorOrInfo(ctx context.Context, okMsg, errMsg string, err error, records ...Record) {
	if err != nil {
		From(ctx).Observe(ctx, EventTypeLogError(), errMsg, caller(1, 0), appendError(records, err)...)
	} else {
		From(ctx).Observe(ctx, EventTypeLogInfo(), okMsg, caller(1, 0), records...)
	}
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogErrorStorage(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogErrorNetwork(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogErrorExternal(), msg, caller(1, 0), appendError(records, err)...)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Observe(ctx, EventTypeLogErrorInternal(), msg, caller(1, 0), appendError(records, err)...)
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var c = From(ctx)
	var spanID = uuid.Must(uuid.NewV7())
	var spanIDs = []uuid.UUID{c.spanID, spanID}
	c.Observer().Observe(ctx, spanIDs, EventTypeSpanStart(), spanName, caller(1, 0), records...)
	c.spanID = spanID
	return c.To(ctx), func(records ...Record) {
		spanIDs[0], spanIDs[1] = spanIDs[1], spanIDs[0]
		c.Observer().Observe(ctx, spanIDs, EventTypeSpanFinish(), spanName, caller(0, 1), records...)
	}
}

func SpanStart(ctx context.Context, spanName string, records ...Record) context.Context {
	var c = From(ctx)
	var spanID = uuid.Must(uuid.NewV7())
	var spanIDs = []uuid.UUID{c.spanID, spanID}
	ctx = context.WithValue(ctx, fmt.Sprint(keyContext, ":", spanName), spanIDs)
	c.Observer().Observe(ctx, spanIDs, EventTypeSpanStart(), spanName, caller(1, 0), records...)
	c.spanID = spanID
	return c.To(ctx)
}

func SpanFinish(ctx context.Context, spanName string, records ...Record) {
	var spanIDs = ctx.Value(fmt.Sprint(keyContext, ":", spanName)).([]uuid.UUID)
	From(ctx).Observer().Observe(ctx, spanIDs, EventTypeSpanFinish(), spanName, caller(0, 1), records...)
}

func Service(ctx context.Context, serviceName string, records ...Record) (context.Context, Finish) {
	var c = From(ctx)
	var spanID = uuid.Must(uuid.NewV7())
	var spanIDs = []uuid.UUID{c.spanID, spanID}
	c.Observer().Observe(ctx, spanIDs, EventTypeSpanServiceBegin(), serviceName, caller(1, 0), records...)
	c.spanID = spanID
	return c.To(ctx), func(records ...Record) {
		spanIDs[0], spanIDs[1] = spanIDs[1], spanIDs[0]
		c.Observer().Observe(ctx, spanIDs, EventTypeSpanServiceEnd(), serviceName, caller(0, 1), records...)
	}
}

func InternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	c.Observer().Observe(ctx, []uuid.UUID{c.spanID, msgID}, EventTypeSpanInternalMessageSent(), msgName, caller(1, 0), records...)
}

func InternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	c.Observer().Observe(ctx, []uuid.UUID{msgID, c.spanID}, EventTypeSpanInternalMessageReceived(), msgName, caller(1, 0), records...)
}

func ExternalMessage(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) Finish {
	var c = From(ctx)
	c.Observer().Observe(ctx, []uuid.UUID{c.spanID, msgID}, EventTypeSpanExternalMessageSent(), msgName, caller(1, 0), records...)
	return func(records ...Record) {
		c.Observer().Observe(ctx, []uuid.UUID{msgID, c.spanID}, EventTypeSpanExternalMessageReceived(), msgName, caller(1, 1), records...)
	}
}

func ExternalMessageSent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	c.Observer().Observe(ctx, []uuid.UUID{c.spanID, msgID}, EventTypeSpanExternalMessageSent(), msgName, caller(1, 0), records...)
}

func ExternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = From(ctx)
	c.Observer().Observe(ctx, []uuid.UUID{msgID, c.spanID}, EventTypeSpanExternalMessageReceived(), msgName, caller(1, 0), records...)
}

// Instance overrides any existing witness context within ctx with a new one
func Instance(ctx context.Context, observer Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, Finish) {
	if observer == nil {
		observer = NilObserver{}
	}
	var c = Context{
		observer: observer,
		spanID:   uuid.Must(uuid.NewV7()),
	}
	var recordVersion = record{
		key:   "version",
		value: instanceVersion,
	}
	c.Observe(ctx, EventTypeSpanInstanceOnline(), instanceName, caller(1, 0), append(records, recordVersion)...)
	return With(ctx, c), func(records ...Record) {
		c.Observe(ctx, EventTypeSpanInstanceOffline(), instanceName, caller(1, 0), append(records, recordVersion)...)
	}
}
