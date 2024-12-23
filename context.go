package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type ContextSpan struct {
	spanName string
	spanID   uuid.UUID
}

type ContextSpans []ContextSpan

func (cs ContextSpans) FindIDs(spanName ...string) []uuid.UUID {
	var spanIDs = make([]uuid.UUID, len(spanName))
	for i := range spanName {
		for j := range cs {
			if cs[j].spanName == spanName[i] {
				spanIDs = append(spanIDs, cs[j].spanID)
			}
		}
	}
	return spanIDs
}

type Context struct {
	observer Observer
	spans    ContextSpans
}

func NewContextSpan(spanName string) ContextSpan {
	return ContextSpan{
		spanName: spanName,
		spanID:   uuid.Must(uuid.NewV7()),
	}
}

func (c Context) observe(ctx context.Context, skip, extra int, eventType EventType, eventName string, records ...Record) {
	// TODO fix caller info for top level functions
	var eventCallerName, eventCallerPath = caller(skip+1, extra)
	//eventName = trim(eventName, MaxLengthEventName)
	//eventValue = eventValue[:min(len(eventValue), MaxLengthEventValue)]
	if debug {
		//eventCallerPath = trim(eventCallerPath, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.SpanIDs(), eventType, eventName, eventCallerPath, records...)
	} else {
		//eventCallerName = trim(eventCallerName, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.SpanIDs(), eventType, eventName, eventCallerName, records...)
	}
}

type Finish func(records ...Record)

func (c Context) span(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	var childSpan = NewContextSpan(contextName)
	c = c.Append(childSpan)
	c.observe(ctx, 2, 0, EventTypeSpanStart(), spanName, records...)
	return With(ctx, c), func(records ...Record) {
		childSpan.Append(c).observe(ctx, 1, 0, EventTypeSpanFinish(), spanName, records...)
	}
}

func (c Context) serviceSpan(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	var childContext = NewContextSpan(spanName)
	c.Append(childContext).observe(ctx, 2, 0, EventTypeServiceBegin(), spanName, records...)
	return With(ctx, childContext), func(records ...Record) {
		childContext.Append(c).observe(ctx, 1, 0, EventTypeServiceEnd(), spanName, records...)
	}
}

func (c Context) IsNil() bool {
	return c.observer == nil || c.spans == nil
}

func (c Context) Observe(ctx context.Context, eventType EventType, eventName string, records ...Record) {
	c.observe(ctx, 1, 0, eventType, eventName, records...)
}

func (c Context) Span(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	return c.span(ctx, contextName, spanName, records...)
}

// Append appends child context spans to current context and returns new context
func (c Context) Append(cs ...ContextSpan) Context {
	return Context{
		observer: c.observer,
		spans:    append(c.spans, cs...),
	}
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) Spans() ContextSpans {
	return c.spans
}

func (c Context) SpanIDs() []uuid.UUID {
	var spanIDs = make([]uuid.UUID, len(c.spans))
	for i := range c.spans {
		spanIDs = append(spanIDs, c.spans[i].spanID)
	}
	return spanIDs
}

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, keyContext, c)
}

func From(ctx context.Context) Context {
	cs, ok := ctx.Value(keyContext).(Context)
	if ok {
		return cs
	}
	return Context{observer: NilObserver{}}
}
