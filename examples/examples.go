// The four shapes of a hand-off, as small as they go. Nothing here runs —
// the interfaces stand in for whatever transport you actually have, because
// witness has no opinion about it: an id travels in the carrier, and both
// sides emit events referencing it.
//
// The rules these illustrate, in one place:
//
//   - The id is minted by whoever introduces it, before the send, because it
//     travels in the carrier. The *event* says the hand-off happened, so it
//     comes after the send succeeded.
//   - A hand-off is one direction. There is no reply event: an answer is
//     either another hand-off with its own id, or — for a synchronous call —
//     nothing at all, since the round trip is the calling span's duration.
//   - The receiving side emits Received (or opens its span with Handle).
//     Never Link: span:link counts as the sending side.
package main

import (
	"context"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
)

type Message interface {
	MessageID() uuid.UUID
}

type Receiver interface {
	Receive(ctx context.Context) (msg Message, received bool)
}

// ExampleReceiving takes a message off a carrier inside its own span.
//
// The span opens before the receive, so a poll that came back empty is still
// visible — its cost is one span with no message in it. Open it after the
// `got` check instead if empty polls are frequent and their latency is not
// interesting.
func ExampleReceiving(ctx context.Context, receiver Receiver) {
	ctx, finish := witness.Span(ctx, "receiving")
	defer finish()

	msg, got := receiver.Receive(ctx)
	if !got {
		return
	}
	witness.Received(ctx, msg.MessageID(), "settle request")

	// ... process the message; these are ordinary events of this span, and
	// the link is not sticky — it lives in the Received event alone.
	witness.Info(ctx, "settled")
}

type Sender interface {
	Send(ctx context.Context, msgID uuid.UUID) error
}

// ExampleSending hands a message off, recording both the intent and the fact.
//
// Link says "about to hand this id off" and Sent says "it went". Both count
// as the sending side, so the edge to whoever receives the id exists either
// way — the Link is what keeps it if this process dies inside Send. The
// price is a link with no receiving half when the send never happened; find
// those with
//
//	SELECT * FROM witness.span_link_sides WHERE sent_at IS NOT NULL AND received_at IS NULL
//
// A send that failed handed nothing off, so it is an error in this span and
// no Sent event.
func ExampleSending(ctx context.Context, sender Sender) {
	ctx, finish := witness.Span(ctx, "sending")
	defer finish()

	msgID := uuid.Must(uuid.NewV7())
	witness.Link(ctx, msgID, "settle request")

	if err := sender.Send(ctx, msgID); err != nil {
		witness.Error(ctx, "send failed", err)
		return
	}
	witness.Sent(ctx, msgID, "settle request")
}

type Request struct {
	MessageID uuid.UUID
}

type Response struct{}

type Caller interface {
	Call(ctx context.Context, req Request) (res Response, err error)
}

// ExampleCall is a synchronous call: a span around it, a Link naming the id
// the callee will see, and nothing on the way back.
//
// The response needs no event. Its arrival is what ends this span, so the
// round trip is the span's duration — one row in span_pairs instead of two
// events something has to subtract. Recording a Received for the response
// would also say this span was triggered by its own callee, which is how it
// would stop being the root of its trace.
func ExampleCall(ctx context.Context, caller Caller) {
	ctx, finish := witness.Span(ctx, "call settle")
	defer finish()

	req := Request{MessageID: uuid.Must(uuid.NewV7())}
	witness.Link(ctx, req.MessageID, "settle request")

	if _, err := caller.Call(ctx, req); err != nil {
		witness.Error(ctx, "call failed", err)
		return
	}
	witness.Info(ctx, "settled")
}

// ExampleHandling is the other side of ExampleCall. Handle opens the span
// for the work the request triggered and records the request arriving inside
// it — Span plus Received in one call, which is the point: emitted in the
// dispatching span instead, the message would hang on a span that outlives
// the work.
func ExampleHandling(ctx context.Context, req Request) (Response, error) {
	ctx, finish := witness.Handle(ctx, req.MessageID, "settle request")
	defer finish()

	witness.Info(ctx, "settled")
	return Response{}, nil
}

// ExampleBatch is ExampleHandling for a worker that takes a whole batch at
// once: one span for the batch, one event referencing every request in it.
//
// A worker cannot enter the n spans that fed it — a chain has one current
// span, so n parents are not expressible — and the relation "this worker is
// processing that request" is a link. Opening a span per batch is what keeps
// those links off the long-lived worker span, which would otherwise drag
// every batch it ever ran into any one request's trace.
func ExampleBatch(ctx context.Context, batch []Request) error {
	ctx, finish := witness.HandleAll(ctx,
		record.Map(batch, func(r Request) uuid.UUID { return r.MessageID }),
		"settle batch", record.Int("size", len(batch)))
	defer finish()

	witness.Info(ctx, "batch settled")
	return nil
}
