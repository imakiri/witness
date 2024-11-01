package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Context struct {
	Debug    bool
	Version  string
	Observer Observer
	spanID   uuid.UUID
}

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func newSpan(ctx context.Context) context.Context {
	var c = From(ctx)
	c.spanID = uuid.Must(uuid.NewV7())
	return With(ctx, c)
}

func With(ctx context.Context, cxx Context) context.Context {
	return context.WithValue(ctx, keyContext, cxx)
}

func From(ctx context.Context) Context {
	observer, ok := ctx.Value(keyContext).(Context)
	if ok {
		return observer
	} else {
		return Context{Observer: NilObserver{}}
	}
}
