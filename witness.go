package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Record interface {
	Key() string
	String() string
}

type Option interface {
	apply(ctx context.Context) context.Context
}

type option func(ctx context.Context) context.Context

func (o option) apply(ctx context.Context) context.Context {
	return o(ctx)
}

const KeyRecordsID = "3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func WithRecords(records ...Record) Option {
	return option(func(ctx context.Context) context.Context {
		return context.WithValue(ctx, KeyRecordsID, records)
	})
}

func Records(ctx context.Context) []Record {
	var records = ctx.Value(KeyRecordsID)
	if records == nil {
		return nil
	}
	return records.([]Record)
}

const keyTraceID = "6DHdp5AbRyGH9y9MIIpRZOAtSsd93gsD"

func New(ctx context.Context, traceID uuid.UUID, options ...Option) context.Context {
	ctx = context.WithValue(ctx, keyTraceID, traceID)
	for _, option := range options {
		ctx = option.apply(ctx)
	}
	return ctx
}

func TraceID(ctx context.Context) uuid.UUID {
	return ctx.Value(keyTraceID).(uuid.UUID)
}
