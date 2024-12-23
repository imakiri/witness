package main

import (
	"context"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/stdlog"
	"github.com/imakiri/witness/record"
)

func main() {
	witness.EnableDebug()

	// create observer instance
	var observer witness.Observer = stdlog.NewObserver()
	// create root span
	var ctx, finish = witness.Instance(context.Background(), observer, "example.simple", "1")
	defer finish()

	var i = 10
	var j = Foo(ctx, i)
	_ = j

	j = Bar(ctx, i)
}

func Foo(ctx context.Context, i int) (j int) {
	ctx, finish := witness.Span(ctx, "Foo", record.Int("i", i))
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
	ctx, finish := witness.Span(ctx, "Bar", record.Int("i", i))
	defer finish(record.Int("j", j))
	return i * i
}
