package witness

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness/core"
)

// The three core types that appear in this package's own signatures are
// aliased so a call site needs one import, not two. They are aliases, not
// definitions: the type is core's, and so is the compatibility promise.
type (
	// Record is one key/value pair attached to an event.
	Record = core.Record
	// EventType classifies an event. Custom ones come from core.MustNewEventType.
	EventType = core.EventType
	// Finish closes a span opened by Span, Service, Worker, SpanStart,
	// Instance or Test.
	Finish = core.Finish
)

func appendError(records []core.Record, err error) []core.Record {
	if err == nil {
		return records
	}
	return append(records, record{
		key:   "err",
		value: err.Error(),
	})
}

// appendCause is appendError for a recover() value: any error keeps its
// Error(), anything else is formatted.
func appendCause(records []core.Record, cause any) []core.Record {
	if cause == nil {
		return records
	}
	if err, ok := cause.(error); ok {
		return appendError(records, err)
	}
	return append(records, record{key: "err", value: fmt.Sprint(cause)})
}

func Info(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeLogInfo()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogInfo(), msg, core.Caller(1), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeLogWarn()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogWarn(), msg, core.Caller(1), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeLogDebug()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogDebug(), msg, core.Caller(1), records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogError(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorRF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogError(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorOrInfo(ctx context.Context, okMsg, errMsg string, err error, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	if err != nil {
		c.Observe(core.EventTypeLogError(), errMsg, core.Caller(1), appendError(records, err)...)
	} else {
		c.Observe(core.EventTypeLogInfo(), okMsg, core.Caller(1), records...)
	}
}

// Fatal records a failure the process cannot continue past. It does not
// exit: witness never terminates a program on its own behalf, and a library
// that calls os.Exit takes that decision away from the caller. Emit this,
// then exit.
func Fatal(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogFatal(), msg, core.Caller(1), appendError(records, err)...)
}

// Panic records a panic that was caught. It does not panic — call it from a
// recover(), where cause is whatever recover() returned:
//
//	defer func() {
//		if r := recover(); r != nil {
//			witness.Panic(ctx, "handler panicked", r)
//			// re-panic, or don't — witness has no opinion
//		}
//	}()
//
// cause is an any because recover() returns one; an error keeps its Error(),
// anything else is formatted with %v.
func Panic(ctx context.Context, msg string, cause any, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeLogPanic(), msg, core.Caller(1), appendCause(records, cause)...)
}

// Count records a counter increment: metricName is the metric, delta is how
// much to add to it, and any further records are its labels.
//
// The delta rides in a "value" record, which is the contract every metric
// observer reads. One event may carry one increment or many — emitting
// delta=1 per event and delta=100 once are the same metric, and which one a
// program does is a volume decision, not a modelling one.
func Count(ctx context.Context, metricName string, delta float64, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeMetricCounter()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeMetricCounter(), metricName, core.Caller(1), prependValue(records, delta)...)
}

// Gauge records the current value of something that goes up and down — queue
// depth, open connections, temperature. Unlike Count's delta this is the
// absolute value at this moment; the last one observed wins.
func Gauge(ctx context.Context, metricName string, value float64, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeMetricGauge()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeMetricGauge(), metricName, core.Caller(1), prependValue(records, value)...)
}

// Sample records one observation of a distribution — a latency, a payload
// size, a batch length — which a backend turns into a histogram.
//
// It is one sample, not a summary: witness emits the value it was given and
// leaves bucketing, quantiles and rates to whatever reads the events. The
// name says what the event is (a sample drawn from a distribution) rather
// than what a particular backend calls the aggregate of them.
func Sample(ctx context.Context, metricName string, value float64, records ...Record) {
	var c = core.From(ctx)
	// Checked before Caller walks the stack: an event nobody wants
	// must not pay for finding out where it came from.
	if !c.Wants(core.EventTypeMetricHistogram()) {
		return
	}
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeMetricHistogram(), metricName, core.Caller(1), prependValue(records, value)...)
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var nc = core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	if tb := nc.TB(); tb != nil {
		tb.Helper()
	}
	nc.Observe(core.EventTypeSpanStart(), spanName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if tb := nc.TB(); tb != nil {
			tb.Helper()
		}
		nc.Observe(core.EventTypeSpanFinish(), spanName, at, records...)
	}
}

// SpanStart is Span with the span_id supplied by the caller instead of
// minted here — for when the id has to exist before the span does, because
// it is going into a message envelope or a header. The span is this
// process's own, like any other: to point at a span another process owns,
// reference it with Link, do not open it.
func SpanStart(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var c = core.From(ctx).WithChildSpan(spanID)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeSpanStart(), spanName, at, records...)
	return c.To(ctx), func(records ...Record) {
		if tb := c.TB(); tb != nil {
			tb.Helper()
		}
		c.Observe(core.EventTypeSpanFinish(), spanName, at, records...)
	}
}

// SpanFinish closes a span by id, for the shape where start and finish are
// not lexically paired and the core.Finish closure cannot be carried between
// them. Prefer the core.Finish that SpanStart returned.
func SpanFinish(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = core.From(ctx).WithChildSpan(spanID)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.Observe(core.EventTypeSpanFinish(), spanName, core.Caller(1), records...)
}

// Link relates a span_id to this span without entering it: it emits a
// span:link event referencing linkID, and the core.Context is unchanged. The
// id comes from the caller — mint it with uuid.Must(uuid.NewV7()) when this
// span is the one introducing it, or pass one that arrived from elsewhere.
// Two spans emitting Link on one id are joined by it, and a single query on
// that id returns both.
//
// The link span is *referenced*, never entered: it is appended to this one
// event after the chain, flagged core.SpanFlagLink and nothing else. Nobody
// opens or closes it — a span two processes both opened has no meaningful
// duration.
//
// **A link counts as the sending side of a hand-off.** span:link is a
// positive event type, so witness.span_link_sides reads it as a send: emit
// Link on an id you are about to hand off — before the send, while Sent
// belongs after it — and the edge to whoever calls Received exists even if
// this process dies mid-send. The cost is a link with no receiving half if
// the send never happened; find those with
//
//	SELECT * FROM witness.span_link_sides WHERE sent_at IS NOT NULL AND received_at IS NULL
//
// The corollary: the *receiving* side of a message must use Received, not
// Link, or it will be read as a second sender.
func Link(ctx context.Context, linkID uuid.UUID, linkName string, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.ObserveLinked([]uuid.UUID{linkID}, core.EventTypeSpanLink(), linkName, core.Caller(1), records...)
}

// Sent records a message hand-off on the sender's side: msgID is the id the
// sender put in the envelope — an HTTP header, a queue row, a broker key —
// and whoever takes it out calls Received with the same id. A query on that
// id then returns both sides.
//
// The id is the caller's to mint (uuid.Must(uuid.NewV7())) because it has to
// exist before the send, while this event says the hand-off *happened*: emit
// it once the send succeeded. A send that failed handed nothing off — that is
// an error in this span, not a link nobody will ever answer. Retries reuse
// the id and record the attempt count rather than minting one id per attempt.
//
// The link span is *referenced*, never entered: it is appended to this one
// event after the chain, flagged core.SpanFlagLink and nothing else, and the
// caller's core.Context is unchanged.
func Sent(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanMessageSent(), msgName, core.Caller(1), records...)
}

// Received is the other half of Sent: the message arrived, on the msgID it
// carried. Emit it inside the span that handles the message, so that span is
// what a query on msgID finds on this side.
func Received(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanMessageReceived(), msgName, core.Caller(1), records...)
}

// ReceivedAll is Received for a batch: one event referencing every msgID the
// batch took in, in the span that handles them.
//
// A batch worker cannot enter the n spans that fed it — a chain has one
// current span and roles are positional, so n parents are not expressible,
// and the relation "this worker is processing that request" is a link, not
// parenthood. The event carries all n links instead: witness events have N
// space dimensions, and this is what they are for.
//
// Emit it once, in a span opened for the batch, and let ordinary events
// follow in that span — re-linking every one of them multiplies rows without
// making any query answerable that this event did not already answer. An
// event about a single item links only that item's msgID.
func ReceivedAll(ctx context.Context, msgIDs []uuid.UUID, msgName string, records ...Record) {
	var c = core.From(ctx)
	if tb := c.TB(); tb != nil {
		tb.Helper()
	}
	c.ObserveLinked(msgIDs, core.EventTypeSpanMessageReceived(), msgName, core.Caller(1), records...)
}

// Handle opens a child span for the work a message triggers, and records the
// message arriving in it: a span:general:start and a span:message:received
// carrying msgID as a link, both attributed to this line, and a
// span:general:finish from the returned core.Finish.
//
// It is Span plus Received in the right order and the right span, which is
// the mistake it exists to prevent: emitting the received half in the caller
// of the handler puts the message on a span that outlives it, and a
// long-lived one accumulates every message it ever dispatched.
//
// There is no reply half, here or anywhere. A hand-off is one direction: one
// side gives (Link before the send, Sent after it), the other takes (Received,
// or this). An answer travelling back is another hand-off, with its own id and
// the same two calls the other way round — and for a synchronous call it needs
// no event at all, since the round trip is the calling span's own duration.
func Handle(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var nc = core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	if tb := nc.TB(); tb != nil {
		tb.Helper()
	}
	nc.Observe(core.EventTypeSpanStart(), msgName, at)
	nc.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanMessageReceived(), msgName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if tb := nc.TB(); tb != nil {
			tb.Helper()
		}
		nc.Observe(core.EventTypeSpanFinish(), msgName, at, records...)
	}
}

// HandleAll is Handle for a batch: one span for the batch, one event
// referencing every msgID it took in.
//
// Opening a span is the point. The links belong to the batch, not to the
// worker that runs batch after batch: hung on a long-lived worker span they
// would drag every batch it ever ran into any one message's trace. The
// returned core.Context carries only the chain — links live in that one
// event, never on the context — so ordinary events that follow are events of
// the batch span and nothing else.
func HandleAll(ctx context.Context, msgIDs []uuid.UUID, msgName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var nc = core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	if tb := nc.TB(); tb != nil {
		tb.Helper()
	}
	nc.Observe(core.EventTypeSpanStart(), msgName, at)
	nc.ObserveLinked(msgIDs, core.EventTypeSpanMessageReceived(), msgName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		if tb := nc.TB(); tb != nil {
			tb.Helper()
		}
		nc.Observe(core.EventTypeSpanFinish(), msgName, at, records...)
	}
}

// Instance opens the root span of a process: a span representing this
// service instance's lifetime, announced by a span:instance:online event
// and closed by the returned core.Finish. Call it once, at process start.
//
// Nothing sits above an instance. ctx is used only as the parent
// context.Context — for cancellation and unrelated values — and any witness
// state already on it, test binding included, is discarded rather than
// inherited. A request arriving from another process does not open an
// instance: it opens a span under this one, entering the upstream span_id
// with SpanStart.
func Instance(ctx context.Context, observer core.Observer, instanceName string, instanceVersion string, records ...Record) (context.Context, core.Finish) {
	return instance(ctx, nil, observer, instanceName, instanceVersion, core.Caller(1), records...)
}

// Test is Instance for a test: it opens a root span the same way, and
// additionally binds tb to the core.Context so every witness entry point can
// call tb.Helper() and keep a failure pointing at the test's own line
// rather than at witness internals.
//
// The instance is named after the test (tb.Name()) and versioned "test" —
// a test process has no build version worth recording, and deriving the
// name means the events of two tests sharing an observer stay tellable
// apart.
//
// Pair it with observers/test, or with any observer whose output you want
// to assert on:
//
//	ctx, finish := witness.Test(context.Background(), t, observer)
//	defer finish()
func Test(ctx context.Context, tb testing.TB, observer core.Observer, records ...Record) (context.Context, core.Finish) {
	if tb != nil {
		tb.Helper()
	}
	return instance(ctx, tb, observer, tb.Name(), "test", core.Caller(1), records...)
}

// instance is Instance with the call site and the owning test passed in,
// so that a delegating caller — Test — still reports its own caller rather
// than the delegation itself.
func instance(ctx context.Context, tb testing.TB, observer core.Observer, instanceName, instanceVersion, at string, records ...Record) (context.Context, Finish) {
	if tb != nil {
		tb.Helper()
	}
	c := core.NewInstance(tb, observer)
	recordVersion := record{key: "version", value: instanceVersion}
	c.Observe(core.EventTypeSpanInstanceOnline(), instanceName, at, append(records, recordVersion)...)
	return core.With(ctx, c), func(records ...Record) {
		if tb := c.TB(); tb != nil {
			tb.Helper()
		}
		c.Observe(core.EventTypeSpanInstanceOffline(), instanceName, at, append(records, recordVersion)...)
	}
}
