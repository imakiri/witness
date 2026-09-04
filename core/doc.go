// Package core is witness's data model and plumbing: the event, the span
// chain, the observer contract, and the machinery that carries a Context
// through a context.Context.
//
// It is separate from the witness package on purpose. Application code calls
// witness.Info, witness.Span, witness.Instance and never names anything here;
// what lives in core is what *observers* implement against and what the
// library itself needs to build an event. Splitting them keeps the witness
// package's compatibility promise down to the couple of dozen functions
// applications actually call.
//
// Import core when you are:
//
//   - writing an Observer — you need Event, Observer, SpanFlags, EventType;
//   - writing a Printer or Appender — Event and PrintFlags;
//   - registering a custom event type — MustNewEventType;
//   - plumbing a Context by hand — From, With, Context.To.
//
// Nothing here emits an event on its own. Context.Observe does, but it takes
// the caller location as a parameter and derives nothing about *why* the
// event exists; the witness package is what turns "log this" into a call to
// it. That is the seam: core knows the shape of an event, witness knows the
// vocabulary.
package core
