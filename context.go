package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

const keyTrace = "witness.context:2y5XTtYWapY7C8ddyB3UaceEERsqYOb8"

func Extract(ctx context.Context) uuid.UUID {
	var traceID, ok = ctx.Value(keyTrace).(uuid.UUID)
	if ok {
		return traceID
	}
	return uuid.Nil
}

func Inject(ctx context.Context, traceID uuid.UUID) context.Context {
	return context.WithValue(ctx, keyTrace, traceID)
}
