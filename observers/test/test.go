package test

import (
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
)

type T interface {
	Fail()
	Helper()
	Logf(format string, args ...any)
}

type Observer struct {
	t              T
	foe            bool
	printerOptions []record.PrettyOption
	printer        *record.Pretty
}

type Option func(o *Observer)

func WithFailOnError() Option {
	return func(o *Observer) {
		o.foe = true
	}
}

func WithPrinterOptions(options ...record.PrettyOption) Option {
	return func(o *Observer) {
		o.printerOptions = options
	}
}

func NewObserver(t T, options ...Option) *Observer {
	var o = new(Observer)
	for _, option := range options {
		option(o)
	}
	o.t = t
	o.printer = record.NewPretty(o.printerOptions...)
	return o
}

func (o *Observer) Observe(event witness.Event) {
	o.t.Helper()
	o.t.Logf("%s", o.printer.Append(nil, event))
	if event.EventType.IsError() && o.foe {
		o.t.Fail()
	}
}
