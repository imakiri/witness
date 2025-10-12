package tee

import (
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"time"
)

type Observer struct {
	observers []witness.Observer
}

func NewObserver(observers ...witness.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {
	for _, observer := range o.observers {
		observer.Observe(spanIDs, eventID, eventDate, eventType, eventName, eventCaller, records...)
	}
}
