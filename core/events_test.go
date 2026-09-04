package core

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// A built-in EventType missing from the events slice is silently dropped by
// every consumer of Events() — stdlog's WithTypes filter, printers.Pretty's
// column widths. Adding an EventType* constructor without registering it is
// the bug this guards.
func TestEventsRegistryIsExhaustive(t *testing.T) {
	var all = []EventType{
		EventTypeLog(), EventTypeSpanLink(), EventTypeMetric(),
		EventTypeLogDebug(), EventTypeLogInfo(), EventTypeLogWarn(),
		EventTypeLogError(), EventTypeLogFatal(), EventTypeLogPanic(),
		EventTypeSpanStart(), EventTypeSpanFinish(),
		EventTypeSpanInstanceOnline(), EventTypeSpanInstanceOffline(),
		EventTypeSpanMessageSent(), EventTypeSpanMessageReceived(),
		EventTypeMetricGauge(), EventTypeMetricCounter(), EventTypeMetricHistogram(),
	}
	var registered = Events()
	for _, e := range all {
		require.True(t, slices.Contains(registered, e), "%s (%d) is not in the events slice", e, e.Value())
	}
	// Only built-ins are this test's business — MustNewEventType appends to
	// the same slice, and other tests in this package register custom types.
	var builtins = slices.DeleteFunc(registered, func(e EventType) bool {
		return e.Value() <= -1000 || e.Value() >= 1000
	})
	require.Len(t, builtins, len(all), "events holds a built-in type not listed in this test")
}

func TestMustNewErrorEventType(t *testing.T) {
	require.False(t, MustNewEventType(9000, "custom:plain").IsError())
	require.True(t, MustNewErrorEventType(9001, "custom:boom").IsError())
	var registered = MustNewErrorEventType(9002, "custom:registered")
	require.True(t, slices.Contains(Events(), registered))
}

func TestDuplicateEventTypeValuePanics(t *testing.T) {
	MustNewEventType(9100, "custom:first")
	require.Panics(t, func() { MustNewEventType(9100, "custom:second") })
	require.Panics(t, func() { MustNewErrorEventType(9100, "custom:third") })
	// A built-in value is just as taken as a custom one — though the reserved
	// range rejects it first.
	require.Panics(t, func() { MustNewEventType(11, "custom:log_info") })
}
