package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type NilObserver struct{}

func (n NilObserver) Observe(ctx context.Context, spanID uuid.UUID, eventType EventType, eventName string, eventValue string, records ...Record) {
}

const keyObserver = "witness.observer:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, logger Observer) context.Context {
	return context.WithValue(ctx, keyObserver, logger)
}

func From(ctx context.Context) Observer {
	logger, ok := ctx.Value(keyObserver).(Observer)
	if ok {
		return logger
	} else {
		return NilObserver{}
	}
}
