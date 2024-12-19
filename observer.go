package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

//const (
//	MaxLengthEventName   = 256
//	MaxLengthEventValue  = 256
//	MaxLengthEventCaller = 1024
//)

type Observer interface {
	// Observe spanIDs: sequence, order matter, you can think that [0] is a parent, and [1] is a child
	Observe(ctx context.Context, spanIDs []uuid.UUID, eventType EventType, eventName string, eventCaller string, records ...Record)
}

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, spanIDs []uuid.UUID, eventType EventType, eventName string, eventCaller string, records ...Record) {
}
