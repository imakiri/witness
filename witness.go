package witness

import (
	"context"
	"fmt"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness/record"
	"runtime"
)

type Record interface {
	Name() string
	String() string
}

type Observer interface {
	Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventValue string, records ...Record)
}

func Observe(ctx context.Context, observer Observer, eventType EventType, eventName string, records ...Record) {

}

func Log(ctx context.Context, eventName string, eventType EventType, eventValue string, records ...Record) {
	From(ctx).Observe(ctx, Extract(ctx), eventType, eventName, eventValue, records...)
}

func Info(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogInfo(), "", records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogWarn(), "", records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogDebug(), "", records...)
}

func Error(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogError(), "", records...)
}

func ErrorStorage(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorStorage(), "", records...)
}

func ErrorNetwork(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorNetwork(), "", records...)
}

func ErrorExternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorExternal(), "", records...)
}

func ErrorInternal(ctx context.Context, msg string, records ...Record) {
	Log(ctx, msg, EventTypeLogErrorInternal(), "", records...)
}

type Finish func(records ...Record)

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, Finish) {
	var observer = From(ctx)
	var newSpanID = uuid.Must(uuid.NewV7())
	var oldSpanID = Extract(ctx)
	var functionName string
	var functionRecords = make([]Record, 0, 3)
	var pc, _, _, ok = runtime.Caller(2)
	if ok {
		var details = runtime.FuncForPC(pc)
		if details != nil {
			functionName = details.Name()
			var atFile, atLine = details.FileLine(pc)
			functionRecords = append(functionRecords, record.String("at", fmt.Sprintf("%s:%d", atFile, atLine)))
		}
	}
	observer.Observe(ctx, oldSpanID, EventTypeFunctionCall(), spanName, functionName, functionRecords...)
	observer.Observe(ctx, newSpanID, EventTypeSpanStart(), spanName, oldSpanID.String(), records...)
	return Inject(ctx, newSpanID), func(records ...Record) {
		observer.Observe(ctx, newSpanID, EventTypeSpanFinish(), spanName, "", records...)
		observer.Observe(ctx, oldSpanID, EventTypeFunctionReturn(), spanName, functionName)
	}
}

func MessageSent(ctx context.Context, messageName string, messageID uuid.UUID, records ...Record) {
	From(ctx).Observe(ctx, Extract(ctx), EventTypeMessageSentInternal(), messageName, messageID.String(), records...)
}

func MessageReceived(ctx context.Context, messageName string, messageID uuid.UUID, records ...Record) {
	From(ctx).Observe(ctx, Extract(ctx), EventTypeMessageReceivedInternal(), messageName, messageID.String(), records...)
}
