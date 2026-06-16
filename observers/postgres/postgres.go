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

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
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
	observeCh      chan witness.Event
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

	return newObserver(config, pool, int(config.Database.MaxConns)), nil
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
		observeCh:  make(chan witness.Event, config.CollectionMaxSize),
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
		time.AfterFunc(o.config.ShutdownTimeout, o.shutdownCancel)
		o.wg.Wait()
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
func (o *Observer) Observe(event witness.Event) {
	select {
	case <-o.done:
		return
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

func nullUUID(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func queueEvent(batch *pgx.Batch, event witness.Event) {
	// Normalize to UTC. The events schema stores event_date as `timestamp
	// without time zone`; mixing wall-clock zones makes Grafana's UTC-based
	// $__timeFilter compare apples to oranges and silently filters
	// everything out.
	batch.Queue(`INSERT INTO witness.events
			(event_id, event_date, event_type, event_message, event_caller,
			 trace_id, parent_trace_id, parent_span_id, service_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		event.EventID, event.EventDate.UTC(), event.EventType.Value(), event.EventMessage, event.EventCaller,
		nullUUID(event.TraceID), nullUUID(event.ParentTraceID), nullUUID(event.ParentSpanID),
		nullString(event.ServiceName),
	).Exec(func(ct pgconn.CommandTag) error {
		if !ct.Insert() || ct.RowsAffected() != 1 {
			return fmt.Errorf("failed to insert event to the database: %s", ct)
		}
		return nil
	})
	for _, spanID := range event.SpanIDs {
		batch.Queue("INSERT INTO witness.spans (event_id, span_id) VALUES ($1, $2)",
			event.EventID, spanID).Exec(func(ct pgconn.CommandTag) error {
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
