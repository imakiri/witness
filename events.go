package witness

import "unicode/utf8"

var maxEventValueLength int

func MaxEventValueLength() int {
	return maxEventValueLength
}

func calcMaxEventValueLength() {
	for _, event := range events {
		maxEventValueLength = max(maxEventValueLength, utf8.RuneCountInString(event.s))
	}
}

func init() {
	calcMaxEventValueLength()
}

type EventType struct {
	i int64
	s string
}

// MustNewEventType registers a user-defined event type. The range
// (-1000, +1000) is reserved for built-in types declared in this package;
// user code must use |i| >= 1000.
func MustNewEventType(i int64, s string) EventType {
	if -1000 < i && i < 1000 {
		panic("i values in range (-1000,+1000) are reserved for built-in event types")
	}
	if utf8.RuneCountInString(s) > 127 {
		panic("s values cannot exceed 128 characters")
	}
	var eventType = EventType{
		i: i,
		s: s,
	}
	events = append(events, eventType)
	calcMaxEventValueLength()
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

var events = []EventType{
	EventTypeMetric(),
	//EventTypeLog(),
	//EventTypeLink(),
	EventTypeSpanStart(),
	EventTypeSpanFinish(),
	EventTypeSpanInstanceOnline(),
	EventTypeSpanInstanceOffline(),
	EventTypeSpanServiceStart(),
	EventTypeSpanServiceFinish(),
	EventTypeSpanInternalMessageSent(),
	EventTypeSpanInternalMessageReceived(),
	EventTypeSpanExternalMessageSent(),
	EventTypeSpanExternalMessageReceived(),
	EventTypeLogInfo(),
	EventTypeLogWarn(),
	EventTypeLogDebug(),
	EventTypeLogError(),
	EventTypeLogErrorStorage(),
	EventTypeLogErrorNetwork(),
	EventTypeLogErrorExternal(),
	EventTypeLogErrorInternal(),
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
		i: 13,
		s: "log:error",
	}
}
func EventTypeLogFatal() EventType {
	return EventType{
		i: 14,
		s: "log:fatal",
	}
}

// EventTypeLogErrorInternal use when system fails due to internal error
func EventTypeLogErrorInternal() EventType {
	return EventType{
		i: 100,
		s: "log:error:internal",
	}
}

// EventTypeLogErrorExternal use when system fails due to failure of an external system e.g. invalid ingoing request or response
func EventTypeLogErrorExternal() EventType {
	return EventType{
		i: 101,
		s: "log:error:external",
	}
}

// EventTypeLogErrorDevice use when system fails to communicate with internal device
func EventTypeLogErrorDevice() EventType {
	return EventType{
		i: 102,
		s: "log:error:device",
	}
}

// EventTypeLogErrorStorage use when system fails to write or read file on disk or other persistent storage
func EventTypeLogErrorStorage() EventType {
	return EventType{
		i: 103,
		s: "log:error:storage",
	}
}

// EventTypeLogErrorNetwork use when system fails to reach another system via network
func EventTypeLogErrorNetwork() EventType {
	return EventType{
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
		s: "span:message_external:sent",
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
