package otlp

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type Protocol int

const (
	ProtocolGRPC Protocol = iota
	ProtocolHTTP
)

// ProviderConfig configures the OTLP exporter. Endpoint is host:port for
// gRPC (e.g. "otel-collector:4317") or full URL for HTTP (e.g.
// "http://otel-collector:4318"). SampleRate of 0 means AlwaysSample.
type ProviderConfig struct {
	Protocol   Protocol
	Endpoint   string
	Insecure   bool
	Headers    map[string]string
	Resource   *resource.Resource
	SampleRate float64
	BatchOpts  []sdktrace.BatchSpanProcessorOption
}

func NewTraceProvider(ctx context.Context, cfg ProviderConfig) (*sdktrace.TracerProvider, error) {
	exporter, err := newExporter(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("otlp: build exporter: %w", err)
	}

	res := cfg.Resource
	if res == nil {
		res = resource.Default()
	}

	sampler := sdktrace.AlwaysSample()
	if cfg.SampleRate > 0 && cfg.SampleRate < 1 {
		sampler = sdktrace.TraceIDRatioBased(cfg.SampleRate)
	}

	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, cfg.BatchOpts...),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	), nil
}

func newExporter(ctx context.Context, cfg ProviderConfig) (*otlptrace.Exporter, error) {
	switch cfg.Protocol {
	case ProtocolGRPC:
		opts := []otlptracegrpc.Option{otlptracegrpc.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		return otlptrace.New(ctx, otlptracegrpc.NewClient(opts...))
	case ProtocolHTTP:
		opts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(cfg.Endpoint)}
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		return otlptrace.New(ctx, otlptracehttp.NewClient(opts...))
	default:
		return nil, fmt.Errorf("otlp: unknown protocol %d", cfg.Protocol)
	}
}
