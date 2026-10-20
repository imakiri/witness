package dynatrace

import (
	"context"
	"strconv"
	"sync"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/otlp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// metrics maps metric events onto OTel instruments, the same event shape the
// prometheus observer reads:
//
//	event_message  -> metric key
//	record "value" -> the delta (counter), observation (histogram) or
//	                  current value (gauge); an event without one is dropped
//	other records  -> dimensions
//
// Unlike prometheus nothing is declared upfront: an instrument is created on
// the first event of its name. Every other record becomes a dimension, so
// keep high-cardinality values (ids, amounts) out of metric events.
type metrics struct {
	meter metric.Meter
	mu    sync.Mutex
	inst  map[string]any // event type + "/" + name -> instrument
}

func newMetrics(mp *sdkmetric.MeterProvider) *metrics {
	return &metrics{meter: mp.Meter(otlp.TracerName), inst: map[string]any{}}
}

func (m *metrics) observe(event witness.Event) {
	var value float64
	var found bool
	attrs := make([]attribute.KeyValue, 0, len(event.Records))
	for _, r := range event.Records {
		if r.KeyEqual("value") {
			v, err := strconv.ParseFloat(string(r.AppendValue(nil)), 64)
			if err != nil {
				return
			}
			value, found = v, true
			continue
		}
		attrs = append(attrs, attribute.String(string(r.AppendKey(nil)), string(r.AppendValue(nil))))
	}
	if !found || event.EventMessage == "" {
		return
	}
	opt := metric.WithAttributes(attrs...)
	ctx := context.Background()

	switch event.EventType {
	case witness.EventTypeMetricCounter():
		if c, ok := m.instrument(event).(metric.Float64Counter); ok {
			c.Add(ctx, value, opt)
		}
	case witness.EventTypeMetricHistogram():
		if h, ok := m.instrument(event).(metric.Float64Histogram); ok {
			h.Record(ctx, value, opt)
		}
	case witness.EventTypeMetricGauge(), witness.EventTypeMetric():
		if g, ok := m.instrument(event).(metric.Float64Gauge); ok {
			g.Record(ctx, value, opt)
		}
	}
}

func (m *metrics) instrument(event witness.Event) any {
	key := event.EventType.String() + "/" + event.EventMessage
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.inst[key]; ok {
		return i
	}
	var i any
	var err error
	switch event.EventType {
	case witness.EventTypeMetricCounter():
		i, err = m.meter.Float64Counter(event.EventMessage)
	case witness.EventTypeMetricHistogram():
		i, err = m.meter.Float64Histogram(event.EventMessage)
	default:
		i, err = m.meter.Float64Gauge(event.EventMessage)
	}
	if err != nil {
		return nil
	}
	m.inst[key] = i
	return i
}
