package log

import (
	"bytes"
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"log"
)

type Adapter struct {
	ctx       context.Context
	prefix    string
	eventType witness.EventType
}

func NewAdapter(ctx context.Context, eventType witness.EventType) *log.Logger {
	var adapter = new(Adapter)
	adapter.ctx = ctx
	adapter.prefix = uuid.Must(uuid.NewV7()).String()
	adapter.eventType = eventType
	return log.New(adapter, adapter.prefix, log.Llongfile|log.Lmicroseconds|log.Lmsgprefix)
}

func (a *Adapter) Write(p []byte) (n int, err error) {
	var segments = bytes.Split(p, []byte(a.prefix))
	if len(segments) != 2 {
		witness.Error(a.ctx, "invalid segments", nil, record.Int("length", len(segments)))
		return 0, err
	}

	var header = segments[0]
	var headerSegments = bytes.Split(header, []byte(" "))
	if len(headerSegments) != 3 {
		witness.Error(a.ctx, "invalid header segments", nil, record.Int("length", len(segments)))
		return 0, err
	}

	//var headerDate = headerSegments[0]
	//var headerTime = headerSegments[1]
	var headerCaller = headerSegments[2]
	var body = segments[1]
	body = bytes.TrimSuffix(body, []byte("\n"))
	witness.From(a.ctx).Observe(a.ctx, a.eventType, string(body), string(headerCaller))
	return len(p), nil
}
