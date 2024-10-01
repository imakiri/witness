package witness

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
