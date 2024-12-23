package witness

import (
	"context"
	"github.com/gofrs/uuid/v5"
)

type Context struct {
	contextName string
	observer    Observer
	spanIDs     []uuid.UUID
}

func NewContext(c Context, contextName string, spanIDs ...uuid.UUID) Context {
	return Context{
		contextName: contextName,
		observer:    c.observer,
		spanIDs:     spanIDs,
	}
}

func (c Context) observe(ctx context.Context, skip, extra int, eventType EventType, eventName string, records ...Record) {
	// TODO fix caller info for top level functions
	var eventCallerName, eventCallerPath = caller(skip+1, extra)
	//eventName = trim(eventName, MaxLengthEventName)
	//eventValue = eventValue[:min(len(eventValue), MaxLengthEventValue)]
	if debug {
		//eventCallerPath = trim(eventCallerPath, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanIDs, eventType, eventName, eventCallerPath, records...)
	} else {
		//eventCallerName = trim(eventCallerName, MaxLengthEventCaller)
		c.observer.Observe(ctx, c.spanIDs, eventType, eventName, eventCallerName, records...)
	}
}

type Finish func(records ...Record)

func (c Context) span(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	var childContext = NewContext(c, contextName, uuid.Must(uuid.NewV7()))
	c.Append(childContext).observe(ctx, 2, 0, EventTypeSpanStart(), spanName, records...)
	return With(ctx, childContext), func(records ...Record) {
		childContext.Append(c).observe(ctx, 1, 0, EventTypeSpanFinish(), spanName, records...)
	}
}

func (c Context) serviceSpan(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	var childContext = NewContext(c, contextName, uuid.Must(uuid.NewV7()))
	c.Append(childContext).observe(ctx, 2, 0, EventTypeSpanServiceBegin(), spanName, records...)
	return With(ctx, childContext), func(records ...Record) {
		childContext.Append(c).observe(ctx, 1, 0, EventTypeSpanServiceEnd(), spanName, records...)
	}
}

func (c Context) IsNil() bool {
	return c.contextName == ""
}

func (c Context) Observe(ctx context.Context, eventType EventType, eventName string, records ...Record) {
	c.observe(ctx, 1, 0, eventType, eventName, records...)
}

func (c Context) Span(ctx context.Context, contextName, spanName string, records ...Record) (context.Context, Finish) {
	return c.span(ctx, contextName, spanName, records...)
}

// Append appends child context to current context and returns new context
func (c Context) Append(cc Context) Context {
	return Context{
		observer: c.observer,
		spanIDs:  append(c.spanIDs, cc.spanIDs...),
	}
}

// Reverse reverses span list, making a parent a child, and a child a parent, and returns new context
func (c Context) Reverse() Context {
	var cx = c
	cx.spanIDs = make([]uuid.UUID, len(c.spanIDs))
	// slices.Reverse() with copy
	for i, j := 0, len(cx.spanIDs)-1; i < j; i, j = i+1, j-1 {
		cx.spanIDs[i], cx.spanIDs[j] = c.spanIDs[j], c.spanIDs[i]
	}
	return cx
}

func (c Context) Observer() Observer {
	return c.observer
}

func (c Context) SpanIDs() []uuid.UUID {
	return c.spanIDs
}

type Contexts []Context

func (cs Contexts) Context(contextName string) Context {
	for i := range cs {
		if cs[i].contextName == contextName {
			return cs[i]
		}
	}
	return Context{observer: NilObserver{}}
}

const keyContext = "witness.context:3D3DNvuPg4yxitoS0wG8Q0FpI0AeY9BQ"

func With(ctx context.Context, c ...Context) context.Context {
	return context.WithValue(ctx, keyContext, Contexts(c))
}

func From(ctx context.Context) Contexts {
	cs, ok := ctx.Value(keyContext).(Contexts)
	if ok && cs != nil {
		return cs
	}
	return Contexts{{observer: NilObserver{}}}
}
