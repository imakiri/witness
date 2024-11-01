package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

func observe(ctx context.Context, skip int, eventType EventType, eventName string, records ...Record) {
	var cxx = From(ctx)
	var eventCallerName, eventCallerPath = caller(3, skip)
	if cxx.Debug {
		cxx.Observer.Observe(ctx, cxx.spanID, eventType, eventName, eventCallerPath, records...)
	} else {
		cxx.Observer.Observe(ctx, cxx.spanID, eventType, eventName, eventCallerName, records...)
	}
}

func Observe(ctx context.Context, eventType EventType, eventName string, records ...Record) {
	observe(ctx, 0, eventType, eventName, records...)
}

func Log(ctx context.Context, eventType EventType, eventName string, records ...Record) {
	observe(ctx, 0, eventType, eventName, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogInfo(), msg, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogWarn(), msg, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogDebug(), msg, records...)
}

func Error(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogError(), msg, records...)
}

func ErrorStorage(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogErrorStorage(), msg, records...)
}

func ErrorNetwork(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogErrorNetwork(), msg, records...)
}

func ErrorExternal(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogErrorExternal(), msg, records...)
}

func ErrorInternal(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 0, EventTypeLogErrorInternal(), msg, records...)
}

type Finish func(records ...Record)

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var messageID = uuid.Must(uuid.NewV7())
	observe(ctx, 1, EventTypeMessageSent(), messageID.String())
	var cxx = newSpan(ctx)
	observe(cxx, 0, EventTypeSpanStart(), spanName, records...)
	observe(cxx, 0, EventTypeMessageReceived(), messageID.String())
	return cxx, func(records ...Record) {
		var messageID = uuid.Must(uuid.NewV7())
		observe(cxx, 0, EventTypeMessageSent(), messageID.String())
		observe(cxx, 0, EventTypeSpanFinish(), spanName, records...)
		observe(ctx, 1, EventTypeMessageReceived(), messageID.String())
	}
}
