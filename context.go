package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Context struct {
	observer Observer
	spanID   uuid.UUID
}

func (c Context) IsNil() bool {
	return c.observer == nil || c.spanID == uuid.Nil
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanID() uuid.UUID {
	return c.spanID
}

func (c Context) WithSpanID(spanID uuid.UUID) Context {
	c.spanID = spanID
	return c
}

func (c Context) Observe(ctx context.Context, eventType EventType, eventName string, eventCaller string, records ...Record) {
	c.observer.Observe(ctx, []uuid.UUID{c.spanID}, eventType, eventName, eventCaller, records...)
}

type Finish func(records ...Record)

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, keyContext, c)
}

func (c Context) To(ctx context.Context) context.Context {
	return With(ctx, c)
}

func From(ctx context.Context) Context {
	cs, ok := ctx.Value(keyContext).(Context)
	if ok {
		return cs
	}
	return Context{observer: NilObserver{}}
}
