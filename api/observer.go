package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Observer interface {
	Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventCaller string, records ...Record)
}

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventCaller string, records ...Record) {
}
