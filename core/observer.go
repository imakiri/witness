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

// EventTypeFilter is an optional interface an Observer may implement to say
// which event types it will do anything with. Accepts is consulted *before*
// an event is built — before the stack walk that finds the call site, before
// the uuid, before the records slice — so a type nobody accepts costs a
// type assertion and a comparison instead of a few hundred nanoseconds.
//
// That is what makes a metric in a hot loop affordable to leave in the code
// with the metric backend absent: witness.Count on a Context whose observer
// declines metric types does no work at all.
//
// It must be cheap, pure and stable: it runs on every event, and an Observer
// that starts accepting a type it used to decline gains nothing retroactive.
// An Observer that does not implement it accepts everything, which is the
// only safe default — a filter that guesses would drop data silently.
type EventTypeFilter interface {
	Accepts(eventType EventType) bool
}

// Accepts reports whether observer will do anything with an event of this
// type. It is the check every entry point makes before building one.
func Accepts(observer Observer, eventType EventType) bool {
	if observer == nil {
		return false
	}
	if f, ok := observer.(EventTypeFilter); ok {
		return f.Accepts(eventType)
	}
	return true
}

type NilObserver struct{}

func (n NilObserver) Observe(_ Event) {}

// Accepts is false for everything: the nil observer does nothing with any
// event, so nothing needs to be built for it.
func (n NilObserver) Accepts(_ EventType) bool { return false }
