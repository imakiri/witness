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
}

type Observer interface {
	Observe(event Event)
}

type NilObserver struct{}

func (n NilObserver) Observe(event Event) {}
