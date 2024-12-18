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
	Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventValue []byte, eventCaller string, records ...Record)
}

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventValue []byte, eventCaller string, records ...Record) {
}
