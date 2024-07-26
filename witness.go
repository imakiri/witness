package witness

import (
	"context"
)

type EventType string

const (
	EventTypeLogInfo  EventType = "log:info"
	EventTypeLogWarn  EventType = "log:warn"
	EventTypeLogDebug EventType = "log:debug"
)

type ErrorType string

const (
	ErrorTypeDisk     ErrorType = "log:error:disk"     // when system fails to write or read file on disk
	ErrorTypeNetwork  ErrorType = "log:error:network"  // when system fails to reach another system via network
	ErrorTypeExternal ErrorType = "log:error:external" // when system fails to validate ingoing request or response
	ErrorTypeInternal ErrorType = "log:error:internal" // when system fails due to internal error
)

type Record interface {
	Key() string
	String() string
}

type Logger interface {
	LogSpanChildOf(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func())
	LogSpanFollowsFrom(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func())
	LogEvent(ctx context.Context, t EventType, name string, records ...Record)
	LogError(ctx context.Context, t ErrorType, name string, records ...Record)
}

type NilLogger struct{}

func (NilLogger) LogSpanChildOf(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return ctx, func() {}
}
func (NilLogger) LogSpanFollowsFrom(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return ctx, func() {}
}
func (NilLogger) LogEvent(ctx context.Context, t EventType, name string, records ...Record) {}
func (NilLogger) LogError(ctx context.Context, t ErrorType, name string, records ...Record) {}

const keyLogger = "witness.logger:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func CtxWithLogger(ctx context.Context, logger Logger) context.Context {
	return context.WithValue(ctx, keyLogger, logger)
}

func CtxLogger(ctx context.Context) Logger {
	logger, ok := ctx.Value(keyLogger).(Logger)
	if ok {
		return logger
	} else {
		return NilLogger{}
	}
}

func Event(ctx context.Context, t EventType, name string, records ...Record) {
	CtxLogger(ctx).LogEvent(ctx, t, name, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	Event(ctx, EventTypeLogInfo, msg, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	Event(ctx, EventTypeLogWarn, msg, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	Event(ctx, EventTypeLogDebug, msg, records...)
}

func Error(ctx context.Context, t ErrorType, msg string, records ...Record) {
	CtxLogger(ctx).LogError(ctx, t, msg, records...)
}

func ErrorDisk(ctx context.Context, msg string, records ...Record) {
	Error(ctx, ErrorTypeDisk, msg, records...)
}

func ErrorNetwork(ctx context.Context, msg string, records ...Record) {
	Error(ctx, ErrorTypeNetwork, msg, records...)
}

func ErrorExternal(ctx context.Context, msg string, records ...Record) {
	Error(ctx, ErrorTypeExternal, msg, records...)
}

func ErrorInternal(ctx context.Context, msg string, records ...Record) {
	Error(ctx, ErrorTypeInternal, msg, records...)
}

func SpanChildOf(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return CtxLogger(ctx).LogSpanChildOf(ctx, name, records...)
}

func SpanFollowsFrom(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return CtxLogger(ctx).LogSpanFollowsFrom(ctx, name, records...)
}
