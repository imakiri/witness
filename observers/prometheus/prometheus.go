// Package prometheus is a witness Observer that exposes pre-aggregated
// metric events as Prometheus metrics over HTTP.
//
// Witness clients are expected to aggregate metrics over a window and emit
// one event per window per metric (see EventTypeMetricCounter and
// EventTypeMetricHistogram). This observer accumulates those window deltas
// into Prometheus-style cumulative metrics and exposes them via a standard
// /metrics HTTP handler.
//
// Counter events (event_type "metric:counter"):
//
//	event_message   -> metric name
//	record "value"  -> delta to add to the cumulative counter
//	record "window_ms" (optional) -> ignored, informational
//	all other records -> Prometheus labels
//
// Histogram events (event_type "metric:histogram") are exposed as Prometheus
// summaries with the pre-computed quantiles carried on the event:
//
//	event_message   -> metric name
//	record "count"  -> delta to add to the cumulative observation count
//	record "p<N>"   -> quantile N/10^digits (p50 -> 0.5, p999 -> 0.999)
//	record "sum"    -> optional, added to cumulative sum if present
//	all other records -> Prometheus labels
//
// Sum is optional because witness histogram events are not required to
// carry it; when absent, the exposed _sum stays at zero.
package prometheus

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Observer struct {
	mu         sync.Mutex
	counters   map[string]*counterFamily   // key = metric name + sorted label keys
	histograms map[string]*histogramFamily // key = metric name + sorted label keys
	registry   *prometheus.Registry
}

func NewObserver() *Observer {
	o := &Observer{
		counters:   make(map[string]*counterFamily),
		histograms: make(map[string]*histogramFamily),
		registry:   prometheus.NewRegistry(),
	}
	o.registry.MustRegister(o)
	return o
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

// Describe implements prometheus.Collector. The observer is an "unchecked"
// collector — metric descriptors are discovered at runtime from incoming
// events, so no descriptors are emitted here.
func (o *Observer) Describe(_ chan<- *prometheus.Desc) {}

// Collect implements prometheus.Collector. Called by the Prometheus registry
// on each scrape.
func (o *Observer) Collect(ch chan<- prometheus.Metric) {
	o.mu.Lock()
	defer o.mu.Unlock()

	for _, f := range o.counters {
		for _, s := range f.series {
			ch <- prometheus.MustNewConstMetric(f.desc, prometheus.CounterValue, s.value, s.labelValues...)
		}
	}
	for _, f := range o.histograms {
		for _, s := range f.series {
			ch <- prometheus.MustNewConstSummary(f.desc, s.count, s.sum, s.quantiles, s.labelValues...)
		}
	}
}

func (o *Observer) Observe(_ []uuid.UUID, _ uuid.UUID, _ time.Time, eventType witness.EventType, eventMessage string, _ string, records ...witness.Record) {
	switch eventType {
	case witness.EventTypeMetricCounter():
		o.observeCounter(eventMessage, records)
	case witness.EventTypeMetricHistogram():
		o.observeHistogram(eventMessage, records)
	}
}

type counterFamily struct {
	desc      *prometheus.Desc
	labelKeys []string
	series    map[string]*counterSeries // key = joined label values
}

type counterSeries struct {
	labelValues []string
	value       float64
}

type histogramFamily struct {
	desc      *prometheus.Desc
	labelKeys []string
	series    map[string]*histogramSeries
}

type histogramSeries struct {
	labelValues []string
	count       uint64
	sum         float64
	quantiles   map[float64]float64
}

func (o *Observer) observeCounter(name string, records []witness.Record) {
	var delta float64
	labels, deltaFound := splitCounterRecords(records, &delta)
	if !deltaFound {
		return
	}
	labelKeys, labelValues := sortLabels(labels)
	familyKey := familyKey(name, labelKeys)
	seriesKey := joinValues(labelValues)

	o.mu.Lock()
	defer o.mu.Unlock()

	f, ok := o.counters[familyKey]
	if !ok {
		f = &counterFamily{
			desc:      prometheus.NewDesc(name, "witness metric:counter", labelKeys, nil),
			labelKeys: labelKeys,
			series:    make(map[string]*counterSeries),
		}
		o.counters[familyKey] = f
	}
	s, ok := f.series[seriesKey]
	if !ok {
		s = &counterSeries{labelValues: labelValues}
		f.series[seriesKey] = s
	}
	s.value += delta
}

func (o *Observer) observeHistogram(name string, records []witness.Record) {
	var countDelta uint64
	var sumDelta float64
	quantiles := make(map[float64]float64)
	labels, countFound := splitHistogramRecords(records, &countDelta, &sumDelta, quantiles)
	if !countFound && len(quantiles) == 0 {
		return
	}
	labelKeys, labelValues := sortLabels(labels)
	familyKey := familyKey(name, labelKeys)
	seriesKey := joinValues(labelValues)

	o.mu.Lock()
	defer o.mu.Unlock()

	f, ok := o.histograms[familyKey]
	if !ok {
		f = &histogramFamily{
			desc:      prometheus.NewDesc(name, "witness metric:histogram", labelKeys, nil),
			labelKeys: labelKeys,
			series:    make(map[string]*histogramSeries),
		}
		o.histograms[familyKey] = f
	}
	s, ok := f.series[seriesKey]
	if !ok {
		s = &histogramSeries{
			labelValues: labelValues,
			quantiles:   make(map[float64]float64),
		}
		f.series[seriesKey] = s
	}
	s.count += countDelta
	s.sum += sumDelta
	for q, v := range quantiles {
		s.quantiles[q] = v
	}
}

func splitCounterRecords(records []witness.Record, delta *float64) (labels map[string]string, deltaFound bool) {
	labels = make(map[string]string, len(records))
	for _, r := range records {
		k := string(r.AppendKey(nil))
		v := string(r.AppendValue(nil))
		switch k {
		case "value":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				*delta = f
				deltaFound = true
			}
		case "window_ms":
			// informational; do not expose as label
		default:
			labels[k] = v
		}
	}
	return labels, deltaFound
}

func splitHistogramRecords(records []witness.Record, count *uint64, sum *float64, quantiles map[float64]float64) (labels map[string]string, countFound bool) {
	labels = make(map[string]string, len(records))
	for _, r := range records {
		k := string(r.AppendKey(nil))
		v := string(r.AppendValue(nil))
		switch k {
		case "count":
			if n, err := strconv.ParseUint(v, 10, 64); err == nil {
				*count = n
				countFound = true
			}
		case "sum":
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				*sum = f
			}
		case "window_ms":
			// informational; do not expose as label
		default:
			if q, ok := parseQuantileKey(k); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					quantiles[q] = f
					continue
				}
			}
			labels[k] = v
		}
	}
	return labels, countFound
}

// parseQuantileKey turns "p50" -> 0.5, "p95" -> 0.95, "p999" -> 0.999.
// Digits after 'p' are read as a fractional decimal: divisor is 10^len(digits),
// so trailing-9 conventions like p999/p9999 work.
func parseQuantileKey(k string) (float64, bool) {
	if len(k) < 2 || k[0] != 'p' {
		return 0, false
	}
	digits := k[1:]
	n, err := strconv.ParseUint(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	div := math.Pow10(len(digits))
	if div == 0 {
		return 0, false
	}
	return float64(n) / div, true
}

func sortLabels(labels map[string]string) (keys, values []string) {
	keys = make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	values = make([]string, len(keys))
	for i, k := range keys {
		values[i] = labels[k]
	}
	return keys, values
}

func familyKey(name string, sortedLabelKeys []string) string {
	var b strings.Builder
	b.Grow(len(name) + 1 + len(sortedLabelKeys)*8)
	b.WriteString(name)
	b.WriteByte('|')
	for i, k := range sortedLabelKeys {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(k)
	}
	return b.String()
}

// joinValues joins sorted label values into a series key. Uses a separator
// unlikely to appear in label values; collisions are unlikely in practice.
func joinValues(values []string) string {
	return strings.Join(values, "\x00")
}
