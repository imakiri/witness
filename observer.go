package witness

import (
	"github.com/gofrs/uuid/v5"
	"time"
)

//const (
//	MaxLengthEventName   = 256
//	MaxLengthEventValue  = 256
//	MaxLengthEventCaller = 1024
//)

type Observer interface {
	Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType EventType, eventName string, eventCaller string, records ...Record)
}

type NilObserver struct{}

func (n NilObserver) Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType EventType, eventName string, eventCaller string, records ...Record) {
}
