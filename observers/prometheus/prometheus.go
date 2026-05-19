// Package prometheus is a witness Observer that exposes metric events as
// Prometheus metrics over HTTP.
//
// Metrics to be exposed are declared upfront in Config. Events whose
// event_message does not match a configured metric name are dropped.
//
// Counter events (event_type "metric:counter"):
//
//	event_message   -> metric name (must match a configured CounterDef.Name)
//	record "value"  -> increment delta, added via CounterVec.Add
//	other records   -> matched against CounterDef.LabelKeys; unknown keys ignored
//
// Histogram events (event_type "metric:histogram"):
//
//	event_message   -> metric name (must match a configured HistogramDef.Name)
//	record "value"  -> single observation, recorded via HistogramVec.Observe
//	other records   -> matched against HistogramDef.LabelKeys; unknown keys ignored
//
// Counter and histogram events share the same shape: one event carries one
// "value" record. A client may emit value=1 per increment, or batch many
// increments into a single event with a larger value — the observer just
// adds whatever delta arrives.
//
// A configured label that has no matching record on an event is observed
// with an empty value, mirroring standard Prometheus client behavior.
package prometheus

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type CounterDef struct {
	Name      string
	Help      string
	LabelKeys []string
}

type HistogramDef struct {
	Name      string
	Help      string
	LabelKeys []string
	Buckets   []float64 // nil -> prometheus.DefBuckets
}

type Config struct {
	Counters   []CounterDef
	Histograms []HistogramDef
}

type Observer struct {
	counters   map[string]*counterFamily   // key = metric name
	histograms map[string]*histogramFamily // key = metric name
	registry   *prometheus.Registry
}

type counterFamily struct {
	vec       *prometheus.CounterVec
	labelKeys []string // sorted
}

type histogramFamily struct {
	vec       *prometheus.HistogramVec
	labelKeys []string // sorted
}

func NewObserver(config Config) (*Observer, error) {
	o := &Observer{
		counters:   make(map[string]*counterFamily, len(config.Counters)),
		histograms: make(map[string]*histogramFamily, len(config.Histograms)),
		registry:   prometheus.NewRegistry(),
	}

	for _, def := range config.Counters {
		keys := sortedCopy(def.LabelKeys)
		vec := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: def.Name,
			Help: def.Help,
		}, keys)
		if err := o.registry.Register(vec); err != nil {
			return nil, fmt.Errorf("register counter %q: %w", def.Name, err)
		}
		o.counters[def.Name] = &counterFamily{vec: vec, labelKeys: keys}
	}

	for _, def := range config.Histograms {
		keys := sortedCopy(def.LabelKeys)
		buckets := def.Buckets
		if buckets == nil {
			buckets = prometheus.DefBuckets
		}
		vec := prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    def.Name,
			Help:    def.Help,
			Buckets: buckets,
		}, keys)
		if err := o.registry.Register(vec); err != nil {
			return nil, fmt.Errorf("register histogram %q: %w", def.Name, err)
		}
		o.histograms[def.Name] = &histogramFamily{vec: vec, labelKeys: keys}
	}

	return o, nil
}

// Handler returns a Prometheus exposition HTTP handler for this observer's
// registry. Mount it at /metrics.
func (o *Observer) Handler() http.Handler {
	return promhttp.HandlerFor(o.registry, promhttp.HandlerOpts{})
}

// Registry exposes the underlying registry for callers that want to compose
// this observer with other Prometheus collectors.
func (o *Observer) Registry() *prometheus.Registry {
	return o.registry
}

func (o *Observer) Observe(_ []uuid.UUID, _ uuid.UUID, _ time.Time, eventType witness.EventType, eventMessage string, _ string, records ...witness.Record) {
	switch eventType {
	case witness.EventTypeMetricCounter():
		o.observeCounter(eventMessage, records)
	case witness.EventTypeMetricHistogram():
		o.observeHistogram(eventMessage, records)
	}
}

func (o *Observer) observeCounter(name string, records []witness.Record) {
	f, ok := o.counters[name]
	if !ok {
		return
	}
	delta, ok := selectFloat(records, "value")
	if !ok {
		return
	}
	f.vec.WithLabelValues(selectLabelValues(records, f.labelKeys)...).Add(delta)
}

func (o *Observer) observeHistogram(name string, records []witness.Record) {
	f, ok := o.histograms[name]
	if !ok {
		return
	}
	value, ok := selectFloat(records, "value")
	if !ok {
		return
	}
	f.vec.WithLabelValues(selectLabelValues(records, f.labelKeys)...).Observe(value)
}

// selectFloat returns the float value of the first record whose key equals
// the target. The record value is parsed via strconv.ParseFloat.
func selectFloat(records []witness.Record, key string) (float64, bool) {
	for _, r := range records {
		if !r.KeyEqual(key) {
			continue
		}
		f, err := strconv.ParseFloat(string(r.AppendValue(nil)), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

// selectLabelValues returns the value for each configured label key, in the
// same order as labelKeys. Keys without a matching record produce an empty
// string, matching standard Prometheus client behavior.
func selectLabelValues(records []witness.Record, labelKeys []string) []string {
	out := make([]string, len(labelKeys))
	for i, k := range labelKeys {
		for _, r := range records {
			if r.KeyEqual(k) {
				out[i] = string(r.AppendValue(nil))
				break
			}
		}
	}
	return out
}

func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
