package main

import (
	"context"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observer/stdlog"
	"github.com/imakiri/witness/record"
)

func main() {
	var observer witness.Observer = stdlog.NewObserver()
	// observer = provider.NewObserver() // create observer instance
	var ctx = witness.With(context.Background(), witness.Context{
		Debug:    true,
		Version:  "22a1229f", // commit hash or tag
		Observer: observer,
	})

	var i = 10
	var j = Foo(ctx, i)
	_ = j

	j = Bar(ctx, i)
}

func Foo(ctx context.Context, i int) (j int) {
	ctx, finish := witness.SpanFunction(ctx, "Foo", record.Int("i", i))
	defer func() { finish(record.Int("j", j)) }()

	for i < 17 {
		select {
		case <-ctx.Done():
			return i
		default:
			witness.Info(ctx, "Foo: work", record.Int("i", i))
			i *= i
		}
	}
	return i
}

func Bar(ctx context.Context, i int) (j int) {
	ctx, finish := witness.SpanFunction(ctx, "Bar", record.Int("i", i))
	defer finish(record.Int("j", j))
	return i * i
}
