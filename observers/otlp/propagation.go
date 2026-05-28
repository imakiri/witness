package otlp

// W3C traceparent inject/extract. Call Inject before sending an outbound
// request, Extract on the receiving side before *MessageReceived.

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/gofrs/uuid/v5"
)

const TraceparentHeader = "traceparent"

// Carrier matches http.Header.Get/Set without dragging in net/http.
type Carrier interface {
	Get(key string) string
	Set(key, value string)
}

func Inject(carrier Carrier, rootSpanID, msgSpanID uuid.UUID) {
	traceID := traceIDFromUUID(rootSpanID)
	spanID := spanIDFromUUID(msgSpanID)
	carrier.Set(TraceparentHeader, fmt.Sprintf("00-%s-%s-01",
		hex.EncodeToString(traceID[:]),
		hex.EncodeToString(spanID[:]),
	))
}

// Extract returns trace_id (16 bytes) and the span_id padded into the low 8
// bytes of a uuid.UUID. Treat msgSpanID as opaque, not a real UUID v7.
func Extract(carrier Carrier) (traceID uuid.UUID, msgSpanID uuid.UUID, ok bool) {
	header := carrier.Get(TraceparentHeader)
	if header == "" {
		return uuid.Nil, uuid.Nil, false
	}
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return uuid.Nil, uuid.Nil, false
	}
	traceBytes, err := hex.DecodeString(parts[1])
	if err != nil || len(traceBytes) != 16 {
		return uuid.Nil, uuid.Nil, false
	}
	spanBytes, err := hex.DecodeString(parts[2])
	if err != nil || len(spanBytes) != 8 {
		return uuid.Nil, uuid.Nil, false
	}
	copy(traceID[:], traceBytes)
	copy(msgSpanID[8:], spanBytes)
	return traceID, msgSpanID, true
}
