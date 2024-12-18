package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"strconv"
	"time"
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

func observe(ctx context.Context, skip, extra int, eventType EventType, eventName string, eventValue string, records ...Record) {
	var c = From(ctx)
	var eventCallerName, eventCallerPath = caller(skip+1, extra)
	//eventName = trim(eventName, MaxLengthEventName)
	//eventValue = eventValue[:min(len(eventValue), MaxLengthEventValue)]
	if c.debug {
		//eventCallerPath = trim(eventCallerPath, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanID, eventType, eventName, eventValue, eventCallerPath, records...)
	} else {
		//eventCallerName = trim(eventCallerName, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanID, eventType, eventName, eventValue, eventCallerName, records...)
	}
}

func err2string(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func Observe(ctx context.Context, eventType EventType, eventName string, eventValue string, records ...Record) {
	observe(ctx, 1, 0, eventType, eventName, eventValue, records...)
}

func ObserveString(ctx context.Context, eventType EventType, eventName string, eventValue string, records ...Record) {
	observe(ctx, 1, 0, eventType, eventName, eventValue, records...)
}

func ObserveInteger(ctx context.Context, eventType EventType, eventName string, eventValue int64, records ...Record) {
	observe(ctx, 1, 0, eventType, eventName, string(strconv.AppendInt(nil, eventValue, 10)), records...)
}

func ObserveTime(ctx context.Context, eventType EventType, eventName string, eventValue time.Time, records ...Record) {
	observe(ctx, 1, 0, eventType, eventName, string(strconv.AppendInt(nil, eventValue.UnixNano(), 10)), records...)
}

func Link(ctx, cxx context.Context, msg string, records ...Record) {
	var linkID = uuid.Must(uuid.NewV7())
	observe(ctx, 1, 0, EventTypeLink(), msg, linkID.String(), records...)
	observe(cxx, 1, 0, EventTypeLink(), msg, linkID.String(), records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogInfo(), msg, "", records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogWarn(), msg, "", records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogDebug(), msg, "", records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogError(), msg, err2string(err), records...)
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogErrorStorage(), msg, err2string(err), records...)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogErrorNetwork(), msg, err2string(err), records...)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogErrorExternal(), msg, err2string(err), records...)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	observe(ctx, 1, 0, EventTypeLogErrorInternal(), msg, err2string(err), records...)
}

type Finish func(records ...Record)

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var messageID = uuid.Must(uuid.NewV7())
	observe(ctx, 2, 1, EventTypeMessageSent(), spanName, messageID.String())
	var cxx = With(ctx, NewContext(ctx, uuid.Must(uuid.NewV7())))
	observe(cxx, 2, 0, EventTypeSpanStart(), spanName, "", records...)
	observe(cxx, 2, 0, EventTypeMessageReceived(), spanName, messageID.String())
	return cxx, func(records ...Record) {
		var messageID = uuid.Must(uuid.NewV7())
		observe(cxx, 1, 0, EventTypeMessageSent(), spanName, messageID.String())
		observe(cxx, 1, 0, EventTypeSpanFinish(), spanName, "", records...)
		observe(ctx, 1, 1, EventTypeMessageReceived(), spanName, messageID.String())
	}
}

// ServiceBegin creates standalone span and links it to existing one
func ServiceBegin(ctx context.Context, serviceName string, records ...Record) Context {
	var c = From(ctx)
	c.spanID = uuid.Must(uuid.NewV7())
	var cxx = With(ctx, c)
	observe(cxx, 2, 0, EventTypeServiceBegin(), serviceName, "", records...)
	var linkID = uuid.Must(uuid.NewV7())
	observe(ctx, 1, 0, EventTypeLink(), "", linkID.String(), records...)
	observe(cxx, 1, 0, EventTypeLink(), "", linkID.String(), records...)
	return c
}

func ServiceEnd(ctx context.Context, c Context, records ...Record) {
	observe(With(ctx, c), 2, 0, EventTypeServiceEnd(), "", "", records...)
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
		spanID:   spanID,
	}
	var cxx = With(ctx, c)
	observe(cxx, 2, 0, EventTypeInstanceOnline(), instanceName, instanceVersion, records...)
	return cxx, func(records ...Record) {
		observe(cxx, 1, 0, EventTypeInstanceOffline(), instanceName, instanceVersion, records...)
	}
}
