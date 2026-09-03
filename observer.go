package witness

import (
	"github.com/gofrs/uuid/v5"
	"time"
)

type Event struct {
	SpanIDs      []uuid.UUID
	EventID      uuid.UUID
	EventDate    time.Time
	EventType    EventType
	EventMessage string
	EventCaller  string
	Records      []Record

	// TraceID is the logical request identifier — the same value across
	// every span produced while handling one request, even across services.
	// Set by witness.Instance to the local root span_id, or by
	// witness.InstanceContinue to the caller's TraceID it received over the
	// wire. Inherited unchanged by witness.Span. Observers that group spans
	// (postgres, otlp, stdlog) key on this to reconstruct a trace.
	TraceID uuid.UUID

	// ParentTraceID / ParentSpanID, when non-nil, pin the event's parent to an
	// externally-provided trace context — typically extracted from a W3C
	// traceparent header on an incoming request. Set only on the
	// span:instance:online event of an InstanceContinue. OTel-aware
	// observers use these to thread cross-service trace continuity;
	// others may ignore them.
	ParentTraceID uuid.UUID
	ParentSpanID  uuid.UUID

	// ServiceName is the local instance's name — the value passed as
	// instanceName to Instance / InstanceContinue. Every event emitted by
	// the same Context carries the same ServiceName; cross-service hops
	// re-set it on the receiver via InstanceContinue. Empty when an event
	// was emitted from a Context that never went through an Instance
	// constructor.
	ServiceName string
}

type Observer interface {
	Observe(event Event)
}

type NilObserver struct{}

func (n NilObserver) Observe(_ Event) {}
