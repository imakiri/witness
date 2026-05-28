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

	// ParentTraceID / ParentSpanID, when non-nil, pin the event's parent to an
	// externally-provided trace context — typically extracted from a W3C
	// traceparent header on an incoming request. OTel-aware observers use
	// these to thread cross-service trace continuity; others ignore them.
	ParentTraceID uuid.UUID
	ParentSpanID  uuid.UUID
}

type Observer interface {
	Observe(event Event)
}

type NilObserver struct{}

func (n NilObserver) Observe(event Event) {}
