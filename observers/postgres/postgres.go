package postgres

import (
	"context"
	"fmt"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"sync"
	"time"
)

type Event struct {
	SpanIDs     []uuid.UUID
	EventID     uuid.UUID
	EventDate   time.Time
	EventType   witness.EventType
	EventName   string
	EventCaller string
	Records     []witness.Record
}

type Config struct {
	ConnectionTimeout  time.Duration
	CollectionDuration time.Duration
	CollectionMaxSize  uint64
	Database           *pgxpool.Config
}

type Observer struct {
	wg         *sync.WaitGroup
	done       chan struct{}
	observeCh  chan Event
	config     Config
	connection *pgxpool.Pool
}

func NewObserver(config Config) (*Observer, error) {
	var err error
	var observer = new(Observer)
	observer.config = config
	observer.wg = new(sync.WaitGroup)
	observer.done = make(chan struct{})
	observer.observeCh = make(chan Event, config.CollectionMaxSize)

	var finish func()
	var ctx = context.Background()
	if config.ConnectionTimeout > 0 {
		ctx, finish = context.WithTimeout(ctx, config.ConnectionTimeout)
		defer finish()
	}

	observer.connection, err = pgxpool.NewWithConfig(ctx, config.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to the database: %w", err)
	}

	if err = observer.connection.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping the database: %w", err)
	}

	for range config.Database.MaxConns {
		observer.wg.Go(observer.worker)
	}

	return observer, nil
}

func (o *Observer) Close() {
	close(o.done)
	o.wg.Wait()
	close(o.observeCh)
}

func (o *Observer) worker() {
	var done = false
	var maxSize = o.config.CollectionMaxSize
	var ticker = time.Tick(o.config.CollectionDuration)
	for !done {
		var batch pgx.Batch
	collection:
		for range maxSize {
			select {
			case <-o.done:
				done = false
				break collection
			case <-ticker:
				break collection
			default:
				var event = <-o.observeCh
				batch.Queue("INSERT INTO witness.events (event_id, event_date, event_type, event_name, event_caller) VALUES ($1, $2, $3, $4, $5)",
					event.EventID, event.EventDate, event.EventType, event.EventName, event.EventCaller).Exec(func(ct pgconn.CommandTag) error {
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
							return fmt.Errorf("failed to insert span to the database: %s", ct)
						}
						return nil
					})
				}
			}
		}
		o.wg.Go(func() {
			if err := o.connection.SendBatch(context.Background(), &batch).Close(); err != nil {
				log.Println("failed to send batch to the database:", err.Error())
			}
		})
	}
}

func (o *Observer) Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {
	select {
	case <-o.done:
		return
	case o.observeCh <- Event{
		SpanIDs:     spanIDs,
		EventID:     eventID,
		EventDate:   eventDate,
		EventType:   eventType,
		EventName:   eventName,
		EventCaller: eventCaller,
		Records:     records,
	}:
		// OK
	}
}
