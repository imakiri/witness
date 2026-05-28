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

	msgID := uuid.Must(uuid.NewV7())
	InternalMessageSent(ctxA, msgID, "task_msg")
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

	msgID := uuid.Must(uuid.NewV7())
	ExternalMessageSent(ctx, msgID, "outbound_call")
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
