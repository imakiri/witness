package dynatrace

import (
	"context"
	"strings"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/otlp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
)

type logs struct {
	logger log.Logger
}

func newLogs(lp *sdklog.LoggerProvider) *logs {
	return &logs{logger: lp.Logger(otlp.TracerName)}
}

func (l *logs) observe(event witness.Event) {
	var r log.Record
	r.SetTimestamp(event.EventDate)
	r.SetObservedTimestamp(event.EventDate)
	sev, text := severity(event.EventType)
	r.SetSeverity(sev)
	r.SetSeverityText(text)
	r.SetBody(attribute.StringValue(event.EventMessage))

	attrs := make([]attribute.KeyValue, 0, len(event.Records)+3)
	for _, rec := range event.Records {
		attrs = append(attrs, attribute.String(string(rec.AppendKey(nil)), string(rec.AppendValue(nil))))
	}
	attrs = append(attrs, attribute.String("witness.event_type", event.EventType.String()))
	if event.EventCaller != "" {
		attrs = append(attrs, attribute.String("witness.event_caller", event.EventCaller))
	}
	if event.EventID != uuid.Nil {
		attrs = append(attrs, attribute.String("witness.event_id", event.EventID.String()))
	}
	r.AddAttributes(attrs...)

	l.logger.Emit(spanContext(event), r)
}

// spanContext gives the SDK the trace_id/span_id to stamp on the record. They
// are derived exactly as otlp derives them for the span itself — the full
// trace uuid and the last 8 bytes of the current span uuid — or the log would
// point at a span that does not exist.
func spanContext(event witness.Event) context.Context {
	ctx := context.Background()
	if len(event.SpanIDs) == 0 {
		return ctx
	}
	traceID := event.TraceID
	if traceID == uuid.Nil {
		traceID = event.SpanIDs[0]
	}
	cur := event.SpanIDs[len(event.SpanIDs)-1]
	var tid trace.TraceID
	var sid trace.SpanID
	copy(tid[:], traceID[:])
	copy(sid[:], cur[8:])
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	if !sc.IsValid() {
		return ctx
	}
	return trace.ContextWithSpanContext(ctx, sc)
}

func severity(t witness.EventType) (log.Severity, string) {
	name := t.String()
	switch {
	case name == witness.EventTypeLogFatal().String():
		return log.SeverityFatal, "FATAL"
	case t.IsError():
		return log.SeverityError, "ERROR"
	case strings.HasPrefix(name, "log:warn"):
		return log.SeverityWarn, "WARN"
	case strings.HasPrefix(name, "log:debug"):
		return log.SeverityDebug, "DEBUG"
	default:
		return log.SeverityInfo, "INFO"
	}
}
