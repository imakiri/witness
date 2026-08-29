package log

import (
	"context"
	"fmt"
	"runtime"
	"strings"

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

type captureObserver struct{ events []witness.Event }

func (c *captureObserver) Observe(event witness.Event) { c.events = append(c.events, event) }

// The adapter's whole job is splitting the log.Logger header off the body.
// Assert on both halves — the header layout depends on the exact flag set
// passed to log.New in NewAdapter.
func TestAdapterParsesHeader(t *testing.T) {
	var observer = new(captureObserver)
	var ctx, finish = witness.Instance(context.Background(), observer, "test-log", "1")
	defer finish()

	var eventType = witness.MustNewEventType(4001, "log:adapter:header")
	var logger = NewAdapter(ctx, eventType)
	logger.Println("some event")
	_, _, line, _ := runtime.Caller(0)

	require.Len(t, observer.events, 2)
	var event = observer.events[1]
	require.Equal(t, eventType, event.EventType)
	require.Equal(t, "some event", event.EventMessage)
	require.True(t, strings.HasSuffix(event.EventCaller, fmt.Sprintf("log_test.go:%d", line-1)), event.EventCaller)
}
