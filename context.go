package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Context struct {
	debug    bool
	observer Observer
	spanIDs  []uuid.UUID
}

func NewContext(c Context, spanIDs ...uuid.UUID) Context {
	return Context{
		debug:    c.debug,
		observer: c.observer,
		spanIDs:  spanIDs,
	}
}

func (c Context) observe(ctx context.Context, skip, extra int, eventType EventType, eventName string, records ...Record) {
	var eventCallerName, eventCallerPath = caller(skip+1, extra)
	//eventName = trim(eventName, MaxLengthEventName)
	//eventValue = eventValue[:min(len(eventValue), MaxLengthEventValue)]
	if c.debug {
		//eventCallerPath = trim(eventCallerPath, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanIDs, eventType, eventName, eventCallerPath, records...)
	} else {
		//eventCallerName = trim(eventCallerName, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanIDs, eventType, eventName, eventCallerName, records...)
	}
}

func (c Context) Append(spanID uuid.UUID) Context {
	return Context{
		debug:    c.debug,
		observer: c.observer,
		spanIDs:  append(c.spanIDs, spanID),
	}
}

func (c Context) Debug() bool {
	return c.debug
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanIDs() []uuid.UUID {
	return c.spanIDs
}

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, keyContext, c)
}

func From(ctx context.Context) Context {
	c, ok := ctx.Value(keyContext).(Context)
	if ok {
		return c
	} else {
		return Context{observer: NilObserver{}}
	}
}
