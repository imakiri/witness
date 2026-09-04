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

func Info(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogInfo(), msg, core.Caller(1), records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogWarn(), msg, core.Caller(1), records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogDebug(), msg, core.Caller(1), records...)
}

func Error(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogError(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorRF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogError(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorOrInfo(ctx context.Context, okMsg, errMsg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	if err != nil {
		c.Observe(core.EventTypeLogError(), errMsg, core.Caller(1), appendError(records, err)...)
	} else {
		c.Observe(core.EventTypeLogInfo(), okMsg, core.Caller(1), records...)
	}
}

func ErrorStorage(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorStorage(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorStorageF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorStorage(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorNetwork(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorNetwork(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorNetworkF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorNetwork(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorExternal(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorExternal(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorExternalF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorExternal(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func ErrorInternal(ctx context.Context, msg string, err error, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorInternal(), msg, core.Caller(1), appendError(records, err)...)
}

func ErrorInternalF(ctx context.Context, msg string, err error, records ...Record) error {
	var c = core.From(ctx)
	c.Helper()
	c.Observe(core.EventTypeLogErrorInternal(), msg, core.Caller(1), appendError(records, err)...)
	if err != nil {
		return fmt.Errorf("%s: %w", msg, err)
	}
	return errors.New(msg)
}

func Span(ctx context.Context, spanName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var nc = core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	nc.Helper()
	nc.Observe(core.EventTypeSpanStart(), spanName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Helper()
		nc.Observe(core.EventTypeSpanFinish(), spanName, at, records...)
	}
}

// SpanStart is Span with the span_id supplied by the caller instead of
// minted here — for when the id has to exist before the span does, because
// it is going into a message envelope or a header. The span is this
// process's own, like any other: to point at a span another process owns,
// reference it with Link / LinkTo, do not open it.
func SpanStart(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	var c = core.From(ctx).WithChildSpan(spanID)
	c.Helper()
	c.Observe(core.EventTypeSpanStart(), spanName, at, records...)
	return c.To(ctx), func(records ...Record) {
		c.Helper()
		c.Observe(core.EventTypeSpanFinish(), spanName, at, records...)
	}
}

// SpanFinish closes a span by id, for the shape where start and finish are
// not lexically paired and the core.Finish closure cannot be carried between
// them. Prefer the core.Finish that SpanStart returned.
func SpanFinish(ctx context.Context, spanID uuid.UUID, spanName string, records ...Record) {
	var c = core.From(ctx).WithChildSpan(spanID)
	c.Helper()
	c.Observe(core.EventTypeSpanFinish(), spanName, core.Caller(1), records...)
}

// Service is Span with span:service:start/finish event types.
func Service(ctx context.Context, serviceName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	nc := core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	nc.Helper()
	nc.Observe(core.EventTypeSpanServiceStart(), serviceName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Helper()
		nc.Observe(core.EventTypeSpanServiceFinish(), serviceName, at, records...)
	}
}

// Worker is Span with span:wait_group:start/finish event types.
func Worker(ctx context.Context, workerName string, records ...Record) (context.Context, core.Finish) {
	var at = core.Caller(1)
	nc := core.From(ctx).WithChildSpan(uuid.Must(uuid.NewV7()))
	nc.Helper()
	nc.Observe(core.EventTypeSpanWorkerStart(), workerName, at, records...)
	return nc.To(ctx), func(records ...Record) {
		nc.Helper()
		nc.Observe(core.EventTypeSpanWorkerFinish(), workerName, at, records...)
	}
}

// Link mints a span_id, emits a span:link event referencing it, and returns
// it so the caller can put it in whatever carrier it has — an HTTP header,
// a message envelope, a job row. Whoever receives it calls LinkTo with the
// same id, and a single query on that span_id returns both sides.
//
// The link span is *referenced*, never entered: it is appended to this one
// event after the chain, flagged core.SpanFlagLink and nothing else, and the
// caller's core.Context is unchanged. Nobody opens or closes it — a span two
// processes both opened has no meaningful duration.
func Link(ctx context.Context, linkName string, records ...Record) uuid.UUID {
	var linkID = uuid.Must(uuid.NewV7())
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{linkID}, core.EventTypeSpanLink(), linkName, core.Caller(1), records...)
	return linkID
}

// LinkTo is the other half of Link: it emits a span:link event referencing
// a span_id that arrived from elsewhere. The core.Context is unchanged.
func LinkTo(ctx context.Context, linkID uuid.UUID, linkName string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{linkID}, core.EventTypeSpanLink(), linkName, core.Caller(1), records...)
}

// InternalMessageSent is Link with span:internal_message:sent as the event
// type: it mints the message's span_id, emits the send, and returns the id
// for the envelope. Pair it with InternalMessageReceived on the recipient.
func InternalMessageSent(ctx context.Context, msgName string, records ...Record) uuid.UUID {
	var msgID = uuid.Must(uuid.NewV7())
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanInternalMessageSent(), msgName, core.Caller(1), records...)
	return msgID
}

// InternalMessageReceived is LinkTo with span:internal_message:received as
// the event type. Emit it inside the span that handles the message, so the
// handler's span is what a query on msgID finds on this side.
func InternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanInternalMessageReceived(), msgName, core.Caller(1), records...)
}

// ExternalMessageSent is InternalMessageSent across a witness-system
// boundary (outbound HTTP, third-party RPC, etc).
func ExternalMessageSent(ctx context.Context, msgName string, records ...Record) uuid.UUID {
	var msgID = uuid.Must(uuid.NewV7())
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanExternalMessageSent(), msgName, core.Caller(1), records...)
	return msgID
}

// ExternalMessageReceived is InternalMessageReceived across a
// witness-system boundary.
func ExternalMessageReceived(ctx context.Context, msgID uuid.UUID, msgName string, records ...Record) {
	var c = core.From(ctx)
	c.Helper()
	c.ObserveLinked([]uuid.UUID{msgID}, core.EventTypeSpanExternalMessageReceived(), msgName, core.Caller(1), records...)
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
	c := core.NewInstance(tb, observer)
	recordVersion := record{key: "version", value: instanceVersion}
	c.Observe(core.EventTypeSpanInstanceOnline(), instanceName, at, append(records, recordVersion)...)
	return core.With(ctx, c), func(records ...Record) {
		c.Observe(core.EventTypeSpanInstanceOffline(), instanceName, at, append(records, recordVersion)...)
	}
}
