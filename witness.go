package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

var debug = false

func EnableDebug() {
	debug = true
}

const mainContextName = "instance"

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
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, eventType, eventName, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogInfo(), msg, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogWarn(), msg, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogDebug(), msg, records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogError(), msg, appendError(records, err)...)
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogErrorStorage(), msg, appendError(records, err)...)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogErrorNetwork(), msg, appendError(records, err)...)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogErrorExternal(), msg, appendError(records, err)...)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	From(ctx).Context(mainContextName).observe(ctx, 1, 0, EventTypeLogErrorInternal(), msg, appendError(records, err)...)
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	return From(ctx).Context(mainContextName).span(ctx, mainContextName, spanName, records...)
}

func ServiceBegin(ctx context.Context, serviceName string, records ...Record) (context.Context, Finish) {
	return From(ctx).Context(mainContextName).serviceSpan(ctx, serviceName, serviceName, records...)
}

func ServiceEnd(ctx context.Context, serviceName string, records ...Record) {
	From(ctx).Context(serviceName).observe(ctx, 2, 0, EventTypeServiceEnd(), "", records...)
}

// Instance overrides any existing witness context within ctx with a new one
func Instance(ctx context.Context, observer Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, Finish) {
	if observer == nil {
		observer = NilObserver{}
	}
	var spanID = uuid.Must(uuid.NewV7())
	var c = Context{
		observer:    observer,
		contextName: mainContextName,
		spanIDs:     []uuid.UUID{spanID},
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
