// Package postgres is an asynchronous, batching witness Observer that ships
// events to Postgres.
//
// Lifecycle and safety properties:
//
//   - Observe never blocks the caller. When the internal channel is full
//     (the DB is slow or producers out-pace MaxConns workers), the event is
//     dropped and the Dropped() counter is incremented. The hot path is
//     racy-close-free: a closed Observer drops new events instead of
//     panicking on send-to-closed-channel.
//   - Close is idempotent. It signals workers to stop accepting new events,
//     gives them Config.ShutdownTimeout to drain whatever is still buffered,
//     then closes the pgxpool. A stuck Postgres cannot wedge Close forever.
//   - Every SendBatch is bounded by Config.BatchTimeout. Workers cannot
//     block on a single batch indefinitely.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/imakiri/witness/core"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	ConnectionTimeout  time.Duration
	CollectionDuration time.Duration
	CollectionMaxSize  uint64
	// BatchTimeout caps how long a single SendBatch may take. Defaults to
	// 2 * CollectionDuration when zero.
	BatchTimeout time.Duration
	// ShutdownTimeout caps how long Close spends draining buffered events
	// before workers are forced to exit. Defaults to 5 * CollectionDuration
	// when zero.
	ShutdownTimeout time.Duration
	Database        *pgxpool.Config
}

// connection is the minimal surface of *pgxpool.Pool that Observer uses.
// Unexported so tests can inject a stub without exposing it in the public API.
type connection interface {
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
	Close()
}

type Observer struct {
	wg             *sync.WaitGroup
	done           chan struct{}
	observeCh      chan core.Event
	config         Config
	connection     connection
	closeOnce      sync.Once
	dropped        atomic.Uint64
	shutdownCtx    context.Context
	shutdownCancel context.CancelFunc
}

func NewObserver(config Config) (*Observer, error) {
	if config.CollectionDuration <= 0 {
		return nil, errors.New("witness/postgres: Config.CollectionDuration must be > 0")
	}
	if config.CollectionMaxSize == 0 {
		return nil, errors.New("witness/postgres: Config.CollectionMaxSize must be > 0")
	}
	if config.Database == nil {
		return nil, errors.New("witness/postgres: Config.Database is required")
	}
	if config.Database.MaxConns <= 0 {
		return nil, errors.New("witness/postgres: Config.Database.MaxConns must be > 0")
	}

	var (
		err    error
		ctx    = context.Background()
		finish func()
	)
	if config.ConnectionTimeout > 0 {
		ctx, finish = context.WithTimeout(ctx, config.ConnectionTimeout)
		defer finish()
	}

	pool, err := pgxpool.NewWithConfig(ctx, config.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping the database: %w", err)
	}

	if err = syncEventTypes(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to sync event types: %w", err)
	}

	return newObserver(config, pool, int(config.Database.MaxConns)), nil
}

// syncEventTypes upserts every registered core.EventType into
// witness.event_types, so SQL can name a type and tell an error from a
// non-error without a hardcoded list.
//
// It runs once, at construction, because that is the only moment the set is
// both complete and stable: MustNewEventType appends to the same registry at
// runtime, and types registered after this call are not written. Register
// custom types in init().
func syncEventTypes(ctx context.Context, conn connection) error {
	var types = core.Events()
	if len(types) == 0 {
		return nil
	}
	var batch = new(pgx.Batch)
	for _, t := range types {
		batch.Queue(`INSERT INTO witness.event_types (event_type, event_type_name, is_error)
                     VALUES ($1, $2, $3)
                     ON CONFLICT (event_type) DO UPDATE
                     SET event_type_name = excluded.event_type_name,
                         is_error        = excluded.is_error`,
			t.Value(), t.String(), t.IsError())
	}
	return conn.SendBatch(ctx, batch).Close()
}

// newObserver builds and starts an Observer with a pre-built connection and
// an explicit worker count. Used by NewObserver and by tests that swap in a
// stub connection.
func newObserver(config Config, conn connection, workers int) *Observer {
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = 2 * config.CollectionDuration
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 5 * config.CollectionDuration
	}

	observer := &Observer{
		wg:         new(sync.WaitGroup),
		done:       make(chan struct{}),
		observeCh:  make(chan core.Event, config.CollectionMaxSize),
		config:     config,
		connection: conn,
	}
	observer.shutdownCtx, observer.shutdownCancel = context.WithCancel(context.Background())

	for range workers {
		observer.wg.Add(1)
		go observer.worker()
	}

	return observer
}

// Close signals every worker to stop accepting new events, waits up to
// Config.ShutdownTimeout for them to drain whatever is still buffered, then
// closes the underlying pgxpool. Safe to call multiple times.
func (o *Observer) Close() {
	o.closeOnce.Do(func() {
		close(o.done)
		// Cap the total drain wall-time. Workers' in-flight SendBatch calls
		// share shutdownCtx as their parent, so canceling it aborts them too.
		// Stop the timer once the workers are done: an unstopped AfterFunc
		// keeps a runtime timer and its closure alive for the whole
		// ShutdownTimeout after a Close that drained promptly.
		timer := time.AfterFunc(o.config.ShutdownTimeout, o.shutdownCancel)
		o.wg.Wait()
		timer.Stop()
		o.shutdownCancel()
		o.connection.Close()
	})
}

// Dropped returns the cumulative number of events Observe refused to enqueue
// because the internal channel was full. Read it on a timer if you want to
// surface backpressure into external metrics.
func (o *Observer) Dropped() uint64 {
	return o.dropped.Load()
}

// Observe is non-blocking. It enqueues event for batching, or drops it
// (incrementing Dropped) when the channel is full or the Observer has been
// closed. Safe to call from any goroutine.
func (o *Observer) Observe(event core.Event) {
	// Two selects, not one: a single select with <-o.done, the send and a
	// default picks *randomly* among the ready cases, so a closed Observer
	// with buffer to spare still enqueued roughly half of what it was given.
	// Checking done on its own makes the shutdown decision deterministic.
	select {
	case <-o.done:
		o.dropped.Add(1)
		return
	default:
	}

	// A Close landing between the two selects still enqueues one event, which
	// drain may or may not reach. Closing that window needs a lock on the hot
	// path, and the event is lost either way — only the Dropped count is
	// approximate, and only for events racing Close.
	select {
	case o.observeCh <- event:
	default:
		o.dropped.Add(1)
	}
}

func (o *Observer) worker() {
	defer o.wg.Done()
	ticker := time.NewTicker(o.config.CollectionDuration)
	defer ticker.Stop()

	for {
		var batch pgx.Batch
		var shuttingDown bool
	collect:
		for range o.config.CollectionMaxSize {
			select {
			case <-o.done:
				shuttingDown = true
				break collect
			case <-ticker.C:
				break collect
			case event := <-o.observeCh:
				queueEvent(&batch, event)
			}
		}
		if batch.Len() > 0 {
			o.flush(&batch)
		}
		if shuttingDown {
			o.drain()
			return
		}
	}
}

// drain non-blockingly empties observeCh after the done signal, batching as
// it goes. Returns when the channel is empty or shutdownCtx is canceled
// (ShutdownTimeout reached).
func (o *Observer) drain() {
	for {
		var batch pgx.Batch
	collect:
		for range o.config.CollectionMaxSize {
			select {
			case <-o.shutdownCtx.Done():
				if batch.Len() > 0 {
					o.flush(&batch)
				}
				return
			case event := <-o.observeCh:
				queueEvent(&batch, event)
			default:
				break collect
			}
		}
		if batch.Len() == 0 {
			return
		}
		o.flush(&batch)
	}
}

func (o *Observer) flush(batch *pgx.Batch) {
	ctx, cancel := context.WithTimeout(o.shutdownCtx, o.config.BatchTimeout)
	defer cancel()
	if err := o.connection.SendBatch(ctx, batch).Close(); err != nil {
		log.Println("witness/postgres: failed to send batch:", err)
	}
}

func queueEvent(batch *pgx.Batch, event core.Event) {
	// Normalize to UTC. The events schema stores event_date as `timestamp
	// without time zone`; mixing wall-clock zones makes Grafana's UTC-based
	// $__timeFilter compare apples to oranges and silently filters
	// everything out.
	batch.Queue(`INSERT INTO witness.events
			(event_id, event_date, event_type, event_message, event_caller)
		VALUES ($1, $2, $3, $4, $5)`,
		event.EventID, event.EventDate.UTC(), event.EventType.Value(), event.EventMessage, event.EventCaller,
	).Exec(func(ct pgconn.CommandTag) error {
		if !ct.Insert() || ct.RowsAffected() != 1 {
			return fmt.Errorf("failed to insert event to the database: %s", ct)
		}
		return nil
	})
	for i, spanID := range event.SpanIDs {
		// SpanFlags is parallel to SpanIDs but may be nil or short on
		// hand-built events; 0 is the schema's "roles unknown".
		var flags core.SpanFlags
		if i < len(event.SpanFlags) {
			flags = event.SpanFlags[i]
		}
		batch.Queue("INSERT INTO witness.spans (event_id, span_id, span_flags) VALUES ($1, $2, $3)",
			event.EventID, spanID, int64(flags)).Exec(func(ct pgconn.CommandTag) error {
			if !ct.Insert() || ct.RowsAffected() != 1 {
				return fmt.Errorf("failed to insert span to the database: %s", ct)
			}
			return nil
		})
	}
	for _, record := range event.Records {
		batch.Queue("INSERT INTO witness.records (event_id, record_key, record_value) VALUES ($1, $2::varchar, $3::varchar)",
			event.EventID, record.AppendKey(nil), record.AppendValue(nil)).Exec(func(ct pgconn.CommandTag) error {
			if !ct.Insert() || ct.RowsAffected() != 1 {
				return fmt.Errorf("failed to insert record to the database: %s", ct)
			}
			return nil
		})
	}
}
