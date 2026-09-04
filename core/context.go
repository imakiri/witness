package core

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"slices"
	"testing"
	"time"
)

// Context carries the per-goroutine witness state: the observer that
// receives events, and the span chain saying where in the call graph those
// events originate.
//
// # The chain
//
// spanIDs is ordered root -> leaf. Its **last element is the current
// span** — the span events happen in — and everything before it is the
// history that led there: the parent, its ancestors, and at the front the
// root minted by Instance. Every role a span plays — own, parent,
// ancestor, instance — follows from its position and is derived per event
// at emit time, never stored: they change as the chain grows, and today's
// own span is tomorrow's parent.
//
// # The four things a witness call can do to it
//
//  1. Emit a point event in the current span: Info, Warn, Debug, Error and
//     kin, Observe, metrics. The chain is untouched.
//  2. Open a child span: Span, Service, Worker, SpanStart. Mints (or takes)
//     a span_id, appends it, and the returned Context has it as current.
//  3. Open a root: Instance, Test. Replaces the chain with a single fresh
//     span. Nothing sits above an instance, and these are the only
//     constructors — a Context cannot be built any other way.
//  4. Reference a foreign span without entering it: Link and the
//     message helpers. The span is appended to that one event *after* the
//     chain, flagged SpanFlagLink and nothing else; the Context is
//     unchanged.
//
// A process only ever emits events from spans it owns. It never enters a
// span another process opened: two processes writing start/finish into one
// span_id would make that span's reconstructed duration meaningless. The
// shared span_id is referenced instead, and a query on it still returns
// both sides — which is all a link ever had to do.
//
// # What it deliberately does not carry
//
// There is no trace_id and no service_name. Both were scalars standing in
// for things the space dimension already expresses: a "trace" is a
// connected component of the event<->span graph, not a column, and a
// service is identified by the instance span at the front of the chain —
// the one flagged SpanFlagInstance, whose span:instance:online event
// carries the name. A scalar trace_id could not survive a Context that
// legitimately belongs to two traces at once, and a scalar service_name
// duplicated what that event already said.
type Context struct {
	// t is the test that owns this Context, if any: witness.Test sets it and
	// every derived Context carries it, purely so entry points can call
	// t.Helper() and keep test failures pointing at the caller's line. It is
	// testing.TB rather than *testing.T so benchmarks and fuzz targets work.
	t        testing.TB
	observer Observer
	// spanIDs is the chain this Context owns, ordered root -> leaf. Every
	// span in it was minted by this process; a referenced foreign span
	// never enters it, so no per-span state needs storing — the roles are
	// entirely positional.
	spanIDs []uuid.UUID
}

// NewInstance builds the Context of a process: a chain of exactly one fresh
// span, which is therefore the instance root. Nothing sits above an
// instance, so this is the only way a chain begins; tb is the optional test
// that owns it.
//
// It emits nothing. The span:instance:online event that announces the
// process is the caller's job — see witness.Instance.
func NewInstance(tb testing.TB, observer Observer) Context {
	if observer == nil {
		observer = NilObserver{}
	}
	return Context{
		t:        tb,
		observer: observer,
		spanIDs:  []uuid.UUID{uuid.Must(uuid.NewV7())},
	}
}

// TB returns the test that owns this Context, or nil.
//
// It exists so that a frame between the test and the failure can mark
// *itself* as a helper:
//
//	if tb := c.TB(); tb != nil {
//		tb.Helper()
//	}
//
// That shape is not decoration. testing.TB.Helper marks the function that
// calls it, so a `func (c Context) Helper() { c.t.Helper() }` wrapper marked
// only itself and left every frame that called it — every witness entry
// point, Observe, ObserveLinked — looking like the origin of the event. A
// t.Logf from an observer was attributed to core/context.go rather than to
// the test's own line. The call has to be written in each frame that should
// be skipped; there is no way to mark a frame from inside a callee.
func (c Context) TB() testing.TB {
	return c.t
}

// WithChildSpan returns a Context whose chain has spanID appended as the new
// current span; everything else (observer, t) is inherited. Every span in a
// chain is one this process owns — to point at a span another process owns,
// reference it as a link with ObserveLinked.
//
// A span_id already in the chain is not appended twice: re-opening a span you
// are already inside is a no-op, and a duplicate would violate the unique
// (event_id, span_id) index in Postgres — which, because that observer
// batches, would discard every event queued alongside it.
func (c Context) WithChildSpan(spanID uuid.UUID) Context {
	if slices.Contains(c.spanIDs, spanID) {
		return c
	}
	return Context{
		t:        c.t,
		observer: c.observer,
		spanIDs:  append(slices.Clone(c.spanIDs), spanID),
	}
}

// Wants reports whether this Context's observer will do anything with an
// event of this type — see EventTypeFilter. Entry points call it before
// doing the work of building an event, in particular before Caller walks the
// stack, which is the expensive part.
func (c Context) Wants(eventType EventType) bool {
	return Accepts(c.observer, eventType)
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

// RootSpanID returns the first span_id in this Context's chain, which is
// the instance root: nothing sits above an instance. Kept as a synonym for
// InstanceSpanID, which says what it means.
func (c Context) RootSpanID() uuid.UUID {
	if len(c.spanIDs) == 0 {
		return uuid.Nil
	}
	return c.spanIDs[0]
}

// InstanceSpanID returns the span_id of the instance this Context belongs
// to: the chain's first span, minted by Instance or Test. It identifies the
// emitting process, and its span:instance:online event carries the
// instance's name and version. Returns uuid.Nil for a Context with no
// witness state.
func (c Context) InstanceSpanID() uuid.UUID {
	if len(c.spanIDs) == 0 {
		return uuid.Nil
	}
	return c.spanIDs[0]
}

// CurrentSpanID returns the deepest span_id in the chain — the span the
// caller is currently inside. Returns uuid.Nil for empty contexts.
func (c Context) CurrentSpanID() uuid.UUID {
	if len(c.spanIDs) == 0 {
		return uuid.Nil
	}
	return c.spanIDs[len(c.spanIDs)-1]
}

// Observe emits one event in this Context's current span.
//
// It takes no span roles: the durable ones (instance, link) already live on
// the chain, and the positional ones (own, parent, ancestor) follow from
// the chain's shape. A caller has nothing left to supply — the earlier
// spanFlags parameter existed only while "link" was a property of an event
// rather than of a span, which stopped being true once a borrowed span
// could become the current one.
//
// eventID and eventDate are generated here rather than accepted: the id is
// a uuid v7 so events sort by creation, and the date is the observer's
// wall clock at the moment of the call. Neither has ever been passed
// anything else by any caller in this repo, and letting them be supplied
// invited two events to claim the same identity or an event to claim a
// time its own process never saw.
func (c Context) Observe(eventType EventType, eventName string, eventCaller string, records ...Record) {
	if c.t != nil {
		c.t.Helper()
	}
	c.ObserveLinked(nil, eventType, eventName, eventCaller, records...)
}

// ObserveLinked is Observe with foreign span_ids referenced by this one event.
// They are appended after the chain and flagged SpanFlagLink only — a link
// is not a scope the process is inside, so it gets no positional role. A
// link already present in the chain is dropped: a duplicate span_id in one
// event violates the unique (event_id, span_id) index in Postgres, which,
// because the observer batches, would discard every event queued with it.
func (c Context) ObserveLinked(links []uuid.UUID, eventType EventType, eventName string, eventCaller string, records ...Record) {
	if !c.Wants(eventType) {
		return
	}
	if c.t != nil {
		c.t.Helper()
	}
	var spanIDs = c.spanIDs
	if len(links) > 0 {
		spanIDs = slices.Clone(c.spanIDs)
		for _, l := range links {
			if !slices.Contains(spanIDs, l) {
				spanIDs = append(spanIDs, l)
			}
		}
	}
	c.observer.Observe(Event{
		SpanIDs:      spanIDs,
		SpanFlags:    c.eventSpanFlags(len(spanIDs) - len(c.spanIDs)),
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    eventType,
		EventMessage: eventName,
		EventCaller:  eventCaller,
		Records:      records,
	})
}

type Finish func(records ...Record)

// keyContext is a unique type to prevent assignment: nothing outside this
// package can construct the key, so nothing outside this package can
// overwrite the witness Context on a ctx. It used to be a string constant —
// reproducible by anyone who read it, and a foreign value stored under it
// would fail the type assertion in From and silently downgrade the subtree
// to NilObserver.
type keyContext struct{}

func With(ctx context.Context, c Context) context.Context {
	return context.WithValue(ctx, keyContext{}, c)
}

func (c Context) To(ctx context.Context) context.Context {
	return With(ctx, c)
}

func From(ctx context.Context) Context {
	cs, ok := ctx.Value(keyContext{}).(Context)
	if ok {
		return cs
	}
	return Context{observer: NilObserver{}}
}
