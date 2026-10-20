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
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type logs struct {
	logger  log.Logger
	sampler sdktrace.Sampler // the trace provider's, so the sampled flag matches the span; nil: always sampled
}

func newLogs(lp *sdklog.LoggerProvider, sampler sdktrace.Sampler) *logs {
	return &logs{logger: lp.Logger(otlp.TracerName), sampler: sampler}
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

	l.logger.Emit(l.spanContext(event), r)
}

// spanContext gives the SDK the trace_id/span_id to stamp on the record. They
// are derived exactly as otlp derives them for the span itself — the full
// trace uuid and the last 8 bytes of the current span uuid — or the log would
// point at a span that does not exist.
//
// The sampled flag is the sampler's answer for that trace_id. The samplers
// NewObserver uses (AlwaysSample, TraceIDRatioBased) decide on the trace_id
// alone, so this is the decision the span itself got.
func (l *logs) spanContext(event witness.Event) context.Context {
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
	var flags trace.TraceFlags
	if l.sampler == nil || l.sampler.ShouldSample(sdktrace.SamplingParameters{ParentContext: ctx, TraceID: tid}).Decision == sdktrace.RecordAndSample {
		flags = trace.FlagsSampled
	}
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: flags})
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
