package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Context struct {
	debug    bool
	observer Observer
	spanID   uuid.UUID
}

func NewContext(debug bool, observer Observer, spanID uuid.UUID) Context {
	return Context{
		debug:    debug,
		observer: observer,
		spanID:   spanID,
	}
}

func (c Context) Debug() bool {
	return c.debug
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanID() uuid.UUID {
	return c.spanID
}

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func newSpan(ctx context.Context) (context.Context, uuid.UUID) {
	var c = From(ctx)
	c.spanID = uuid.Must(uuid.NewV7())
	return With(ctx, c), c.spanID
}

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
