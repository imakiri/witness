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

func (o *Observer) Observe(ctx context.Context, spanID uuid.UUID, eventType witness.EventType, eventName string, eventValue []byte, eventCaller string, records ...witness.Record) {
	//TODO implement me
	panic("implement me")
}
