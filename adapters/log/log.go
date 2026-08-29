package log

import (
	"bytes"
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"log"
	"time"
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

// Write parses one line produced by the log.Logger built in NewAdapter and
// re-emits it as a witness event. The header is everything before the uuid
// prefix; its last whitespace-separated field is the "file:line:" that
// Llongfile writes, whatever timestamp fields precede it. Errors are
// reported as witness events and the line is reported as consumed — a
// logging adapter must never make the caller's log.Println fail.
func (a *Adapter) Write(p []byte) (n int, err error) {
	var segments = bytes.Split(p, []byte(a.prefix))
	if len(segments) != 2 {
		witness.Error(a.ctx, "invalid segments", nil, record.Int("length", len(segments)))
		return len(p), nil
	}

	var headerSegments = bytes.Fields(segments[0])
	if len(headerSegments) == 0 {
		witness.Error(a.ctx, "invalid header segments", nil, record.Int("length", len(headerSegments)))
		return len(p), nil
	}

	var headerCaller = bytes.TrimSuffix(headerSegments[len(headerSegments)-1], []byte(":"))
	var body = segments[1]
	body = bytes.TrimSuffix(body, []byte("\n"))
	witness.From(a.ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), a.eventType, string(body), string(headerCaller))
	return len(p), nil
}
