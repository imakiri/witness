package log

import (
	"context"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"testing"
)

func TestLog(t *testing.T) {
	var observer = stdlog.NewObserver()
	var ctx, finish = witness.Instance(context.Background(), observer, "test-log", "1")
	defer finish()

	var eventType = witness.MustNewEventType(4000, "log:adapter:log")
	var log = NewAdapter(ctx, eventType)
	log.Println("some event")
}
