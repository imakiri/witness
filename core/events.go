package core

import (
	"fmt"
	"unicode/utf8"
)

func CalcMaxEventValueLength(events []EventType) int {
	var length int
	for _, event := range events {
		length = max(length, utf8.RuneCountInString(event.s))
	}
	return length
}

type EventType struct {
	e bool
	i int64
	s string
}

func EventTypesCompare(a, b EventType) int {
	switch {
	case a.Value() > b.Value():
		return 1
	case a.Value() < b.Value():
		return -1
	default:
		return 0
	}
}

// MustNewEventType registers a user-defined event type. The range
// (-1000, +1000) is reserved for built-in types declared in this package;
// user code must use |i| >= 1000.
//
// The type is not an error type: use MustNewErrorEventType for that.
func MustNewEventType(i int64, s string) EventType {
	return mustNewEventType(false, i, s)
}

// MustNewErrorEventType is MustNewEventType for a type that reports a
// failure. IsError returns true for it, which is what routes an event to
// stdlog's error writer and what makes the test observer's WithFailOnError
// fail the test — a custom type registered with MustNewEventType reaches
// neither.
func MustNewErrorEventType(i int64, s string) EventType {
	return mustNewEventType(true, i, s)
}

func mustNewEventType(isError bool, i int64, s string) EventType {
	if -1000 < i && i < 1000 {
		panic("i values in range (-1000,+1000) are reserved for built-in event types")
	}
	if utf8.RuneCountInString(s) > 127 {
		panic("s values cannot exceed 128 characters")
	}
	// EventTypesCompare orders on i alone, so a duplicate i would make the
	// binary search in a type filter (stdlog's WithTypes) match whichever of
	// the two it happened to land on. Refuse the collision at registration.
	for _, registered := range events {
		if registered.i == i {
			panic(fmt.Sprintf("event type i=%d is already registered as %q, cannot register it as %q",
				i, registered.s, s))
		}
	}
	var eventType = EventType{
		e: isError,
		i: i,
		s: s,
	}
	events = append(events, eventType)
	return eventType
}

func (e EventType) Value() int64 {
	return e.i
}

func (e EventType) String() string {
	return e.s
}

func (e EventType) Append(dst []byte) []byte {
	return append(dst, e.s...)
}

func (e EventType) IsError() bool {
	return e.e
}

// events holds every built-in EventType. Observers that filter by type
// (stdlog's WithTypes, printers.Pretty's column widths) read it through
// Events(), so a type missing here is a type those observers silently drop.
// Keep it exhaustive: every EventType* constructor below must appear.
var events = []EventType{
	EventTypeLog(),
	EventTypeSpanLink(),
	EventTypeMetric(),
	EventTypeSpanStart(),
	EventTypeSpanFinish(),
	EventTypeSpanInstanceOnline(),
	EventTypeSpanInstanceOffline(),
	EventTypeSpanMessageSent(),
	EventTypeSpanMessageReceived(),
	EventTypeLogInfo(),
	EventTypeLogWarn(),
	EventTypeLogDebug(),
	EventTypeLogError(),
	EventTypeLogFatal(),
	EventTypeLogPanic(),
	EventTypeMetricGauge(),
	EventTypeMetricCounter(),
	EventTypeMetricHistogram(),
}

func Events() []EventType {
	var es = make([]EventType, len(events))
	copy(es, events)
	return es
}

func EventTypeLog() EventType {
	return EventType{
		i: 1,
		s: "log",
	}
}

func EventTypeLogDebug() EventType {
	return EventType{
		i: 10,
		s: "log:debug",
	}
}
func EventTypeLogInfo() EventType {
	return EventType{
		i: 11,
		s: "log:info",
	}
}
func EventTypeLogWarn() EventType {
	return EventType{
		i: 12,
		s: "log:warn",
	}
}
func EventTypeLogError() EventType {
	return EventType{
		e: true,
		i: 13,
		s: "log:error",
	}
}

// EventTypeLogFatal use when the process cannot continue. Recording it does
// not stop anything — witness never exits or panics on its own behalf; the
// caller does that after the event is emitted.
func EventTypeLogFatal() EventType {
	return EventType{
		e: true,
		i: 14,
		s: "log:fatal",
	}
}

// EventTypeLogPanic use from a recover() to record a panic that was caught.
//
// There is deliberately no taxonomy of error causes here — the types
// log:error:{internal,external,device,storage,network} existed and were
// removed. Nothing in witness or in SQL ever branched on which one an event
// carried (every consumer reads EventType.IsError()), the boundaries between
// them were a guess each program draws differently, and the same fact says
// more as a record: record.String("kind", "postgres") is open-ended where a
// five-value enum is not. A program that wants a closed set of its own has
// MustNewErrorEventType, whose |i| >= 1000 range exists for exactly this.
func EventTypeLogPanic() EventType {
	return EventType{
		e: true,
		i: 15,
		s: "log:panic",
	}
}

func EventTypeSpanLink() EventType {
	return EventType{
		i: 2,
		s: "span:link",
	}
}

// EventTypeSpanStart opens a span; EventTypeSpanFinish closes it.
//
// There is one kind of span. span:service:start/finish (22) and
// span:wait_group:start/finish (23, emitted by a witness.Worker) were
// removed: nothing branched on them — otlp routed both through the same
// start/finish handlers, the SQL views matched the whole 20..23 range — so
// the only difference was the string in the output, which a span's *name*
// already carries. "service" was actively misleading on top of that: in this
// model a service is the instance span at the head of the chain, the one
// flagged SpanFlagInstance whose span:instance:online event names it, not a
// child span somewhere below.
//
// A program that wants machine-readable span kinds registers its own paired
// type with MustNewEventType (|i| >= 1000, +/- for start/finish); the views
// accept that range and witness.event_types now gives it a name in SQL.
func EventTypeSpanStart() EventType {
	return EventType{
		i: 20,
		s: "span:general:start",
	}
}
func EventTypeSpanFinish() EventType {
	return EventType{
		i: -20,
		s: "span:general:finish",
	}
}
func EventTypeSpanInstanceOnline() EventType {
	return EventType{
		i: 21,
		s: "span:instance:online",
	}
}
func EventTypeSpanInstanceOffline() EventType {
	return EventType{
		i: -21,
		s: "span:instance:offline",
	}
}

// EventTypeSpanMessageSent use when handing a message off: the sender's half
// of a link, on a msgID the sender put in the envelope.
//
// There is no internal/external variant. The pair used to be doubled (24/25)
// depending on whether the peer was inside your witness system, and nothing
// ever read the difference — every view treats the two as one set and the
// otlp observer routes them through one branch. Where the distinction
// matters (a peer outside the system never emits its half, so a link with no
// receiving side is expected rather than a lost message) say so in a record.
func EventTypeSpanMessageSent() EventType {
	return EventType{
		i: 24,
		s: "span:message:sent",
	}
}

// EventTypeSpanMessageReceived use when taking a message off a carrier: the
// receiver's half, on the msgID that arrived with it.
func EventTypeSpanMessageReceived() EventType {
	return EventType{
		i: -24,
		s: "span:message:received",
	}
}

func EventTypeMetric() EventType {
	return EventType{
		i: 3,
		s: "metric",
	}
}

func EventTypeMetricGauge() EventType {
	return EventType{
		i: 30,
		s: "metric:gauge",
	}
}
func EventTypeMetricCounter() EventType {
	return EventType{
		i: 31,
		s: "metric:counter",
	}
}
func EventTypeMetricHistogram() EventType {
	return EventType{
		i: 32,
		s: "metric:histogram",
	}
}
