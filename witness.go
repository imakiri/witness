package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

func trim(str string, length int) string {
	var last int
	for i, _ := range str {
		if i > length {
			break
		}
		last = i
	}
	return str[:last]
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
	From(ctx).observe(ctx, 1, 0, eventType, eventName, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogInfo(), msg, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogWarn(), msg, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogDebug(), msg, records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogError(), msg, appendError(records, err)...)
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogErrorStorage(), msg, appendError(records, err)...)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogErrorNetwork(), msg, appendError(records, err)...)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogErrorExternal(), msg, appendError(records, err)...)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).observe(ctx, 1, 0, EventTypeLogErrorInternal(), msg, appendError(records, err)...)
}

type Finish func(records ...Record)

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var messageID = uuid.Must(uuid.NewV7()) // messageID as spanID
	var outerContext = From(ctx)
	outerContext.Append(messageID).observe(ctx, 2, 1, EventTypeMessageSent(), spanName)

	var innerContext = NewContext(outerContext, uuid.Must(uuid.NewV7()))
	innerContext.observe(ctx, 2, 0, EventTypeSpanStart(), spanName, records...)
	innerContext.Append(messageID).observe(ctx, 2, 0, EventTypeMessageReceived(), spanName)

	return With(ctx, innerContext), func(records ...Record) {
		var messageID = uuid.Must(uuid.NewV7())
		innerContext.Append(messageID).observe(ctx, 1, 0, EventTypeMessageSent(), spanName)
		innerContext.observe(ctx, 1, 0, EventTypeSpanFinish(), spanName, records...)
		outerContext.Append(messageID).observe(ctx, 1, 1, EventTypeMessageReceived(), spanName)
	}
}

// ServiceBegin creates standalone span and links it to existing one
func ServiceBegin(ctx context.Context, serviceName string, records ...Record) Context {
	var c = From(ctx)
	var linkID = uuid.Must(uuid.NewV7()) // linkID as spanID
	var serviceContext = NewContext(c, uuid.Must(uuid.NewV7()))

	c.Append(linkID).observe(ctx, 1, 0, EventTypeLink(), serviceName, records...)
	serviceContext.observe(ctx, 2, 0, EventTypeServiceBegin(), serviceName, records...)
	serviceContext.Append(linkID).observe(ctx, 1, 0, EventTypeLink(), serviceName, records...)
	return serviceContext
}

func ServiceEnd(ctx context.Context, c Context, records ...Record) {
	c.observe(ctx, 2, 0, EventTypeServiceEnd(), "", records...)
}

// Instance overrides any existing witness context withing ctx with new one
func Instance(ctx context.Context, debug bool, observer Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, Finish) {
	if observer == nil {
		observer = NilObserver{}
	}
	var spanID = uuid.Must(uuid.NewV7())
	var c = Context{
		debug:    debug,
		observer: observer,
		spanIDs:  []uuid.UUID{spanID},
	}
	var recordVersion = record{
		key:   "version",
		value: instanceVersion,
	}
	c.observe(ctx, 2, 0, EventTypeInstanceOnline(), instanceName, append(records, recordVersion)...)
	return With(ctx, c), func(records ...Record) {
		c.observe(ctx, 1, 0, EventTypeInstanceOffline(), instanceName, append(records, recordVersion)...)
	}
}
