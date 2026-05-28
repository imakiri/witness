package otlp

import (
	"sync"

	"github.com/gofrs/uuid/v5"
	"go.opentelemetry.io/otel/trace"
)

func traceIDFromUUID(id uuid.UUID) trace.TraceID {
	var t trace.TraceID
	copy(t[:], id[:])
	return t
}

// spanIDFromUUID uses the last 8 bytes of the UUID — that's the random
// portion of UUID v7, so collisions are negligible.
func spanIDFromUUID(id uuid.UUID) trace.SpanID {
	var s trace.SpanID
	copy(s[:], id[8:])
	return s
}

type registry struct {
	m sync.Map // map[uuid.UUID]trace.Span
}

func (r *registry) Get(id uuid.UUID) (trace.Span, bool) {
	v, ok := r.m.Load(id)
	if !ok {
		return nil, false
	}
	return v.(trace.Span), true
}

func (r *registry) Set(id uuid.UUID, span trace.Span) {
	r.m.Store(id, span)
}

func (r *registry) Delete(id uuid.UUID) {
	r.m.Delete(id)
}

func (r *registry) Range(fn func(id uuid.UUID, span trace.Span) bool) {
	r.m.Range(func(k, v any) bool {
		return fn(k.(uuid.UUID), v.(trace.Span))
	})
}
