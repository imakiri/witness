package witness

import (
	"bytes"
	"context"
	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness/record"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"os"
	"testing"
)

func TestLogger(t *testing.T) {
	var buf = new(bytes.Buffer)
	var encoderCfg = zapcore.EncoderConfig{
		MessageKey:     "msg",
		LevelKey:       "level",
		NameKey:        "logger",
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.StringDurationEncoder,
	}
	var core = zapcore.NewCore(zapcore.NewJSONEncoder(encoderCfg), zapcore.AddSync(buf), zapcore.DebugLevel)
	var logger = zap.New(core)
	defer logger.Sync()

	var ctx = context.Background()
	var traceID = uuid.Must(uuid.FromString("018f4c15-1446-7cd1-b981-97ab714256be"))
	ctx = New(ctx, traceID, WithZapLogger(logger), WithRecords(record.New("service", "test-service")))

	Info(context.Background(), "test-info", record.Int("test-key", 10))
	Info(ctx, "test-info", record.Int("test-key", 10))
	Warn(ctx, "test-warn", record.String("test-key", "test-value"))
	Error(ctx, "test-error", record.New("test-key", "test-value"))
	Debug(ctx, "test-error", record.New("test-key", "test-value"))

	const expected = `{"level":"info","msg":"test-info","trace_id":"018f4c15-1446-7cd1-b981-97ab714256be","service":"test-service","test-key":"10"}
{"level":"warn","msg":"test-warn","trace_id":"018f4c15-1446-7cd1-b981-97ab714256be","service":"test-service","test-key":"test-value"}
{"level":"error","msg":"test-error","trace_id":"018f4c15-1446-7cd1-b981-97ab714256be","service":"test-service","test-key":"test-value"}
{"level":"debug","msg":"test-error","trace_id":"018f4c15-1446-7cd1-b981-97ab714256be","service":"test-service","test-key":"test-value"}
`
	var actual = buf.String()

	buf.WriteTo(os.Stdout)
	require.Equal(t, expected, actual)
}
