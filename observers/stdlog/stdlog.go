package stdlog

import (
	"errors"
	"fmt"
	"github.com/imakiri/witness"
	"io"
	"os"
	"slices"
)

type Observer struct {
	writer    io.Writer
	errWriter io.Writer
	types     []witness.EventType
	flags     witness.PrintFlags
	printer   witness.Printer
}

type Option func(o *Observer) error

// WithTypes restricts the observer to the listed event types; everything
// else is dropped. Without it no filtering happens at all — including for
// types registered via MustNewEventType after this observer was built.
func WithTypes(types []witness.EventType) Option {
	return func(o *Observer) error {
		o.types = slices.SortedFunc(slices.Values(types), witness.EventTypesCompare)
		return nil
	}
}

// WithWriter sets the destination for non-error events. Defaults to
// os.Stdout. A nil writer is an error rather than a silent discard —
// io.Discard says that on purpose.
func WithWriter(w io.Writer) Option {
	return func(o *Observer) error {
		if w == nil {
			return errors.New("stdlog: WithWriter: nil writer")
		}
		o.writer = w
		return nil
	}
}

// WithErrorWriter sets the destination for error events (EventType.IsError).
// Defaults to whatever WithWriter set, i.e. os.Stdout — errors are not split
// out unless you ask. Pass os.Stderr to separate them, or
// io.MultiWriter(os.Stdout, os.Stderr) to get both.
func WithErrorWriter(w io.Writer) Option {
	return func(o *Observer) error {
		if w == nil {
			return errors.New("stdlog: WithErrorWriter: nil writer")
		}
		o.errWriter = w
		return nil
	}
}

// WithUsingStdErr routes error events to stderr.
//
// Deprecated: use WithErrorWriter(os.Stderr). It used to write errors to
// stdout *and* stderr, printing every error twice; it is now stderr only.
// For the old behaviour pass io.MultiWriter(os.Stdout, os.Stderr).
func WithUsingStdErr() Option {
	return WithErrorWriter(os.Stderr)
}

func WithFlags(flags ...witness.PrintFlags) Option {
	return func(o *Observer) error {
		o.flags = witness.PrintNone
		for _, f := range flags {
			o.flags |= f
		}
		return nil
	}
}

func NewObserver(printer witness.Printer, options ...Option) (*Observer, error) {
	var o = &Observer{
		printer: printer,
		flags:   witness.PrintAll,
		writer:  os.Stdout,
	}

	for i, option := range options {
		if err := option(o); err != nil {
			return nil, fmt.Errorf("failed at option %d: %w", i, err)
		}
	}

	if o.errWriter == nil {
		o.errWriter = o.writer
	}
	return o, nil
}

func (o *Observer) Observe(event witness.Event) {
	if o.types != nil {
		if _, found := slices.BinarySearchFunc(o.types, event.EventType, witness.EventTypesCompare); !found {
			return
		}
	}
	var w = o.writer
	if event.EventType.IsError() {
		w = o.errWriter
	}
	o.printer.Print(w, event, o.flags)
}
