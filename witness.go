package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Finish func()

type Record interface {
	Name() string
	String() string
}

type Observer interface {
	Observe(ctx context.Context, traceID uuid.UUID, eventType EventType, eventName string, records ...Record)
}

func Observe(ctx context.Context, observer Observer, eventType EventType, eventName string, records ...Record) {

}

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, traceID uuid.UUID, eventType EventType, eventName string, records ...Record) {
}

const keyObserver = "witness.observer:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, logger Observer) context.Context {
	return context.WithValue(ctx, keyObserver, logger)
}

func From(ctx context.Context) Observer {
	logger, ok := ctx.Value(keyObserver).(Observer)
	if ok {
		return logger
	} else {
		return NilObserver{}
	}
}

func Log(ctx context.Context, name string, t EventType, records ...Record) {
	From(ctx).Observe(ctx, Extract(ctx), t, name, records...)
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
	var traceID = Extract(ctx)
	if traceID == uuid.Nil {
		return ctx, func() {}
	}
	observer.Observe(ctx, traceID, EventTypeSpanStart(), name, records...)
	return ctx, func() {
		observer.Observe(ctx, traceID, EventTypeSpanFinish(), name)
	}
}
