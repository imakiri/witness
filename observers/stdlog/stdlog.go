package stdlog

import (
	"errors"
	"fmt"
	"github.com/imakiri/witness/core"
	"io"
	"os"
	"slices"
)

type Observer struct {
	writer    io.Writer
	errWriter io.Writer
	types     []core.EventType
	flags     core.PrintFlags
	printer   core.Printer
}

type Option func(o *Observer) error

// WithTypes restricts the observer to the listed event types; everything
// else is dropped. Without it no filtering happens at all — including for
// types registered via MustNewEventType after this observer was built.
func WithTypes(types []core.EventType) Option {
	return func(o *Observer) error {
		o.types = slices.SortedFunc(slices.Values(types), core.EventTypesCompare)
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

func WithFlags(flags ...core.PrintFlags) Option {
	return func(o *Observer) error {
		o.flags = core.PrintNone
		for _, f := range flags {
			o.flags |= f
		}
		return nil
	}
}

func NewObserver(printer core.Printer, options ...Option) (*Observer, error) {
	var o = &Observer{
		printer: printer,
		flags:   core.PrintAll,
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

func (o *Observer) Observe(event core.Event) {
	if o.types != nil {
		if _, found := slices.BinarySearchFunc(o.types, event.EventType, core.EventTypesCompare); !found {
			return
		}
	}
	var w = o.writer
	if event.EventType.IsError() {
		w = o.errWriter
	}
	o.printer.Print(w, event, o.flags)
}
