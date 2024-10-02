package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

const keySpan = "witness.span:2y5XTtYWapY7C8ddyB3UaceEERsqYOb8"

func Extract(ctx context.Context) uuid.UUID {
	var id, ok = ctx.Value(keySpan).(uuid.UUID)
	if ok {
		return id
	}
	return uuid.Nil
}

func Inject(ctx context.Context, spanID uuid.UUID) context.Context {
	return context.WithValue(ctx, keySpan, spanID)
}
