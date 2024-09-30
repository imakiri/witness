package witness

import (
	"context"
)

type EventTypeSpan string

const (
	EventTypeSpanStart  EventTypeSpan = "span:start"
	EventTypeSpanFinish EventTypeSpan = "span:finish"
)

type EventTypeLog string

const (
	EventTypeLogInfo          EventTypeLog = "log:info"
	EventTypeLogWarn          EventTypeLog = "log:warn"
	EventTypeLogDebug         EventTypeLog = "log:debug"
	EventTypeLogErrorStorage  EventTypeLog = "log:error:storage"  // when system fails to write or read file on disk or other persistent storage
	EventTypeLogErrorNetwork  EventTypeLog = "log:error:network"  // when system fails to reach another system via network
	EventTypeLogErrorExternal EventTypeLog = "log:error:external" // when system fails due to failure of an external system e.g. invalid ingoing request or response
	EventTypeLogErrorInternal EventTypeLog = "log:error:internal" // when system fails due to internal error
)

type Finish func()

type Record interface {
	Name() string
	String() string
}

type Observer interface {
	ObserveSpan(ctx context.Context, name string, new bool) (context.Context, Finish)
	ObserveLog(ctx context.Context, name string, t EventTypeLog, records ...Record)
}

type NilLogger struct{}

func (n NilLogger) ObserveSpan(ctx context.Context, name string, new bool) (context.Context, Finish) {
	return ctx, func() {}
}

func (n NilLogger) ObserveLog(ctx context.Context, name string, t EventTypeLog, records ...Record) {}

const keyLogger = "witness.logger:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, logger Observer) context.Context {
	return context.WithValue(ctx, keyLogger, logger)
}

func From(ctx context.Context) Observer {
	logger, ok := ctx.Value(keyLogger).(Observer)
	if ok {
		return logger
	} else {
		return NilLogger{}
	}
}

func Log(ctx context.Context, name string, t EventTypeLog, records ...Record) {
	From(ctx).ObserveLog(ctx, name, t, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogInfo, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogWarn, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogDebug, records...)
}

func ErrorStorage(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorStorage, records...)
}

func ErrorNetwork(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorNetwork, records...)
}

func ErrorExternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorExternal, records...)
}

func ErrorInternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorInternal, records...)
}

func SpanChildOf(ctx context.Context, name string, records ...Record) (context.Context, Finish) {
	var observer = From(ctx)
	ctx, finish := observer.ObserveSpan(ctx, name, false)
	observer.ObserveLog(ctx, name, EventTypeLogInfo, records...)
	return ctx, finish
}

func SpanFollowsFrom(ctx context.Context, name string, records ...Record) (context.Context, Finish) {
	var observer = From(ctx)
	ctx, finish := observer.ObserveSpan(ctx, name, true)
	observer.ObserveLog(ctx, name, EventTypeLogInfo, records...)
	return ctx, finish
}
