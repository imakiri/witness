package witness

import (
	"context"
	"github.com/imakiri/witness/record"
	"github.com/opentracing/opentracing-go"
	otLog "github.com/opentracing/opentracing-go/log"
	"go.uber.org/zap"
)

const KeyLogger = "vp7bDJF4dHfKy545JohmsL8yoelUtpli"

func WithZapLogger(logger *zap.Logger) Option {
	return option(func(ctx context.Context) context.Context {
		return context.WithValue(ctx, KeyLogger, logger.
			WithOptions(zap.AddCallerSkip(2)).
			Named(TraceID(ctx).String()))
	})
}

type level int8

const (
	levelInfo level = iota + 1
	levelWarn
	levelError
	levelDebug
)

func fixingRobPikeIdiocracy(records []Record) []Record {
	var newRecords = make([]Record, 0, len(records))
	for _, r := range records {
		if rs, ok := r.(record.Records); ok {
			for _, r := range rs {
				newRecords = append(newRecords, r)
			}
		} else {
			if r != nil {
				newRecords = append(newRecords, r)
			}
		}
	}
	return newRecords
}

func log(ctx context.Context, lvl level, msg string, records ...Record) {
	records = fixingRobPikeIdiocracy(records)
	var exRecords = Records(ctx)
	switch logger := ctx.Value(KeyLogger).(type) {
	case *zap.Logger:
		var fields = make([]zap.Field, 0, 1+len(records)+len(exRecords))
		fields = append(fields, zap.Stringer("trace_id", TraceID(ctx)))
		for _, exRecord := range exRecords {
			fields = append(fields, zap.Stringer(exRecord.Key(), exRecord))
		}
		for _, r := range records {
			fields = append(fields, zap.Stringer(r.Key(), r))
		}
		switch lvl {
		case levelInfo:
			logger.Info(msg, fields...)
		case levelWarn:
			logger.Warn(msg, fields...)
		case levelError:
			logger.Error(msg, fields...)
		case levelDebug:
			logger.Debug(msg, fields...)
		}
	}

	var span = opentracing.SpanFromContext(ctx)
	if span != nil {
		var fields = make([]otLog.Field, 0, 1+len(records))
		fields = append(fields, otLog.String("trace_id", TraceID(ctx).String()))
		for _, exRecord := range exRecords {
			fields = append(fields, otLog.String(exRecord.Key(), exRecord.String()))
		}
		for _, r := range records {
			fields = append(fields, otLog.String(r.Key(), r.String()))
		}
		span.LogFields(fields...)
	}
}

func Info(ctx context.Context, msg string, records ...Record) {
	log(ctx, levelInfo, msg, records...)
}

func Warn(ctx context.Context, msg string, records ...Record) {
	log(ctx, levelWarn, msg, records...)
}

func Error(ctx context.Context, msg string, records ...Record) {
	log(ctx, levelError, msg, records...)
}

func Debug(ctx context.Context, msg string, records ...Record) {
	log(ctx, levelDebug, msg, records...)
}

func OnError(ctx context.Context, msg string, err error, from string, records ...Record) {
	if err != nil {
		log(ctx, levelError, msg, append(records, record.Error(from, err))...)
	}
}

func InfoOrError(ctx context.Context, msg string, err error, from string, records ...Record) {
	if err != nil {
		log(ctx, levelError, msg, append(records, record.Error(from, err))...)
	} else {
		log(ctx, levelInfo, msg, records...)
	}
}

func DebugOrError(ctx context.Context, msg string, err error, from string, records ...Record) {
	if err != nil {
		log(ctx, levelError, msg, append(records, record.Error(from, err))...)
	} else {
		log(ctx, levelDebug, msg, records...)
	}
}
