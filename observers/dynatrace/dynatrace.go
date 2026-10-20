// Package dynatrace exports witness events to Dynatrace over its OTLP ingest
// API (OTLP/HTTP, protobuf — Dynatrace accepts neither gRPC nor JSON there).
//
// One event goes to exactly one signal:
//
//	span:*    -> traces, through an embedded otlp.Observer (same span tree,
//	             same ids, same traceparent chaining as the Jaeger setup)
//	metric:*  -> metrics, one instrument per event_message (see metrics.go)
//	the rest  -> logs, carrying the trace_id/span_id of the event's span so
//	             Dynatrace shows them in the context of that trace
//
// Log events also still reach the otlp.Observer, which turns them into span
// events and sets the span status on errors — the trace view needs those.
package dynatrace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/otlp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Config struct {
	// Endpoint is the OTLP base URL, without the /v1/<signal> suffix:
	//
	//	SaaS:       https://{env-id}.live.dynatrace.com/api/v2/otlp
	//	Managed:    https://{domain}/e/{env-id}/api/v2/otlp
	//	ActiveGate: https://{activegate}:9999/e/{env-id}/api/v2/otlp
	//	OneAgent:   http://localhost:14499/otlp
	Endpoint string

	// APIToken needs the scopes openTelemetryTrace.ingest, logs.ingest and
	// metrics.ingest. Leave it empty for the OneAgent local endpoint, which
	// takes no token.
	APIToken string

	// ServiceName becomes the service.name resource attribute, merged over
	// Resource. Dynatrace names the service after it.
	ServiceName string
	Resource    *resource.Resource

	// SampleRate of 0 means AlwaysSample. Logs are not sampled: a log of an
	// unsampled trace is still exported with its trace_id, but without the
	// sampled flag, so it does not claim a trace that never arrives.
	SampleRate float64

	DisableLogs    bool
	DisableMetrics bool

	// MetricInterval is the export period; 0 keeps the SDK default (60s).
	MetricInterval time.Duration
}

type Observer struct {
	traces  *otlp.Observer
	logs    *logs    // nil when disabled
	metrics *metrics // nil when disabled

	shutdown []func(context.Context) error
}

func NewObserver(ctx context.Context, cfg Config) (*Observer, error) {
	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("dynatrace: Config.Endpoint is required")
	}
	base := strings.TrimRight(cfg.Endpoint, "/")
	var headers map[string]string
	if cfg.APIToken != "" {
		headers = map[string]string{"Authorization": "Api-Token " + cfg.APIToken}
	}
	res, err := newResource(cfg)
	if err != nil {
		return nil, err
	}

	// Every exporter is built before any provider: the providers' batch
	// processors and the periodic reader start goroutines, which an exporter
	// failing further down would otherwise leave running.
	traceExp, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(base+"/v1/traces"),
		otlptracehttp.WithHeaders(headers),
	)
	if err != nil {
		return nil, fmt.Errorf("dynatrace: trace exporter: %w", err)
	}
	var logExp *otlploghttp.Exporter
	if !cfg.DisableLogs {
		logExp, err = otlploghttp.New(ctx,
			otlploghttp.WithEndpointURL(base+"/v1/logs"),
			otlploghttp.WithHeaders(headers),
		)
		if err != nil {
			return nil, fmt.Errorf("dynatrace: log exporter: %w", err)
		}
	}
	var metricExp *otlpmetrichttp.Exporter
	if !cfg.DisableMetrics {
		metricExp, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpointURL(base+"/v1/metrics"),
			otlpmetrichttp.WithHeaders(headers),
			otlpmetrichttp.WithTemporalitySelector(deltaTemporality),
		)
		if err != nil {
			return nil, fmt.Errorf("dynatrace: metric exporter: %w", err)
		}
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate > 0 && cfg.SampleRate < 1 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
		sdktrace.WithIDGenerator(otlp.IDGenerator()),
	)

	var lp *sdklog.LoggerProvider
	if logExp != nil {
		lp = sdklog.NewLoggerProvider(
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
			sdklog.WithResource(res),
		)
	}

	var mp *sdkmetric.MeterProvider
	if metricExp != nil {
		var readerOpts []sdkmetric.PeriodicReaderOption
		if cfg.MetricInterval > 0 {
			readerOpts = append(readerOpts, sdkmetric.WithInterval(cfg.MetricInterval))
		}
		mp = sdkmetric.NewMeterProvider(
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, readerOpts...)),
			sdkmetric.WithResource(res),
		)
	}

	return newObserver(tp, sampler, lp, mp)
}

// newObserver wires already built providers; tests pass in-memory ones.
// tp must use otlp.IDGenerator, or log span_ids name spans that do not exist,
// and sampler must be the one tp samples with, or logs claim the wrong traces
// as sampled.
func newObserver(tp *sdktrace.TracerProvider, sampler sdktrace.Sampler, lp *sdklog.LoggerProvider, mp *sdkmetric.MeterProvider) (*Observer, error) {
	traces, err := otlp.NewObserver(otlp.Config{Provider: tp})
	if err != nil {
		return nil, err
	}
	o := &Observer{traces: traces, shutdown: []func(context.Context) error{traces.Shutdown}}
	if lp != nil {
		o.logs = newLogs(lp, sampler)
		o.shutdown = append(o.shutdown, lp.Shutdown)
	}
	if mp != nil {
		o.metrics = newMetrics(mp)
		o.shutdown = append(o.shutdown, mp.Shutdown)
	}
	return o, nil
}

func newResource(cfg Config) (*resource.Resource, error) {
	res := cfg.Resource
	if res == nil {
		res = resource.Default()
	}
	if cfg.ServiceName == "" {
		return res, nil
	}
	res, err := resource.Merge(res, resource.NewSchemaless(attribute.String("service.name", cfg.ServiceName)))
	if err != nil {
		return nil, fmt.Errorf("dynatrace: resource: %w", err)
	}
	return res, nil
}

// deltaTemporality: Dynatrace rejects cumulative monotonic sums and
// histograms. UpDownCounters stay cumulative — that is what it expects there.
func deltaTemporality(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case sdkmetric.InstrumentKindCounter,
		sdkmetric.InstrumentKindHistogram,
		sdkmetric.InstrumentKindObservableCounter:
		return metricdata.DeltaTemporality
	default:
		return metricdata.CumulativeTemporality
	}
}

// Shutdown flushes and stops every signal; call it before the process exits
// or the last batch is lost.
func (o *Observer) Shutdown(ctx context.Context) error {
	var errs []error
	for _, f := range o.shutdown {
		errs = append(errs, f(ctx))
	}
	return errors.Join(errs...)
}

func (o *Observer) Observe(event witness.Event) {
	name := event.EventType.String()
	switch {
	case strings.HasPrefix(name, "span"):
		o.traces.Observe(event)
	case strings.HasPrefix(name, "metric"):
		if o.metrics != nil {
			o.metrics.observe(event)
		}
	default:
		o.traces.Observe(event)
		if o.logs != nil {
			o.logs.observe(event)
		}
	}
}
