package witness

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
	EventTypeSpanServiceStart(),
	EventTypeSpanServiceFinish(),
	EventTypeSpanWorkerStart(),
	EventTypeSpanWorkerFinish(),
	EventTypeSpanInternalMessageSent(),
	EventTypeSpanInternalMessageReceived(),
	EventTypeSpanExternalMessageSent(),
	EventTypeSpanExternalMessageReceived(),
	EventTypeLogInfo(),
	EventTypeLogWarn(),
	EventTypeLogDebug(),
	EventTypeLogError(),
	EventTypeLogFatal(),
	EventTypeLogErrorDevice(),
	EventTypeLogErrorStorage(),
	EventTypeLogErrorNetwork(),
	EventTypeLogErrorExternal(),
	EventTypeLogErrorInternal(),
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
func EventTypeLogFatal() EventType {
	return EventType{
		e: true,
		i: 14,
		s: "log:fatal",
	}
}

// EventTypeLogErrorInternal use when system fails due to internal error
func EventTypeLogErrorInternal() EventType {
	return EventType{
		e: true,
		i: 100,
		s: "log:error:internal",
	}
}

// EventTypeLogErrorExternal use when system fails due to failure of an external system e.g. invalid ingoing request or response
func EventTypeLogErrorExternal() EventType {
	return EventType{
		e: true,
		i: 101,
		s: "log:error:external",
	}
}

// EventTypeLogErrorDevice use when system fails to communicate with internal device
func EventTypeLogErrorDevice() EventType {
	return EventType{
		e: true,
		i: 102,
		s: "log:error:device",
	}
}

// EventTypeLogErrorStorage use when system fails to write or read file on disk or other persistent storage
func EventTypeLogErrorStorage() EventType {
	return EventType{
		e: true,
		i: 103,
		s: "log:error:storage",
	}
}

// EventTypeLogErrorNetwork use when system fails to reach another system via network
func EventTypeLogErrorNetwork() EventType {
	return EventType{
		e: true,
		i: 104,
		s: "log:error:network",
	}
}

func EventTypeSpanLink() EventType {
	return EventType{
		i: 2,
		s: "span:link",
	}
}
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

func EventTypeSpanServiceStart() EventType {
	return EventType{
		i: 22,
		s: "span:service:start",
	}
}
func EventTypeSpanServiceFinish() EventType {
	return EventType{
		i: -22,
		s: "span:service:finish",
	}
}
func EventTypeSpanWorkerStart() EventType {
	return EventType{
		i: 23,
		s: "span:wait_group:start",
	}
}
func EventTypeSpanWorkerFinish() EventType {
	return EventType{
		i: -23,
		s: "span:wait_group:finish",
	}
}

// EventTypeSpanInternalMessageSent use when sending message to service within your witness system
func EventTypeSpanInternalMessageSent() EventType {
	return EventType{
		i: 24,
		s: "span:internal_message:sent",
	}
}

// EventTypeSpanInternalMessageReceived use when receiving message from service within your witness system
func EventTypeSpanInternalMessageReceived() EventType {
	return EventType{
		i: -24,
		s: "span:internal_message:received",
	}
}

// EventTypeSpanExternalMessageSent use when sending message to service outside your witness system
func EventTypeSpanExternalMessageSent() EventType {
	return EventType{
		i: 25,
		s: "span:external_message:sent",
	}
}

// EventTypeSpanExternalMessageReceived use when receiving message from service outside your witness system
func EventTypeSpanExternalMessageReceived() EventType {
	return EventType{
		i: -25,
		s: "span:external_message:received",
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
