package postgres

import (
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Observer struct {
	connection pgxpool.Pool
}

func (o *Observer) Observe(ctx context.Context, spanIDs []uuid.UUID, eventType witness.EventType, eventName string, eventCaller string, records ...witness.Record) {
	//TODO implement me
	panic("implement me")
}
