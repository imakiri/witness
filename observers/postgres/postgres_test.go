package postgres

import (
	"context"
	_ "embed"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed 000_schema.up.sql
var schemaSQL string

// stubConn is a no-op connection used by lifecycle / race / backpressure tests
// that do not need a real Postgres. The hooks let individual tests synchronize
// with worker progress (e.g. block flush, count batches).
type stubConn struct {
	sendBatch func(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	batches   atomic.Uint64
	closed    atomic.Bool
}

func (s *stubConn) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	s.batches.Add(1)
	if s.sendBatch != nil {
		return s.sendBatch(ctx, b)
	}
	return noopBatchResults{}
}

func (s *stubConn) Close() { s.closed.Store(true) }

type noopBatchResults struct{}

func (noopBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil }
func (noopBatchResults) Query() (pgx.Rows, error)         { return nil, nil }
func (noopBatchResults) QueryRow() pgx.Row                { return nil }
func (noopBatchResults) Close() error                     { return nil }

// blockingBatchResults.Close waits on `block` before returning, simulating a
// slow / wedged Postgres so the worker stays in flush. Honours the SendBatch
// context so ShutdownTimeout cancellation propagates the same way the real
// pgx pool would.
type blockingBatchResults struct {
	ctx   context.Context
	block chan struct{}
}

func (b *blockingBatchResults) Exec() (pgconn.CommandTag, error) { return pgconn.CommandTag{}, nil }
func (b *blockingBatchResults) Query() (pgx.Rows, error)         { return nil, nil }
func (b *blockingBatchResults) QueryRow() pgx.Row                { return nil }
func (b *blockingBatchResults) Close() error {
	select {
	case <-b.block:
		return nil
	case <-b.ctx.Done():
		return b.ctx.Err()
	}
}

func makeEvent() core.Event {
	return core.Event{
		EventID:      uuid.Must(uuid.NewV7()),
		EventDate:    time.Now(),
		EventType:    core.EventTypeLogInfo(),
		EventMessage: "test",
		EventCaller:  "postgres_test",
	}
}

// TestNewObserverRejectsBadConfig — config validation runs before any pool is
// created, so each of these returns an error without touching a database.
func TestNewObserverRejectsBadConfig(t *testing.T) {
	withMaxConns := func(n int32) *pgxpool.Config { return &pgxpool.Config{MaxConns: n} }
	cases := []struct {
		name   string
		config Config
	}{
		{"zero CollectionDuration", Config{CollectionMaxSize: 1, Database: withMaxConns(1)}},
		{"negative CollectionDuration", Config{CollectionDuration: -time.Second, CollectionMaxSize: 1, Database: withMaxConns(1)}},
		{"zero CollectionMaxSize", Config{CollectionDuration: time.Second, Database: withMaxConns(1)}},
		{"nil Database", Config{CollectionDuration: time.Second, CollectionMaxSize: 1}},
		{"zero MaxConns", Config{CollectionDuration: time.Second, CollectionMaxSize: 1, Database: withMaxConns(0)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewObserver(tc.config); err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}
}

// TestObserverDoubleClose — Close must be idempotent; the second call is a
// no-op rather than a panic on close-of-closed-channel.
func TestObserverDoubleClose(t *testing.T) {
	obs := newObserver(Config{
		CollectionDuration: 5 * time.Millisecond,
		CollectionMaxSize:  4,
		BatchTimeout:       50 * time.Millisecond,
		ShutdownTimeout:    50 * time.Millisecond,
	}, &stubConn{}, 1)

	obs.Close()
	obs.Close()
}

// TestObserverCloseRace — concurrent Observe + Close must not panic and must
// not data-race under -race. The number-of-events isn't asserted; what matters
// is that the run completes cleanly.
func TestObserverCloseRace(t *testing.T) {
	obs := newObserver(Config{
		CollectionDuration: 5 * time.Millisecond,
		CollectionMaxSize:  64,
		BatchTimeout:       time.Second,
		ShutdownTimeout:    time.Second,
	}, &stubConn{}, 4)

	const producers = 64
	const eventsPerProducer = 200
	var wg sync.WaitGroup
	for range producers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range eventsPerProducer {
				obs.Observe(makeEvent())
			}
		}()
	}

	// Close while producers are still hammering Observe. The select inside
	// Observe must transition cleanly to the "done" case without panicking on
	// observeCh and without racing on the dropped counter.
	time.Sleep(2 * time.Millisecond)
	obs.Close()
	wg.Wait()
}

// TestObserverDropNewestUnderBackpressure — once the channel is full (worker
// wedged in SendBatch), further Observe calls drop the event and bump the
// dropped counter rather than blocking the producer.
func TestObserverDropNewestUnderBackpressure(t *testing.T) {
	unblock := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	conn := &stubConn{
		sendBatch: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			startOnce.Do(func() { close(started) })
			return &blockingBatchResults{ctx: ctx, block: unblock}
		},
	}

	const maxSize uint64 = 2
	obs := newObserver(Config{
		CollectionDuration: time.Hour, // ticker effectively disabled
		CollectionMaxSize:  maxSize,
		BatchTimeout:       time.Hour,
		ShutdownTimeout:    2 * time.Second,
	}, conn, 1)

	// Prime: send exactly CollectionMaxSize events; worker will batch both,
	// call SendBatch, and block on Close() of the BatchResults. After this
	// the channel buffer is empty.
	for range maxSize {
		obs.Observe(makeEvent())
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never called SendBatch")
	}

	// Now flood the channel. Capacity is maxSize, so the first maxSize sends
	// land in the buffer and the rest must be dropped.
	const extra uint64 = 32
	for range extra {
		obs.Observe(makeEvent())
	}

	want := extra - maxSize
	if got := obs.Dropped(); got != want {
		t.Errorf("Dropped() = %d, want %d", got, want)
	}

	// Release the wedged flush, then close cleanly.
	close(unblock)
	done := make(chan struct{})
	go func() { obs.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return")
	}
}

// TestObserverCloseRespectsShutdownTimeout — if SendBatch never completes on
// its own, Close still returns within roughly ShutdownTimeout because the
// shared shutdownCtx cancels in-flight flushes.
func TestObserverCloseRespectsShutdownTimeout(t *testing.T) {
	block := make(chan struct{})
	defer close(block) // make sure the goroutine eventually returns

	conn := &stubConn{
		sendBatch: func(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
			return &blockingBatchResults{ctx: ctx, block: block}
		},
	}

	const shutdown = 150 * time.Millisecond
	obs := newObserver(Config{
		CollectionDuration: time.Hour,
		CollectionMaxSize:  1,
		BatchTimeout:       time.Hour,
		ShutdownTimeout:    shutdown,
	}, conn, 1)

	obs.Observe(makeEvent())

	start := time.Now()
	obs.Close()
	elapsed := time.Since(start)

	if elapsed < shutdown {
		t.Errorf("Close returned before ShutdownTimeout: %v < %v", elapsed, shutdown)
	}
	// Generous upper bound — schedulers, GC, etc.
	if elapsed > shutdown+2*time.Second {
		t.Errorf("Close took much longer than ShutdownTimeout: %v", elapsed)
	}
}

// TestObserverObserveAfterCloseDoesNotPanic — Observe must remain safe after
// Close. With drop-newest semantics the call returns silently; the dropped
// counter may or may not move depending on whether the send race wins.
func TestObserverObserveAfterCloseDoesNotPanic(t *testing.T) {
	obs := newObserver(Config{
		CollectionDuration: 5 * time.Millisecond,
		CollectionMaxSize:  4,
		BatchTimeout:       50 * time.Millisecond,
		ShutdownTimeout:    50 * time.Millisecond,
	}, &stubConn{}, 1)
	obs.Close()

	for range 100 {
		obs.Observe(makeEvent())
	}
}

// After Close every event is dropped and counted. The buffer is deliberately
// left with room to spare: a single select over <-done, the send and a
// default picks randomly among ready cases, so it used to enqueue roughly
// half of these and count none of them.
func TestObserverAfterCloseDropsAndCountsEveryEvent(t *testing.T) {
	const events = 100
	obs := newObserver(Config{
		CollectionDuration: time.Hour, // ticker effectively disabled
		CollectionMaxSize:  events * 2,
		BatchTimeout:       time.Hour,
		ShutdownTimeout:    time.Second,
	}, &stubConn{}, 1)
	obs.Close()

	for range events {
		obs.Observe(makeEvent())
	}

	if got := obs.Dropped(); got != events {
		t.Errorf("Dropped() = %d, want %d", got, events)
	}
	if n := len(obs.observeCh); n != 0 {
		t.Errorf("%d events sit in the channel after Close, want 0", n)
	}
}

// TestObserverPropagatesInstanceSpan — events emitted through a
// core.Context that went through witness.Instance must all land in the DB
// carrying that instance's span_id, flagged as the instance (span_flags & 8).
// That flagged span is what replaced the service_name column: it identifies
// the emitting process, and its span:instance:online event carries the name.
// Integration test, env-gated.
func TestObserverPropagatesInstanceSpan(t *testing.T) {
	dsn := os.Getenv("WITNESS_TEST_DSN")
	if dsn == "" {
		t.Skip("WITNESS_TEST_DSN not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS witness CASCADE"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := admin.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	poolCfg.MaxConns = 2

	obs, err := NewObserver(Config{
		CollectionDuration: time.Hour,
		CollectionMaxSize:  64,
		BatchTimeout:       5 * time.Second,
		ShutdownTimeout:    5 * time.Second,
		Database:           poolCfg,
	})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}

	// Build a real witness Context the way an application would.
	rootCtx, finishInstance := witness.Instance(context.Background(), obs, "test-service", "v0.0.0")
	childCtx, finishSpan := witness.Span(rootCtx, "do-work")
	witness.Info(childCtx, "hello from child span")
	finishSpan()
	finishInstance()

	obs.Close()

	type row struct {
		instance string
		count    int
	}
	rows, err := admin.Query(ctx, `
		SELECT s.span_id::text, count(*)::int
		FROM witness.spans s
		WHERE s.span_flags & 8 <> 0
		GROUP BY s.span_id
		ORDER BY s.span_id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()

	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.instance, &r.count); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	if len(got) != 1 {
		t.Fatalf("expected one instance span, got %+v", got)
	}
	// Instance+Span pair = 4 lifecycle events + 1 Info = 5.
	if got[0].count != 5 {
		t.Errorf("expected 5 events under the instance span, got %d", got[0].count)
	}

	// The instance's name lives on its span:instance:online event.
	var name string
	if err := admin.QueryRow(ctx, `
		SELECT e.event_message
		FROM witness.events e
		JOIN witness.spans s ON s.event_id = e.event_id
		WHERE s.span_id = $1::uuid AND e.event_type = 21`, got[0].instance).Scan(&name); err != nil {
		t.Fatalf("query instance name: %v", err)
	}
	if name != "test-service" {
		t.Errorf("instance name = %q, want %q", name, "test-service")
	}
}

// TestObserverDrainsOnClose — events buffered when Close is called must reach
// the database within ShutdownTimeout. Integration test, env-gated.
func TestObserverDrainsOnClose(t *testing.T) {
	dsn := os.Getenv("WITNESS_TEST_DSN")
	if dsn == "" {
		t.Skip("WITNESS_TEST_DSN not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, "DROP SCHEMA IF EXISTS witness CASCADE"); err != nil {
		t.Fatalf("drop schema: %v", err)
	}
	if _, err := admin.Exec(ctx, schemaSQL); err != nil {
		t.Fatalf("apply schema: %v", err)
	}

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	poolCfg.MaxConns = 2

	const total = 137 // not a multiple of CollectionMaxSize on purpose
	obs, err := NewObserver(Config{
		// Ticker effectively disabled — verifying that buffered events are
		// flushed at shutdown rather than as a side-effect of the periodic
		// timer. Buffer is sized strictly larger than `total` so the
		// producer can dump every event before any worker pickup, which
		// keeps the assertion "no drops, drain on close" unambiguous.
		CollectionDuration: time.Hour,
		CollectionMaxSize:  uint64(total) * 4,
		BatchTimeout:       5 * time.Second,
		ShutdownTimeout:    5 * time.Second,
		Database:           poolCfg,
	})
	if err != nil {
		t.Fatalf("NewObserver: %v", err)
	}

	for range total {
		obs.Observe(makeEvent())
	}

	closed := make(chan struct{})
	go func() { obs.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return within ShutdownTimeout slack")
	}

	if dropped := obs.Dropped(); dropped > 0 {
		t.Errorf("expected zero drops, got %d", dropped)
	}

	var count int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM witness.events").Scan(&count); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if count != total {
		t.Errorf("events in DB: got %d, want %d", count, total)
	}
}
