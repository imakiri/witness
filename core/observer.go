package core

import (
	"github.com/gofrs/uuid/v5"
	"time"
)

type Event struct {
	// SpanIDs is the emitter's span chain, ordered root -> leaf, followed by
	// any spans this event merely references (links). It is therefore NOT
	// purely a chain: the last entry is the current span only when the event
	// carries no link. Select by SpanFlags — SpanFlagOwn, SpanFlagParent —
	// rather than by position.
	SpanIDs []uuid.UUID

	// SpanFlags is parallel to SpanIDs: SpanFlags[i] is the role SpanIDs[i]
	// plays in this event (own / parent / ancestor / instance / link). A nil
	// or short slice means "roles unknown" — not "no roles" — so consumers
	// must index-guard rather than assume the lengths match. Every Event
	// built by a witness Context carries a full slice; hand-built Events
	// (tests, custom producers) may not.
	SpanFlags []SpanFlags

	EventID      uuid.UUID
	EventDate    time.Time
	EventType    EventType
	EventMessage string
	EventCaller  string
	Records      []Record
}

type Observer interface {
	Observe(event Event)
}

type NilObserver struct{}

func (n NilObserver) Observe(_ Event) {}
