package test_test

import (
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/test"
	"github.com/imakiri/witness/printers"
	"github.com/imakiri/witness/record"
	"github.com/stretchr/testify/require"
	"testing"
)

func Test(t *testing.T) {
	printer, err := printers.NewPretty()
	require.NoError(t, err)

	observer, err := test.NewObserver(t, printer, test.WithFailOnError())
	require.NoError(t, err)

	ctx, finish := witness.Test(t.Context(), t, observer)
	defer finish()

	witness.Info(ctx, "TEST INFO MSG", record.String("foo", "bar"), record.Number("buzz", 17))
	witness.Info(ctx, "TEST INFO MSG", record.String("foo", "bar"), record.Number("buzz", 17))
	//witness.Error(ctx, "TEST INFO MSG", nil, record.String("foo", "bar"), record.Number("buzz", 17))
}
