// Batch: n requests hand work to an in-process queue, one worker takes them
// all at once.
//
// The worker cannot enter the n request spans — a chain has one current span,
// so n parents are not expressible, and "this worker is processing that
// request" is a link, not parenthood. Each request hands off its span with
// witness.Sent, and the worker takes the whole batch in one span with
// witness.HandleAll, which records every msgID in a single fan-in event.
//
// A hand-off has one direction and no reply half. If the request waited for
// an answer, the answer is either another hand-off with its own id, or — for
// a synchronous call — no event at all, the round trip being the calling
// span's duration.
//
//	go run ./batch
package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/printers"
	"github.com/imakiri/witness/record"
)

const batchSize = 3

type job struct {
	orderID string
	// msgID is the span_id shared with the request that enqueued this job.
	// It travels in the job, exactly like a traceparent travels in a header —
	// witness does not care what the carrier is.
	msgID uuid.UUID
}

func main() {
	printer, err := printers.NewPretty()
	if err != nil {
		log.Fatalln("printers.NewPretty failed with error:", err)
	}
	observer, err := stdlog.NewObserver(printer)
	if err != nil {
		log.Fatalln("stdlog.NewObserver failed with error:", err)
	}

	ctx, finish := witness.Instance(context.Background(), observer, "example.batch", "1")
	defer finish()

	var queue = make(chan job, 16)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		worker(ctx, queue)
	}()

	for i := 0; i < 7; i++ {
		handle(ctx, queue, "order-"+time.Now().Format("150405.000000"))
	}

	close(queue)
	wg.Wait()
}

// handle is one inbound request: an ordinary span under the instance. It does
// its own work, hands the rest to the queue, and returns — it does not wait
// for the batch, and its span finishes long before the worker runs.
func handle(ctx context.Context, queue chan<- job, orderID string) {
	ctx, finish := witness.Span(ctx, "POST /order")
	defer finish()

	witness.Info(ctx, "order accepted", record.String("order_id", orderID))

	// The msgID has to exist before the send — it travels inside the job —
	// but the event says the hand-off *happened*, so it is emitted after the
	// send succeeds. Emitting it up front would leave a link nobody will ever
	// answer behind every dropped attempt.
	var msgID = uuid.Must(uuid.NewV7())

	attempts, err := enqueue(queue, job{orderID: orderID, msgID: msgID})
	if err != nil {
		// No link: nothing was handed off. The failure belongs to this span.
		witness.Error(ctx, "enqueue failed", err,
			record.Int("attempts", attempts))
		return
	}

	// One event however many attempts it took — the retries are a record on
	// it, not links of their own. Reusing msgID across attempts also keeps
	// an at-least-once delivery from drawing two edges to the same worker.
	witness.Sent(ctx, msgID, "enqueue settle",
		record.Int("attempts", attempts))
}

// enqueue retries a non-blocking send. Returns the number of attempts made.
//
// ponytail: fixed 3 tries, fixed backoff. The witness shape is the same
// whatever the retry policy is.
func enqueue(queue chan<- job, j job) (int, error) {
	for attempt := 1; attempt <= 3; attempt++ {
		select {
		case queue <- j:
			return attempt, nil
		default:
			time.Sleep(time.Millisecond)
		}
	}
	return 3, errors.New("queue full")
}

// worker drains the queue in batches. Its span is long-lived, so every batch
// gets its own child span: links accumulated on the worker span would drag
// the worker's entire history into any request's trace.
func worker(ctx context.Context, queue <-chan job) {
	ctx, finish := witness.Span(ctx, "settle_worker")
	defer finish()

	var batch = make([]job, 0, batchSize)
	var flush = func() {
		if len(batch) == 0 {
			return
		}
		settle(ctx, batch)
		batch = batch[:0]
	}

	// ponytail: size-only batching, no timer. A real worker also flushes on a
	// tick — that changes nothing about the witness shape.
	for j := range queue {
		if batch = append(batch, j); len(batch) == batchSize {
			flush()
		}
	}
	flush()
}

// settle is one batch: one span, one fan-in event naming every request it
// took in, then ordinary events in that span.
func settle(ctx context.Context, batch []job) {
	var msgIDs = make([]uuid.UUID, len(batch))
	for i, j := range batch {
		msgIDs[i] = j.msgID
	}

	// One span for the batch, one event connecting it to all n requests.
	// Everything after is an ordinary event in this span, and a trace walk
	// from any of the n requests already reaches all of them.
	ctx, finish := witness.HandleAll(ctx, msgIDs, "dequeue settle",
		record.Int("size", len(batch)))
	defer finish()

	time.Sleep(10 * time.Millisecond)
	witness.Info(ctx, "batch settled")

	// An event about one item links only that item — fanning in here would
	// claim every request in the batch was involved.
	witness.Link(ctx, batch[0].msgID, "order flagged for review",
		record.String("order_id", batch[0].orderID))
}
