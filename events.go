package witness

import "unicode/utf8"

var maxValueLength int

func MaxValueLength() int {
	return maxValueLength
}

func init() {
	for _, event := range events {
		maxValueLength = max(maxValueLength, utf8.RuneCountInString(event.s))
	}
}

type EventType struct {
	i int64
	s string
}

func (e EventType) Value() int64 {
	return e.i
}

func (e EventType) String() string {
	return e.s
}

var events = []EventType{
	EventTypeMetric(),
	EventTypeTraceNew(),
	EventTypeInstanceOnline(),
	EventTypeInstanceOffline(),
	EventTypeSpanStart(),
	EventTypeSpanFinish(),
	EventTypeMessageSentInternal(),
	EventTypeMessageSentExternal(),
	EventTypeMessageReceivedInternal(),
	EventTypeMessageReceivedExternal(),
	EventTypeFunctionCall(),
	EventTypeFunctionReturn(),
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

func EventTypeMetric() EventType {
	return EventType{
		i: 0,
		s: "metric",
	}
}
func EventTypeTraceNew() EventType {
	return EventType{
		i: 1,
		s: "trace:new",
	}
}
func EventTypeInstanceOnline() EventType {
	return EventType{
		i: 10,
		s: "instance:online",
	}
}
func EventTypeInstanceOffline() EventType {
	return EventType{
		i: 11,
		s: "instance:offline",
	}
}
func EventTypeSpanStart() EventType {
	return EventType{
		i: 20,
		s: "span:start",
	}
}
func EventTypeSpanFinish() EventType {
	return EventType{
		i: 21,
		s: "span:finish",
	}
}

// EventTypeMessageSentInternal use when sending message to service within your witness system
func EventTypeMessageSentInternal() EventType {
	return EventType{
		i: 30,
		s: "message:sent:internal",
	}
}

// EventTypeMessageSentExternal use when sending message to service outside your witness system
func EventTypeMessageSentExternal() EventType {
	return EventType{
		i: 31,
		s: "message:sent:external",
	}
}

// EventTypeMessageReceivedInternal use when receiving message from service within your witness system
func EventTypeMessageReceivedInternal() EventType {
	return EventType{
		i: 40,
		s: "message:received:internal",
	}
}

// EventTypeMessageReceivedExternal use when receiving message from service outside your witness system
func EventTypeMessageReceivedExternal() EventType {
	return EventType{
		i: 41,
		s: "message:received:external",
	}
}

// EventTypeFunctionCall use when calling a function
func EventTypeFunctionCall() EventType {
	return EventType{
		i: 50,
		s: "function:call",
	}
}

// EventTypeFunctionReturn use when returning from a function
func EventTypeFunctionReturn() EventType {
	return EventType{
		i: 51,
		s: "function:return",
	}
}

func EventTypeLogInfo() EventType {
	return EventType{
		i: 100,
		s: "log:info",
	}
}

func EventTypeLogWarn() EventType {
	return EventType{
		i: 200,
		s: "log:warn",
	}
}

func EventTypeLogDebug() EventType {
	return EventType{
		i: 300,
		s: "log:debug",
	}
}

// EventTypeLogError generic error
func EventTypeLogError() EventType {
	return EventType{
		i: 400,
		s: "log:error",
	}
}

// EventTypeLogErrorStorage use when system fails to write or read file on disk or other persistent storage
func EventTypeLogErrorStorage() EventType {
	return EventType{
		i: 401,
		s: "log:error:storage",
	}
}

// EventTypeLogErrorNetwork use when system fails to reach another system via network
func EventTypeLogErrorNetwork() EventType {
	return EventType{
		i: 402,
		s: "log:error:network",
	}
}

// EventTypeLogErrorExternal use when system fails due to failure of an external system e.g. invalid ingoing request or response
func EventTypeLogErrorExternal() EventType {
	return EventType{
		i: 403,
		s: "log:error:external",
	}
}

// EventTypeLogErrorInternal use when system fails due to internal error
func EventTypeLogErrorInternal() EventType {
	return EventType{
		i: 404,
		s: "log:error:internal",
	}
}
