package witness

import (
	"context"
)

type Finish func()

type Record interface {
	Name() string
	String() string
}

type Observer interface {
	ObserveSpan(ctx context.Context, name string) (context.Context, Finish)
	ObserveLog(ctx context.Context, name string, t EventType, records ...Record)
}

//type Observer2 interface {
//	Observe(ctx Context, eventType EventType, eventName string, records ...Record)
//}
//
//type Context struct {
//	TraceID    uuid.UUID
//	InstanceID uuid.UUID
//	SpanID     uuid.UUID
//}
//
//
//func Observe(ctx context.Context, observer Observer2, eventType EventType, eventName string, records ...Record) {
//
//}

type NilObserver struct{}

func (n NilObserver) ObserveSpan(ctx context.Context, name string) (context.Context, Finish) {
	return ctx, func() {}
}

func (n NilObserver) ObserveLog(ctx context.Context, name string, t EventType, records ...Record) {}

const keyLogger = "witness.logger:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, logger Observer) context.Context {
	return context.WithValue(ctx, keyLogger, logger)
}

func From(ctx context.Context) Observer {
	logger, ok := ctx.Value(keyLogger).(Observer)
	if ok {
		return logger
	} else {
		return NilObserver{}
	}
}

func Log(ctx context.Context, name string, t EventType, records ...Record) {
	From(ctx).ObserveLog(ctx, name, t, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogInfo(), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogWarn(), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogDebug(), records...)
}

func Error(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogError(), records...)
}

func ErrorStorage(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorStorage(), records...)
}

func ErrorNetwork(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorNetwork(), records...)
}

func ErrorExternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorExternal(), records...)
}

func ErrorInternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorInternal(), records...)
}

func Span(ctx context.Context, name string, records ...Record) (context.Context, Finish) {
	var observer = From(ctx)
	ctx, finish := observer.ObserveSpan(ctx, name)
	observer.ObserveLog(ctx, name, EventTypeLogInfo(), records...)
	return ctx, finish
}
