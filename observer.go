package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventValue string, records ...Record) {
}

const keyObserver = "witness.observer:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, observer Observer) context.Context {
	return context.WithValue(ctx, keyObserver, observer)
}

func From(ctx context.Context) Observer {
	observer, ok := ctx.Value(keyObserver).(Observer)
	if ok {
		return observer
	} else {
		return NilObserver{}
	}
}
