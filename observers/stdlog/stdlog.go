package stdlog

import (
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"os"
	"slices"
	"time"
)

type Observer struct {
	useStdErr      bool
	types          []witness.EventType
	printerOptions []record.PrinterOption
	printer        *record.Printer
}

type Option func(o *Observer)

func WithTypes(types []witness.EventType) Option {
	return func(o *Observer) {
		o.types = types
	}
}

func WithUsingStdErr() Option {
	return func(o *Observer) {
		o.useStdErr = true
	}
}

func WithPrinterOptions(options ...record.PrinterOption) Option {
	return func(o *Observer) {
		o.printerOptions = options
	}
}

func NewObserver(options ...Option) *Observer {
	var o = new(Observer)
	for _, opt := range options {
		opt(o)
	}

	if o.types == nil {
		o.types = witness.Events()
	}
	slices.SortFunc(o.types, witness.EventTypesCompare)

	o.printer = record.NewPrinter(append([]record.PrinterOption{
		record.PrinterWithMaxEventTypeLength(witness.CalcMaxEventValueLength(o.types)),
		//record.PrinterWithPrintLF(false),
	}, o.printerOptions...)...)

	return o
}

func (o *Observer) appendTime(b []byte, t time.Time) []byte {
	return t.AppendFormat(b, "2006-01-02T15:04:05.000000000Z07:00")
}

func (o *Observer) Observe(event witness.Event) {
	if _, found := slices.BinarySearchFunc(o.types, event.EventType, witness.EventTypesCompare); !found {
		return
	}
	if event.EventType.IsError() && o.useStdErr {
		o.printer.Print(os.Stderr, event)
	}
	o.printer.Print(os.Stdout, event)
}
