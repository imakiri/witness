package testenv

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
	"github.com/imakiri/witness/record"
	"github.com/segmentio/kafka-go"
)

const (
	topic = "orders"

	// Arrival rate of the ingest service, and the number of processor
	// workers. One reader fetches messages one at a time and hands them to
	// the pool: a consumer group over a one-partition topic gives an
	// assignment to a single member, so "more consumers" would silently be
	// one consumer and the demo would run an order of magnitude behind the
	// producer while looking healthy.
	requestsPerSecond = 140
	processorWorkers  = 32

	batchWindow = 200 * time.Millisecond
)

const msgIDHeader = "witness-msg-id"

func between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(rand.Int63n(int64(hi-lo)))
}

// createTopic is worth doing explicitly: auto-creation happens on first
// produce, and a reader that started first spends its life logging that the
// topic is unknown.
func createTopic(ctx context.Context, brokers []string) error {
	client := &kafka.Client{Addr: kafka.TCP(brokers...), Timeout: 10 * time.Second}
	_, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{Topic: topic, NumPartitions: 1, ReplicationFactor: 1}},
	})
	return err
}

// RunIngest accepts requests at a fixed rate, does some work, and hands each
// one to Kafka. Its request span is the trace root: nothing triggered it.
func RunIngest(ctx context.Context, obs core.Observer, brokers []string) {
	ctx, finish := witness.Instance(ctx, obs, "ingest", "1.0",
		record.String("rate", "140/s"), record.String("sink", topic))
	defer finish()

	writer := &kafka.Writer{
		Addr:         kafka.TCP(brokers...),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		BatchTimeout: 10 * time.Millisecond,
		Async:        false,
	}
	defer writer.Close()

	witness.Info(ctx, "ingest listening", record.Int("rps", requestsPerSecond))

	var wg sync.WaitGroup
	ticker := time.NewTicker(time.Second / requestsPerSecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleRequest(ctx, writer)
			}()
		}
	}
}

func handleRequest(ctx context.Context, writer *kafka.Writer) {
	var orderID = uuid.Must(uuid.NewV7())
	ctx, finish := witness.Span(ctx, "receive-order",
		record.String("order_id", orderID.String()),
		record.String("channel", "http"))

	var took time.Duration
	defer func() { finish(record.String("took", took.String())) }()

	started := time.Now()
	witness.Info(ctx, "order accepted")

	time.Sleep(between(50*time.Millisecond, 100*time.Millisecond))
	witness.Info(ctx, "order validated")

	// The id travels in the message and both sides name it: the send is a
	// hand-off, so it is emitted after the produce succeeded — a failed
	// produce handed nothing off and is an error in this span.
	var msgID = uuid.Must(uuid.NewV7())
	err := writer.WriteMessages(ctx, kafka.Message{
		Key:     orderID.Bytes(),
		Value:   []byte(orderID.String()),
		Headers: []kafka.Header{{Key: msgIDHeader, Value: msgID.Bytes()}},
	})
	if err != nil {
		witness.Error(ctx, "produce", err)
		took = time.Since(started)
		return
	}
	witness.Sent(ctx, msgID, "order -> kafka", record.String("topic", topic))
	witness.Count(ctx, "orders_produced", 1)
	took = time.Since(started)
}

// RunProcessor reads Kafka one message at a time, works on each, then hands
// it to an in-process batching sub-service.
func RunProcessor(ctx context.Context, obs core.Observer, brokers []string) {
	ctx, finish := witness.Instance(ctx, obs, "processor", "1.0",
		record.Int("workers", processorWorkers),
		record.String("batch_window", batchWindow.String()))
	defer finish()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  brokers,
		Topic:    topic,
		GroupID:  "processor",
		MinBytes: 1,
		MaxBytes: 10 << 20,
	})
	defer reader.Close()

	jobs := make(chan job, processorWorkers)
	batched := make(chan job, 4*processorWorkers)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); runBatcher(ctx, batched) }()

	for i := 0; i < processorWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				process(ctx, j, batched)
			}
		}()
	}

	witness.Info(ctx, "processor consuming", record.String("topic", topic))

	for {
		msg, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				witness.Error(ctx, "read message", err)
				continue
			}
			break
		}
		msgID, ok := headerUUID(msg.Headers, msgIDHeader)
		if !ok {
			witness.Warn(ctx, "message without a witness id")
			continue
		}
		select {
		case jobs <- job{msgID: msgID, orderID: string(msg.Value)}:
		case <-ctx.Done():
		}
	}

	close(jobs)
	wg.Wait()
}

type job struct {
	msgID   uuid.UUID // the hand-off this job answers
	nextID  uuid.UUID // the hand-off it makes to the batcher
	orderID string
}

// process is one message. Handle opens the span *and* records the received
// half in it, so the span that took the work is the span the trace walk
// arrives at. It hangs off the instance ctx, not off a long-lived consumer
// span: a consumer span would be a trace root whose walk drags in every
// message the process ever handled.
func process(ctx context.Context, j job, batched chan<- job) {
	ctx, finish := witness.Handle(ctx, j.msgID, "process order",
		record.String("order_id", j.orderID))
	defer finish()

	time.Sleep(between(70*time.Millisecond, 150*time.Millisecond))
	witness.Info(ctx, "order enriched")

	// A fresh id: reusing the Kafka one would put two receivers on it. The
	// hop is in-process, which is exactly the case link_edges must draw —
	// it deliberately does not require the two sides to be in different
	// instances.
	j.nextID = uuid.Must(uuid.NewV7())
	witness.Sent(ctx, j.nextID, "order -> batcher")

	select {
	case batched <- j:
	case <-ctx.Done():
	}
}

// runBatcher collects jobs for batchWindow and runs each batch. Batches run
// concurrently: a batch takes longer than the window that filled it, so a
// serial batcher would fall behind for good.
func runBatcher(ctx context.Context, in <-chan job) {
	ticker := time.NewTicker(batchWindow)
	defer ticker.Stop()

	var (
		wg    sync.WaitGroup
		batch []job
	)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		b := batch
		batch = nil
		wg.Add(1)
		go func() { defer wg.Done(); settle(ctx, b) }()
	}
	for {
		select {
		case <-ctx.Done():
			flush()
			wg.Wait()
			return
		case j := <-in:
			batch = append(batch, j)
		case <-ticker.C:
			flush()
		}
	}
}

// settle is one batch: one span, one fan-in event naming every job it took
// in. The links go on this per-batch span and nowhere else — re-linking each
// subsequent event would cost rows × n for no query power, and putting them
// on a long-lived batcher span would make every job's trace walk drag in the
// batcher's whole history.
func settle(ctx context.Context, batch []job) {
	msgIDs := make([]uuid.UUID, len(batch))
	for i, j := range batch {
		msgIDs[i] = j.nextID
	}

	ctx, finish := witness.HandleAll(ctx, msgIDs, "settle batch",
		record.Int("size", len(batch)))
	defer finish(record.Int("settled", len(batch)))

	time.Sleep(between(80*time.Millisecond, 120*time.Millisecond))
	witness.Info(ctx, "batch settled")
	witness.Sample(ctx, "batch_size", float64(len(batch)))

	// A call to something outside the system: an ordinary child span, no
	// link — there is no peer emitting the other half.
	_, finishCall := witness.Span(ctx, "settlement-api call",
		record.String("peer", "settlement.example"))
	time.Sleep(between(100*time.Millisecond, 300*time.Millisecond))
	finishCall(record.Int("status", 200))
}

func headerUUID(headers []kafka.Header, key string) (uuid.UUID, bool) {
	for _, h := range headers {
		if h.Key != key {
			continue
		}
		id, err := uuid.FromBytes(h.Value)
		if err != nil {
			return uuid.Nil, false
		}
		return id, true
	}
	return uuid.Nil, false
}
