package witness

import (
	"context"
	"github.com/imakiri/witness/core"
	"sync"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

type captureObserver struct {
	mu     sync.Mutex
	events []core.Event
}

func (c *captureObserver) Observe(event core.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *captureObserver) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

func (c *captureObserver) last() core.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events[len(c.events)-1]
}

func (c *captureObserver) all() []core.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]core.Event(nil), c.events...)
}

func TestSpanNestsUnderInstance(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "test_instance", "v1")
	ctx, finishSvc := Span(ctx, "auth_service")
	finishSvc()
	finishInst()

	events := obs.all()
	require.Len(t, events, 4)

	rootSpan := events[0].SpanIDs[0]
	require.Equal(t, core.EventTypeSpanInstanceOnline(), events[0].EventType)

	require.Equal(t, core.EventTypeSpanStart(), events[1].EventType)
	require.Len(t, events[1].SpanIDs, 2, "span start should carry root + span_id")
	require.Equal(t, rootSpan, events[1].SpanIDs[0])

	svcSpan := events[1].SpanIDs[1]
	require.NotEqual(t, uuid.Nil, svcSpan)

	require.Equal(t, core.EventTypeSpanFinish(), events[2].EventType)
	require.Equal(t, []uuid.UUID{rootSpan, svcSpan}, events[2].SpanIDs)

	require.Equal(t, core.EventTypeSpanInstanceOffline(), events[3].EventType)
}

func TestSpanInsideServiceInheritsServiceSpan(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "inst", "v1")
	ctx, finishSvc := Span(ctx, "svc")
	ctx, finishSpan := Span(ctx, "inner")
	finishSpan()
	finishSvc()
	finishInst()

	events := obs.all()
	// instance:online, service:start, span:start, span:finish, service:finish, instance:offline
	require.Len(t, events, 6)

	rootSpan := events[0].SpanIDs[0]
	svcSpan := events[1].SpanIDs[1]
	require.Equal(t, core.EventTypeSpanStart(), events[2].EventType)
	require.Len(t, events[2].SpanIDs, 3, "nested span should carry root + service + inner span_id")
	require.Equal(t, rootSpan, events[2].SpanIDs[0])
	require.Equal(t, svcSpan, events[2].SpanIDs[1])
}

func TestMessageSharesMsgID(t *testing.T) {
	obsA := &captureObserver{}
	obsB := &captureObserver{}

	// Two independent witness instances ("producer" and "consumer"), each with
	// its own root span. They share only msgID, which travels over the wire.
	ctxA, finishA := Instance(context.Background(), obsA, "producer", "v1")
	defer finishA()

	ctxB, finishB := Instance(context.Background(), obsB, "consumer", "v1")
	defer finishB()

	var msgID = uuid.Must(uuid.NewV7())
	Sent(ctxA, msgID, "task_msg")
	Received(ctxB, msgID, "task_msg")

	sentEvents := obsA.all()
	recvEvents := obsB.all()
	require.Len(t, sentEvents, 2) // instance:online + message:sent
	require.Len(t, recvEvents, 2) // instance:online + message:received

	sent := sentEvents[1]
	recv := recvEvents[1]
	require.Equal(t, core.EventTypeSpanMessageSent(), sent.EventType)
	require.Equal(t, core.EventTypeSpanMessageReceived(), recv.EventType)

	require.Contains(t, sent.SpanIDs, msgID)
	require.Contains(t, recv.SpanIDs, msgID)

	// Producer and consumer must NOT share any root span — only msgID links them.
	prodRoot := sentEvents[0].SpanIDs[0]
	consRoot := recvEvents[0].SpanIDs[0]
	require.NotEqual(t, prodRoot, consRoot)
}

func TestMessageRoundTrip(t *testing.T) {
	obs := &captureObserver{}
	ctx, finish := Instance(context.Background(), obs, "client", "v1")
	defer finish()

	var msgID = uuid.Must(uuid.NewV7())
	Sent(ctx, msgID, "outbound_call")
	Received(ctx, msgID, "outbound_call_response")

	events := obs.all()
	require.Len(t, events, 3)

	require.Equal(t, core.EventTypeSpanMessageSent(), events[1].EventType)
	require.Contains(t, events[1].SpanIDs, msgID)

	require.Equal(t, core.EventTypeSpanMessageReceived(), events[2].EventType)
	require.Contains(t, events[2].SpanIDs, msgID)
}

func TestNestedSpanInheritsParent(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "inst", "v1")
	ctx, finishWorker := Span(ctx, "w1")
	finishWorker()
	finishInst()

	events := obs.all()
	require.Len(t, events, 4)

	rootSpan := events[0].SpanIDs[0]
	require.Equal(t, core.EventTypeSpanStart(), events[1].EventType)
	require.Len(t, events[1].SpanIDs, 2)
	require.Equal(t, rootSpan, events[1].SpanIDs[0])

	workerSpan := events[1].SpanIDs[1]
	require.Equal(t, core.EventTypeSpanFinish(), events[2].EventType)
	require.Equal(t, []uuid.UUID{rootSpan, workerSpan}, events[2].SpanIDs)
}

func TestSpanFlags(t *testing.T) {
	t.Run("instance root is own and instance", func(t *testing.T) {
		obs := &captureObserver{}
		_, finish := Instance(context.Background(), obs, "svc", "v1")
		finish()

		online := obs.all()[0]
		require.Len(t, online.SpanIDs, 1)
		require.Equal(t, []core.SpanFlags{core.SpanFlagOwn | core.SpanFlagInstance}, online.SpanFlags)
	})

	t.Run("depth roles follow the chain", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		ctx, _ = Span(ctx, "auth")
		_, _ = Span(ctx, "inner")

		inner := obs.last()
		require.Len(t, inner.SpanIDs, 3)
		require.Equal(t, []core.SpanFlags{
			core.SpanFlagAncestor | core.SpanFlagInstance,
			core.SpanFlagParent,
			core.SpanFlagOwn,
		}, inner.SpanFlags)
	})

	t.Run("depth four separates the instance from a plain ancestor", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		ctx, _ = Span(ctx, "auth")
		ctx, _ = Span(ctx, "outer")
		_, _ = Span(ctx, "inner")

		inner := obs.last()
		require.Len(t, inner.SpanIDs, 4)
		require.Equal(t, []core.SpanFlags{
			core.SpanFlagAncestor | core.SpanFlagInstance, // 12
			core.SpanFlagAncestor,                         // 4 — the first
			core.SpanFlagParent,                           // 2   middle ancestor
			core.SpanFlagOwn,                              // 1
		}, inner.SpanFlags)
	})

	t.Run("two element chain has parent but no ancestor", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		_, _ = Span(ctx, "child")

		child := obs.last()
		require.Equal(t, []core.SpanFlags{
			core.SpanFlagParent | core.SpanFlagInstance,
			core.SpanFlagOwn,
		}, child.SpanFlags)
	})

	t.Run("a link is referenced, never entered", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		ctx, _ = Span(ctx, "handler")

		var msgID = uuid.Must(uuid.NewV7())
		Sent(ctx, msgID, "job")
		sent := obs.last()

		// The chain keeps its positional roles and the link is appended
		// after it carrying core.SpanFlagLink and nothing else. Exactly one own.
		require.Equal(t, []core.SpanFlags{
			core.SpanFlagParent | core.SpanFlagInstance,
			core.SpanFlagOwn,
			core.SpanFlagLink,
		}, sent.SpanFlags)
		require.Equal(t, msgID, sent.SpanIDs[2])

		var owns int
		for _, f := range sent.SpanFlags {
			if f&core.SpanFlagOwn != 0 {
				owns++
			}
		}
		require.Equal(t, 1, owns, "an event has exactly one own span")

		// The core.Context is untouched: the next event carries the chain alone.
		Info(ctx, "still here")
		require.Equal(t, []core.SpanFlags{
			core.SpanFlagParent | core.SpanFlagInstance,
			core.SpanFlagOwn,
		}, obs.last().SpanFlags)
	})

	t.Run("both halves of a hand-off reference the same span", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "consumer", "v1")
		ctx, _ = Span(ctx, "handler")
		msgID := uuid.Must(uuid.NewV7())

		Received(ctx, msgID, "job")
		recv := obs.last()
		require.Equal(t, msgID, recv.SpanIDs[2])
		require.Equal(t, core.SpanFlags(core.SpanFlagLink), recv.SpanFlags[2],
			"a link carries no positional role")
	})

	t.Run("both sides of a link emit the same id", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")

		var linkID = uuid.Must(uuid.NewV7())
		Link(ctx, linkID, "job dispatch")
		minted := obs.last()
		require.Equal(t, core.EventTypeSpanLink(), minted.EventType)
		require.Equal(t, linkID, minted.SpanIDs[1])
		require.Equal(t, core.SpanFlags(core.SpanFlagLink), minted.SpanFlags[1])

		Link(ctx, linkID, "job dispatch")
		joined := obs.last()
		require.Equal(t, core.EventTypeSpanLink(), joined.EventType)
		require.Equal(t, linkID, joined.SpanIDs[1])
		require.Equal(t, core.SpanFlags(core.SpanFlagLink), joined.SpanFlags[1])
	})

	t.Run("a link already in the chain is not duplicated", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		own := core.From(ctx).CurrentSpanID()

		// A duplicate span_id in one event would trip the unique
		// (event_id, span_id) index and, because Postgres batches, take
		// every event queued with it down too.
		Link(ctx, own, "self")

		last := obs.last()
		require.Len(t, last.SpanIDs, 1)
		require.Equal(t, []core.SpanFlags{core.SpanFlagOwn | core.SpanFlagInstance}, last.SpanFlags)
	})

	t.Run("SpanStart opens a span with a caller-supplied id", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		spanID := uuid.Must(uuid.NewV7())

		ctx, finish := SpanStart(ctx, spanID, "preallocated")
		Info(ctx, "inside")
		finish()

		events := obs.all()
		for _, e := range events[1:4] {
			require.Equal(t, spanID, e.SpanIDs[1])
			require.Equal(t, core.SpanFlags(core.SpanFlagOwn), e.SpanFlags[1],
				"a caller-supplied id is still this process's own span")
		}
	})

	t.Run("Test opens an instance named after the test", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, finish := Test(context.Background(), t, obs)
		finish()

		online := obs.all()[0]
		require.Equal(t, t.Name(), online.EventMessage)
		require.Equal(t, []core.SpanFlags{core.SpanFlagOwn | core.SpanFlagInstance}, online.SpanFlags)
		require.Equal(t, core.From(ctx).InstanceSpanID(), online.SpanIDs[0])
	})
}

// TestHandle covers the receiving constructor: one span for the work a
// message triggers, the message recorded inside it, and nothing sticky left
// on the context afterwards.
func TestHandle(t *testing.T) {
	obs := &captureObserver{}
	ctx, _ := Instance(context.Background(), obs, "consumer", "v1")
	var msgID = uuid.Must(uuid.NewV7())

	hctx, finish := Handle(ctx, msgID, "settle job")
	Info(hctx, "working")
	finish()

	var events = obs.all()[1:] // drop instance:online
	require.Len(t, events, 4)

	require.Equal(t, core.EventTypeSpanStart(), events[0].EventType)
	var span = events[0].SpanIDs[1]

	// The message lands in the handler's span, not in its caller's.
	require.Equal(t, core.EventTypeSpanMessageReceived(), events[1].EventType)
	require.Equal(t, []uuid.UUID{events[0].SpanIDs[0], span, msgID}, events[1].SpanIDs)
	require.Equal(t, core.SpanFlagOwn, events[1].SpanFlags[1])
	require.Equal(t, core.SpanFlagLink, events[1].SpanFlags[2])

	// The link is not sticky: it lives in that one event.
	require.Equal(t, core.EventTypeLogInfo(), events[2].EventType)
	require.Equal(t, []uuid.UUID{events[0].SpanIDs[0], span}, events[2].SpanIDs)

	require.Equal(t, core.EventTypeSpanFinish(), events[3].EventType)
	require.Equal(t, []uuid.UUID{events[0].SpanIDs[0], span}, events[3].SpanIDs)
}

// TestHandleAll is the batch shape: n requests hand work off, a worker takes
// the whole batch in one span, and one event references all n.
//
// The worker never enters the request spans — a chain has one current span
// and roles are positional, so n parents are not expressible. The requests
// appear only as links on that one event, which is what makes the batch
// reachable from each request's trace walk without any of them owning the
// worker's spans.
func TestHandleAll(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "batcher", "v1")
	defer finishInst()

	const n = 3
	var msgIDs = make([]uuid.UUID, 0, n)
	var reqSpans = make([]uuid.UUID, 0, n)
	for i := 0; i < n; i++ {
		reqCtx, finishReq := Span(ctx, "POST /order")
		var msgID = uuid.Must(uuid.NewV7())
		Sent(reqCtx, msgID, "enqueue")
		msgIDs = append(msgIDs, msgID)
		reqSpans = append(reqSpans, core.From(reqCtx).CurrentSpanID())
		finishReq()
	}

	// The worker is long-lived; the batch gets its own span, so the worker
	// span never accumulates a batch's links.
	workerCtx, finishWorker := Span(ctx, "settle_worker")
	defer finishWorker()
	batchCtx, finishBatch := HandleAll(workerCtx, msgIDs, "dequeue", record{key: "size", value: "3"})
	Info(batchCtx, "batch written")
	finishBatch()

	var byType = func(et core.EventType) []core.Event {
		var out []core.Event
		for _, e := range obs.all() {
			if e.EventType == et {
				out = append(out, e)
			}
		}
		return out
	}

	var fanIn = byType(core.EventTypeSpanMessageReceived())
	require.Len(t, fanIn, 1, "one event per batch, not one per item")

	// instance, worker, batch, then the n links — chain first, links after.
	require.Len(t, fanIn[0].SpanIDs, 3+n)
	require.Equal(t, core.SpanFlagInstance|core.SpanFlagAncestor, fanIn[0].SpanFlags[0])
	require.Equal(t, core.SpanFlagParent, fanIn[0].SpanFlags[1])
	require.Equal(t, core.SpanFlagOwn, fanIn[0].SpanFlags[2])
	for i := 3; i < 3+n; i++ {
		require.Equal(t, core.SpanFlagLink, fanIn[0].SpanFlags[i],
			"a referenced request carries link and nothing else")
	}
	require.Equal(t, msgIDs, fanIn[0].SpanIDs[3:])

	// A msgID arriving twice — a retried job re-enqueued under the same id —
	// must be referenced once: a duplicate span_id in one event violates the
	// unique (event_id, span_id) index, which takes the whole pgx batch down.
	ReceivedAll(batchCtx, []uuid.UUID{msgIDs[0], msgIDs[0]}, "dequeue")
	require.Len(t, byType(core.EventTypeSpanMessageReceived())[1].SpanIDs, 4)

	// Each link is the id the matching request handed off.
	var sent = byType(core.EventTypeSpanMessageSent())
	require.Len(t, sent, n)
	for i, e := range sent {
		require.Equal(t, msgIDs[i], e.SpanIDs[len(e.SpanIDs)-1])
		require.Equal(t, reqSpans[i], e.SpanIDs[1])
	}

	// Events after the fan-in are plain events in the batch span: the links
	// are not sticky, the Context never carried them.
	var logs = byType(core.EventTypeLogInfo())
	require.Len(t, logs, 1)
	require.Len(t, logs[0].SpanIDs, 3)
}

// TestMetrics pins the shape every metric observer reads: the event type
// says which kind of metric it is, event_message is the metric name, and the
// value rides in a record keyed "value", first, ahead of the labels.
func TestMetrics(t *testing.T) {
	obs := &captureObserver{}
	ctx, _ := Instance(context.Background(), obs, "svc", "v1")

	Count(ctx, "jobs_total", 2, record{key: "queue", value: "settle"})
	Gauge(ctx, "queue_depth", 41.5)
	Sample(ctx, "job_seconds", 0.25)

	var events = obs.all()[1:]
	require.Len(t, events, 3)

	for i, want := range []struct {
		eventType core.EventType
		name      string
		value     string
	}{
		{core.EventTypeMetricCounter(), "jobs_total", "2"},
		{core.EventTypeMetricGauge(), "queue_depth", "41.5"},
		{core.EventTypeMetricHistogram(), "job_seconds", "0.25"},
	} {
		require.Equal(t, want.eventType, events[i].EventType)
		require.Equal(t, want.name, events[i].EventMessage)
		require.True(t, events[i].Records[0].KeyEqual("value"))
		require.Equal(t, want.value, string(events[i].Records[0].AppendValue(nil)))
	}

	// Labels follow the value, in the order they were passed.
	require.Len(t, events[0].Records, 2)
	require.True(t, events[0].Records[1].KeyEqual("queue"))
}
