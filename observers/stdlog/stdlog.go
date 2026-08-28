package stdlog

import (
	"fmt"
	"github.com/imakiri/witness"
	"io"
	"os"
	"slices"
)

type Observer struct {
	useStdErr bool
	types     []witness.EventType
	flags     witness.PrintFlags
	printer   witness.Printer
}

type Option func(o *Observer) error

func WithTypes(types []witness.EventType) Option {
	return func(o *Observer) error {
		o.types = types
		return nil
	}
}

func WithUsingStdErr() Option {
	return func(o *Observer) error {
		o.useStdErr = true
		return nil
	}
}

func WithFlags(flags ...witness.PrintFlags) Option {
	return func(o *Observer) error {
		o.flags = 0
		for _, f := range flags {
			o.flags |= f
		}
		return nil
	}
}

func NewObserver(printer witness.Printer, options ...Option) (*Observer, error) {
	var o = &Observer{
		printer: printer,
		flags:   ^witness.PrintFlags(0),
	}

	for i, option := range options {
		if err := option(o); err != nil {
			return nil, fmt.Errorf("failed at option %d: %w", i, err)
		}
	}

	if o.types == nil {
		o.types = witness.Events()
	}
	slices.SortFunc(o.types, witness.EventTypesCompare)
	return o, nil
}

func (o *Observer) Observe(event witness.Event) {
	if _, found := slices.BinarySearchFunc(o.types, event.EventType, witness.EventTypesCompare); !found {
		return
	}
	var w io.Writer = os.Stdout
	if o.useStdErr && event.EventType.IsError() {
		w = io.MultiWriter(os.Stdout, os.Stderr)
	}
	o.printer.Print(w, event, o.flags)
}
