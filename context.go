package witness

import (
	"bytes"
	"context"
	"github.com/gofrs/uuid/v5"
	"slices"
	"time"
)

type Context struct {
	observer Observer
	spanIDs  []uuid.UUID
}

func (c Context) IsNil() bool {
	return c.observer == nil || c.spanIDs == nil
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanIDs() []uuid.UUID {
	return c.spanIDs
}

func NewContext(observer Observer) Context {
	return Context{
		observer: observer,
		spanIDs:  []uuid.UUID{uuid.Must(uuid.NewV7())},
	}
}

func (c Context) Join(cts ...Context) Context {
	var spanIDs = make([]uuid.UUID, len(c.spanIDs), len(c.spanIDs)+len(cts))
	copy(spanIDs, c.spanIDs)
	for _, ctx := range cts {
		spanIDs = append(spanIDs, ctx.SpanIDs()...)
	}
	slices.SortFunc(spanIDs, func(a, b uuid.UUID) int {
		return bytes.Compare(a[:], b[:])
	})
	return Context{
		observer: c.observer,
		spanIDs:  slices.Clone(slices.Compact(spanIDs)),
	}
}

func (c Context) Observe(eventID uuid.UUID, eventDate time.Time, eventType EventType, eventName string, eventCaller string, records ...Record) {
	if c.observer != nil {
		c.observer.Observe(c.spanIDs, eventID, eventDate, eventType, eventName, eventCaller, records...)
	}
}

func (c Context) Info(msg string, records ...Record) {
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogInfo(), msg, caller(1, 0), records...)
}

func (c Context) Warn(msg string, records ...Record) {
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogWarn(), msg, caller(1, 0), records...)
}

func (c Context) Debug(msg string, records ...Record) {
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogDebug(), msg, caller(1, 0), records...)
}

func (c Context) Error(msg string, err error, records ...Record) {
	c.Observe(uuid.Must(uuid.NewV7()), time.Now(), EventTypeLogError(), msg, caller(1, 0), appendError(records, err)...)
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

func Join(ctx context.Context, cts ...context.Context) context.Context {
	var contexts = make([]Context, len(cts))
	for i := range cts {
		contexts[i] = From(cts[i])
	}
	return From(ctx).Join(contexts...).To(ctx)
}
