package tee

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
)

type Observer struct {
	observers []witness.Observer
}

func NewObserver(observers ...witness.Observer) Observer {
	return Observer{observers: observers}
}

func (o Observer) Observe(ctx context.Context, spanIDs []uuid.UUID, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {
	for _, observer := range o.observers {
		observer.Observe(ctx, spanIDs, eventType, eventName, eventCaller, records...)
	}
}
