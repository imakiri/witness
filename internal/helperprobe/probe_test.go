// Package helperprobe exists for one test in the parent module, which runs
// `go test -v` on this package and reads the file:line each log line is
// attributed to.
//
// That attribution is the only observable effect of testing.TB.Helper, and
// it cannot be asserted in-process: testing.TB cannot be implemented outside
// the testing package, and the helper set is not exposed. So the check is an
// exec — see TestHelperAttribution in the root module.
package helperprobe

import (
	"context"
	"testing"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
)

// probeObserver logs every event through the test, marking itself a helper
// exactly as observers/test does.
type probeObserver struct{ t *testing.T }

func (o probeObserver) Observe(event core.Event) {
	o.t.Helper()
	o.t.Logf("PROBE %s", event.EventType)
}

// TestAttributionProbe emits one event of every shape a witness call can
// take. It always passes; what matters is where its log lines point.
func TestAttributionProbe(t *testing.T) {
	ctx, finishTest := witness.Test(context.Background(), t, probeObserver{t: t})

	witness.Info(ctx, "point event")
	witness.Error(ctx, "error event", nil)
	witness.Count(ctx, "metric", 1)

	spanCtx, finishSpan := witness.Span(ctx, "span")
	witness.Sent(spanCtx, core.From(spanCtx).CurrentSpanID(), "hand-off")
	finishSpan()

	handleCtx, finishHandle := witness.Handle(ctx, core.From(ctx).InstanceSpanID(), "handled")
	finishHandle()
	_ = handleCtx

	finishTest()
}
