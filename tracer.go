package witness

import (
	"context"
	"github.com/opentracing/opentracing-go"
)

const KeyTracer = "FZxAMruPDKPab5veVqEmJqYFlwp1hYOx"

func WithOpenTracer(tracer opentracing.Tracer) Option {
	return option(func(ctx context.Context) context.Context {
		return context.WithValue(ctx, KeyTracer, tracer)
	})
}

type relation int8

const (
	relationChildOf relation = iota + 1
	relationFollowsFrom
)

func trace(ctx context.Context, rel relation, name string, records ...Record) (cxx context.Context, finish func()) {
	var exRecords = Records(ctx)
	switch tracer := ctx.Value(KeyTracer).(type) {
	case opentracing.Tracer:
		var tags = make(opentracing.Tags)
		for _, record := range exRecords {
			tags[record.Key()] = record.String()
		}
		for _, record := range records {
			tags[record.Key()] = record.String()
		}

		switch rel {
		case relationChildOf:
			span, cxx := opentracing.StartSpanFromContextWithTracer(ctx, tracer, name, tags)
			return cxx, span.Finish
		case relationFollowsFrom:
			var span = opentracing.SpanFromContext(ctx)
			if span == nil {
				span, cxx := opentracing.StartSpanFromContextWithTracer(ctx, tracer, name, tags)
				return cxx, span.Finish
			}
			span = tracer.StartSpan(name, tags, opentracing.FollowsFrom(span.Context()))
			return opentracing.ContextWithSpan(ctx, span), span.Finish
		default:
			return ctx, func() {}
		}
	default:
		return ctx, func() {}
	}
}

func TraceChildOf(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return trace(ctx, relationChildOf, name, records...)
}

func TraceFollowsFrom(ctx context.Context, name string, records ...Record) (cxx context.Context, finish func()) {
	return trace(ctx, relationFollowsFrom, name, records...)
}
