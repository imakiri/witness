// Package propagation carries witness span identifiers across HTTP boundaries
// using the W3C traceparent header. It is transport-agnostic enough to back
// other carriers — anything that exposes an http.Header-compatible map will
// do — but the only documented API is HTTP. Pair Inject on the sender with
// Extract on the receiver, then feed the returned values into
// witness.InstanceContinue to graft the new instance under the upstream span.
package propagation

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
)

const TraceparentHeader = "traceparent"

// Inject writes a W3C traceparent header. traceID is the upstream root span_id
// (first 16 bytes used as trace_id); spanID is the span_id of the operation
// the receiver should attach to (last 8 bytes used as span_id).
func Inject(h http.Header, traceID, spanID uuid.UUID) {
	tid := traceIDFromUUID(traceID)
	sid := spanIDFromUUID(spanID)
	h.Set(TraceparentHeader, fmt.Sprintf("00-%s-%s-01",
		hex.EncodeToString(tid[:]),
		hex.EncodeToString(sid[:]),
	))
}

// Extract reads a W3C traceparent header. parentSpanID is returned as a
// uuid.UUID with the 8-byte W3C span_id padded into the low half — treat it
// as opaque, not a real uuid.v7. ok is false when the header is missing or
// malformed.
func Extract(h http.Header) (traceID uuid.UUID, parentSpanID uuid.UUID, ok bool) {
	header := h.Get(TraceparentHeader)
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
	copy(parentSpanID[8:], spanBytes)
	return traceID, parentSpanID, true
}

func traceIDFromUUID(u uuid.UUID) (out [16]byte) {
	copy(out[:], u[:])
	return out
}

func spanIDFromUUID(u uuid.UUID) (out [8]byte) {
	copy(out[:], u[8:])
	return out
}
