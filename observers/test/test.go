package test

import (
	"fmt"
	"github.com/imakiri/witness/core"
)

type T interface {
	Helper()
	Logf(format string, args ...any)
	Fail()
}

type Observer struct {
	t       T
	foe     bool
	flags   core.PrintFlags
	printer core.Appender
}

type Option func(o *Observer) error

func WithFailOnError() Option {
	return func(o *Observer) error {
		o.foe = true
		return nil
	}
}

func WithFlags(flags ...core.PrintFlags) Option {
	return func(o *Observer) error {
		o.flags = core.PrintNone
		for _, f := range flags {
			o.flags |= f
		}
		return nil
	}
}

func NewObserver(t T, printer core.Appender, options ...Option) (*Observer, error) {
	var o = &Observer{
		t:       t,
		flags:   core.PrintAll,
		printer: printer,
	}

	for i, option := range options {
		if err := option(o); err != nil {
			return nil, fmt.Errorf("failed at option %d: %w", i, err)
		}
	}

	return o, nil
}

func (o *Observer) Observe(event core.Event) {
	o.t.Helper()
	o.t.Logf("%s", o.printer.Append(nil, event, o.flags))
	if event.EventType.IsError() && o.foe {
		o.t.Fail()
	}
}
