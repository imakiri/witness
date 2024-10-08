package stdlog

import (
	"context"
	"fmt"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"testing"
)

func foo(ctx context.Context, i int, s string) (re string) {
	ctx, finish := witness.Span(ctx, "service.foo", record.Int("i", i), record.String("s", s))
	defer finish(record.String("result", re))

	re = fmt.Sprintf("%d: %s", i, s)
	witness.Warn(ctx, "strange result", record.String("result", re))
	return
}

func TestSpan(t *testing.T) {
	var observer = NewObserver()
	var ctx = witness.With(context.Background(), observer)

	ctx, finish := witness.Span(ctx, "testSpan")
	defer finish()

	var _ = foo(ctx, 10, "test string")
}
