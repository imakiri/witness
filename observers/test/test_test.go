package test_test

import (
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/test"
	"github.com/imakiri/witness/record"
	"testing"
)

func Test(t *testing.T) {
	var observer = test.NewObserver(t)
	var ctx = witness.With(t.Context(), witness.NewTestContext(t, observer))

	witness.Info(ctx, "TEST INFO MSG", record.String("foo", "bar"), record.Number("buzz", 17))
}
