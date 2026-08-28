package log

import (
	"context"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/printers"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLog(t *testing.T) {
	printer, err := printers.NewPretty()
	require.NoError(t, err)

	observer, err := stdlog.NewObserver(printer)
	require.NoError(t, err)

	var ctx, finish = witness.Instance(context.Background(), observer, "test-log", "1")
	defer finish()

	var eventType = witness.MustNewEventType(4000, "log:adapter:log")
	var log = NewAdapter(ctx, eventType)
	log.Println("some event")
}
