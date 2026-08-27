package test

import (
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/record"
	"testing"
	"time"
)

type Observer struct {
	t              *testing.T
	foe            bool
	printerOptions []record.PrinterOption
	printer        *record.Printer
}

type Option func(o *Observer)

func WithFailOnError() Option {
	return func(o *Observer) {
		o.foe = true
	}
}

func WithPrinterOptions(options ...record.PrinterOption) Option {
	return func(o *Observer) {
		o.printerOptions = options
	}
}

func NewObserver(t *testing.T, options ...Option) *Observer {
	var o = new(Observer)
	for _, option := range options {
		option(o)
	}
	o.t = t
	o.printer = record.NewPrinter(o.printerOptions...)
	return o
}

func (o *Observer) Observe(spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time, eventType witness.EventType, eventMessage string, eventCaller string, records ...witness.Record) {
	o.t.Helper()
	if eventType.IsError() && o.foe {
		o.t.Errorf("%s", o.printer.Append(nil, spanIDs, eventID, eventDate, eventType, eventMessage, eventCaller, records...))
	}
	o.t.Logf("%s", o.printer.Append(nil, spanIDs, eventID, eventDate, eventType, eventMessage, eventCaller, records...))
}
