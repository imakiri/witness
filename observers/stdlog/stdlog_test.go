package stdlog_test

import (
	"context"
	"fmt"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/printers"
	"github.com/imakiri/witness/record"
	"github.com/stretchr/testify/require"
	"strings"
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
	printer, err := printers.NewPretty()
	require.NoError(t, err)

	observer, err := stdlog.NewObserver(printer)
	require.NoError(t, err)

	var ctx, finish = witness.Instance(context.Background(), observer, "test_span", "1")
	defer finish()

	ctx, finish2 := witness.Span(ctx, "testSpan")
	defer func() {
		finish2()
	}()

	var _ = foo(ctx, 10, "test string")
}

// Error routing is the one branch in Observe: errors go to errWriter,
// everything else to writer, and errWriter defaults to writer.
func TestErrorRouting(t *testing.T) {
	printer, err := printers.NewPretty()
	require.NoError(t, err)

	for _, tc := range []struct {
		name             string
		split            bool
		wantOut, wantErr string
	}{
		{"default: errors go to the main writer", false, "boom", ""},
		{"split: errors go to the error writer only", true, "", "boom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut strings.Builder
			var options = []stdlog.Option{stdlog.WithWriter(&out)}
			if tc.split {
				options = append(options, stdlog.WithErrorWriter(&errOut))
			}
			observer, err := stdlog.NewObserver(printer, options...)
			require.NoError(t, err)

			ctx, _ := witness.Test(context.Background(), t, observer)
			witness.Error(ctx, "boom", nil)

			require.Contains(t, out.String(), tc.wantOut)
			if tc.wantErr == "" {
				require.Empty(t, errOut.String())
			} else {
				require.Contains(t, errOut.String(), tc.wantErr)
			}
		})
	}
}

func TestNilWriterIsAnError(t *testing.T) {
	printer, err := printers.NewPretty()
	require.NoError(t, err)

	_, err = stdlog.NewObserver(printer, stdlog.WithWriter(nil))
	require.Error(t, err)
	_, err = stdlog.NewObserver(printer, stdlog.WithErrorWriter(nil))
	require.Error(t, err)
}
