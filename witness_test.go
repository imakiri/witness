package witness

import (
	"context"
	"sync"
	"testing"

	"github.com/gofrs/uuid/v5"
	"github.com/stretchr/testify/require"
)

type captureObserver struct {
	mu     sync.Mutex
	events []Event
}

func (c *captureObserver) Observe(event Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, event)
}

func (c *captureObserver) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = nil
}

func (c *captureObserver) last() Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events[len(c.events)-1]
}

func (c *captureObserver) all() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Event(nil), c.events...)
}

func TestServiceNestsUnderInstance(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "test_instance", "v1")
	ctx, finishSvc := Service(ctx, "auth_service")
	finishSvc()
	finishInst()

	events := obs.all()
	require.Len(t, events, 4)

	rootSpan := events[0].SpanIDs[0]
	require.Equal(t, EventTypeSpanInstanceOnline(), events[0].EventType)

	require.Equal(t, EventTypeSpanServiceStart(), events[1].EventType)
	require.Len(t, events[1].SpanIDs, 2, "service start should carry root + service span_id")
	require.Equal(t, rootSpan, events[1].SpanIDs[0])

	svcSpan := events[1].SpanIDs[1]
	require.NotEqual(t, uuid.Nil, svcSpan)

	require.Equal(t, EventTypeSpanServiceFinish(), events[2].EventType)
	require.Equal(t, []uuid.UUID{rootSpan, svcSpan}, events[2].SpanIDs)

	require.Equal(t, EventTypeSpanInstanceOffline(), events[3].EventType)
}

func TestSpanInsideServiceInheritsServiceSpan(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "inst", "v1")
	ctx, finishSvc := Service(ctx, "svc")
	ctx, finishSpan := Span(ctx, "inner")
	finishSpan()
	finishSvc()
	finishInst()

	events := obs.all()
	// instance:online, service:start, span:start, span:finish, service:finish, instance:offline
	require.Len(t, events, 6)

	rootSpan := events[0].SpanIDs[0]
	svcSpan := events[1].SpanIDs[1]
	require.Equal(t, EventTypeSpanStart(), events[2].EventType)
	require.Len(t, events[2].SpanIDs, 3, "nested span should carry root + service + inner span_id")
	require.Equal(t, rootSpan, events[2].SpanIDs[0])
	require.Equal(t, svcSpan, events[2].SpanIDs[1])
}

func TestInternalMessageSharesMsgID(t *testing.T) {
	obsA := &captureObserver{}
	obsB := &captureObserver{}

	// Two independent witness instances ("producer" and "consumer"), each with
	// its own root span. They share only msgID, which travels over the wire.
	ctxA, finishA := Instance(context.Background(), obsA, "producer", "v1")
	defer finishA()

	ctxB, finishB := Instance(context.Background(), obsB, "consumer", "v1")
	defer finishB()

	msgID := InternalMessageSent(ctxA, "task_msg")
	InternalMessageReceived(ctxB, msgID, "task_msg")

	sentEvents := obsA.all()
	recvEvents := obsB.all()
	require.Len(t, sentEvents, 2) // instance:online + internal_message:sent
	require.Len(t, recvEvents, 2) // instance:online + internal_message:received

	sent := sentEvents[1]
	recv := recvEvents[1]
	require.Equal(t, EventTypeSpanInternalMessageSent(), sent.EventType)
	require.Equal(t, EventTypeSpanInternalMessageReceived(), recv.EventType)

	require.Contains(t, sent.SpanIDs, msgID)
	require.Contains(t, recv.SpanIDs, msgID)

	// Producer and consumer must NOT share any root span — only msgID links them.
	prodRoot := sentEvents[0].SpanIDs[0]
	consRoot := recvEvents[0].SpanIDs[0]
	require.NotEqual(t, prodRoot, consRoot)
}

func TestExternalMessageRoundTrip(t *testing.T) {
	obs := &captureObserver{}
	ctx, finish := Instance(context.Background(), obs, "client", "v1")
	defer finish()

	msgID := ExternalMessageSent(ctx, "outbound_call")
	ExternalMessageReceived(ctx, msgID, "outbound_call_response")

	events := obs.all()
	require.Len(t, events, 3)

	require.Equal(t, EventTypeSpanExternalMessageSent(), events[1].EventType)
	require.Contains(t, events[1].SpanIDs, msgID)

	require.Equal(t, EventTypeSpanExternalMessageReceived(), events[2].EventType)
	require.Contains(t, events[2].SpanIDs, msgID)
}

func TestWorkerInheritsParent(t *testing.T) {
	obs := &captureObserver{}
	ctx, finishInst := Instance(context.Background(), obs, "inst", "v1")
	ctx, finishWorker := Worker(ctx, "w1")
	finishWorker()
	finishInst()

	events := obs.all()
	require.Len(t, events, 4)

	rootSpan := events[0].SpanIDs[0]
	require.Equal(t, EventTypeSpanWorkerStart(), events[1].EventType)
	require.Len(t, events[1].SpanIDs, 2)
	require.Equal(t, rootSpan, events[1].SpanIDs[0])

	workerSpan := events[1].SpanIDs[1]
	require.Equal(t, EventTypeSpanWorkerFinish(), events[2].EventType)
	require.Equal(t, []uuid.UUID{rootSpan, workerSpan}, events[2].SpanIDs)
}

func TestSpanFlags(t *testing.T) {
	t.Run("instance root is own and instance", func(t *testing.T) {
		obs := &captureObserver{}
		_, finish := Instance(context.Background(), obs, "svc", "v1")
		finish()

		online := obs.all()[0]
		require.Len(t, online.SpanIDs, 1)
		require.Equal(t, []SpanFlags{SpanFlagOwn | SpanFlagInstance}, online.SpanFlags)
	})

	t.Run("depth roles follow the chain", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		ctx, _ = Service(ctx, "auth")
		_, _ = Span(ctx, "inner")

		inner := obs.last()
		require.Len(t, inner.SpanIDs, 3)
		require.Equal(t, []SpanFlags{
			SpanFlagAncestor | SpanFlagInstance,
			SpanFlagParent,
			SpanFlagOwn,
		}, inner.SpanFlags)
	})

	t.Run("two element chain has parent but no ancestor", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		_, _ = Span(ctx, "child")

		child := obs.last()
		require.Equal(t, []SpanFlags{
			SpanFlagParent | SpanFlagInstance,
			SpanFlagOwn,
		}, child.SpanFlags)
	})

	t.Run("a link is referenced, never entered", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		ctx, _ = Span(ctx, "handler")

		msgID := InternalMessageSent(ctx, "job")
		sent := obs.last()

		// The chain keeps its positional roles and the link is appended
		// after it carrying SpanFlagLink and nothing else. Exactly one own.
		require.Equal(t, []SpanFlags{
			SpanFlagParent | SpanFlagInstance,
			SpanFlagOwn,
			SpanFlagLink,
		}, sent.SpanFlags)
		require.Equal(t, msgID, sent.SpanIDs[2])

		var owns int
		for _, f := range sent.SpanFlags {
			if f&SpanFlagOwn != 0 {
				owns++
			}
		}
		require.Equal(t, 1, owns, "an event has exactly one own span")

		// The Context is untouched: the next event carries the chain alone.
		Info(ctx, "still here")
		require.Equal(t, []SpanFlags{
			SpanFlagParent | SpanFlagInstance,
			SpanFlagOwn,
		}, obs.last().SpanFlags)
	})

	t.Run("both halves of a hand-off reference the same span", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "consumer", "v1")
		ctx, _ = Span(ctx, "handler")
		msgID := uuid.Must(uuid.NewV7())

		InternalMessageReceived(ctx, msgID, "job")
		recv := obs.last()
		require.Equal(t, msgID, recv.SpanIDs[2])
		require.Equal(t, SpanFlags(SpanFlagLink), recv.SpanFlags[2],
			"a link carries no positional role")
	})

	t.Run("Link mints an id, LinkTo references one", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")

		linkID := Link(ctx, "job dispatch")
		minted := obs.last()
		require.Equal(t, EventTypeSpanLink(), minted.EventType)
		require.Equal(t, linkID, minted.SpanIDs[1])
		require.Equal(t, SpanFlags(SpanFlagLink), minted.SpanFlags[1])

		LinkTo(ctx, linkID, "job dispatch")
		joined := obs.last()
		require.Equal(t, EventTypeSpanLink(), joined.EventType)
		require.Equal(t, linkID, joined.SpanIDs[1])
		require.Equal(t, SpanFlags(SpanFlagLink), joined.SpanFlags[1])
	})

	t.Run("a link already in the chain is not duplicated", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, _ := Instance(context.Background(), obs, "svc", "v1")
		own := From(ctx).CurrentSpanID()

		// A duplicate span_id in one event would trip the unique
		// (event_id, span_id) index and, because Postgres batches, take
		// every event queued with it down too.
		LinkTo(ctx, own, "self")

		last := obs.last()
		require.Len(t, last.SpanIDs, 1)
		require.Equal(t, []SpanFlags{SpanFlagOwn | SpanFlagInstance}, last.SpanFlags)
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
			require.Equal(t, SpanFlags(SpanFlagOwn), e.SpanFlags[1],
				"a caller-supplied id is still this process's own span")
		}
	})

	t.Run("Test opens an instance named after the test", func(t *testing.T) {
		obs := &captureObserver{}
		ctx, finish := Test(context.Background(), t, obs)
		finish()

		online := obs.all()[0]
		require.Equal(t, t.Name(), online.EventMessage)
		require.Equal(t, []SpanFlags{SpanFlagOwn | SpanFlagInstance}, online.SpanFlags)
		require.Equal(t, From(ctx).InstanceSpanID(), online.SpanIDs[0])
	})
}
