// Package propagation carries witness span identifiers across HTTP boundaries
// using the W3C traceparent header. It is transport-agnostic enough to back
// other carriers — anything that exposes an http.Header-compatible map will
// do — but the only documented API is HTTP. Pair Inject on the sender with
// Extract on the receiver, then feed the returned span_id into
// witness.SpanStart so the receiver's work happens inside the very span the
// sender opened. Both processes then write events carrying that span_id and
// one query on it reconnects them.
//
// Witness has no trace_id: a trace is a connected component of the
// event<->span graph, and one shared span_id is all a receiver needs to
// rejoin it. That leaves the traceparent's 16-byte trace-id field free to
// carry the upstream span_id whole, so Extract recovers the original
// uuid.v7 exactly. The 8-byte parent-id field repeats its low half, which
// is what the W3C format requires and what OTel-native collectors read.
package propagation

import (
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofrs/uuid/v5"
)

const TraceparentHeader = "traceparent"

// Inject writes a W3C traceparent header naming spanID as the span the
// receiver should attach to. The whole uuid goes into the trace-id field
// and its low 8 bytes into the parent-id field.
func Inject(h http.Header, spanID uuid.UUID) {
	tid := traceIDFromUUID(spanID)
	sid := spanIDFromUUID(spanID)
	h.Set(TraceparentHeader, fmt.Sprintf("00-%s-%s-01",
		hex.EncodeToString(tid[:]),
		hex.EncodeToString(sid[:]),
	))
}

// Extract reads a W3C traceparent header and returns the upstream span_id
// whole, recovered from the 16-byte trace-id field. ok is false when the
// header is missing or malformed.
//
// A header written by a non-witness producer carries a real W3C trace-id
// there, not a uuid.v7; the value is still a usable opaque span identifier,
// but do not expect a time-sortable uuid back.
func Extract(h http.Header) (parentSpanID uuid.UUID, ok bool) {
	header := h.Get(TraceparentHeader)
	if header == "" {
		return uuid.Nil, false
	}
	parts := strings.Split(header, "-")
	if len(parts) != 4 {
		return uuid.Nil, false
	}
	traceBytes, err := hex.DecodeString(parts[1])
	if err != nil || len(traceBytes) != 16 {
		return uuid.Nil, false
	}
	if _, err := hex.DecodeString(parts[2]); err != nil || len(parts[2]) != 16 {
		return uuid.Nil, false
	}
	copy(parentSpanID[:], traceBytes)
	return parentSpanID, true
}

func traceIDFromUUID(u uuid.UUID) (out [16]byte) {
	copy(out[:], u[:])
	return out
}

func spanIDFromUUID(u uuid.UUID) (out [8]byte) {
	copy(out[:], u[8:])
	return out
}
