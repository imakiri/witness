package otlp

// W3C traceparent inject/extract.
//
// Deprecated: this file is a thin compatibility shim over
// github.com/imakiri/witness/propagation. New code should depend on the
// witness/propagation package directly and pass an http.Header.

import (
	"net/http"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness/propagation"
)

// TraceparentHeader is the W3C header name.
//
// Deprecated: use propagation.TraceparentHeader.
const TraceparentHeader = propagation.TraceparentHeader

// Carrier matches http.Header.Get/Set without dragging in net/http. http.Header
// already satisfies it.
//
// Deprecated: use http.Header directly with the propagation package.
type Carrier interface {
	Get(key string) string
	Set(key, value string)
}

// Inject writes a W3C traceparent header.
//
// Deprecated: use propagation.Inject with an http.Header.
func Inject(carrier Carrier, spanID uuid.UUID) {
	if h, ok := carrier.(http.Header); ok {
		propagation.Inject(h, spanID)
		return
	}
	h := http.Header{}
	propagation.Inject(h, spanID)
	carrier.Set(TraceparentHeader, h.Get(TraceparentHeader))
}

// Extract reads a W3C traceparent header.
//
// Deprecated: use propagation.Extract with an http.Header.
func Extract(carrier Carrier) (parentSpanID uuid.UUID, ok bool) {
	if h, ok := carrier.(http.Header); ok {
		return propagation.Extract(h)
	}
	h := http.Header{}
	if v := carrier.Get(TraceparentHeader); v != "" {
		h.Set(TraceparentHeader, v)
	}
	return propagation.Extract(h)
}
