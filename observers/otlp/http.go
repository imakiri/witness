package otlp

import (
	"net/http"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/propagation"
)

// Transport wraps base so every outgoing request gets a traceparent header
// derived from the current witness span context. If base is nil,
// http.DefaultTransport is used.
func Transport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{base: base}
}

type transport struct{ base http.RoundTripper }

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	// TraceID, not SpanIDs()[0]: after InstanceContinue the first span is the
	// local root, and a second hop would leave with a foreign trace_id.
	c := witness.From(req.Context())
	if c.TraceID() != uuid.Nil && c.CurrentSpanID() != uuid.Nil {
		req = req.Clone(req.Context())
		propagation.Inject(req.Header, c.TraceID(), c.CurrentSpanID())
	}
	return t.base.RoundTrip(req)
}

// Middleware opens a witness Instance for each incoming request. If the
// request carries a W3C traceparent header, the new Instance continues the
// upstream trace via InstanceContinue; otherwise a fresh root span is created
// via Instance.
func Middleware(observer witness.Observer, name, version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			var finish witness.Finish
			if traceID, parentSpanID, ok := propagation.Extract(r.Header); ok {
				ctx, finish = witness.InstanceContinue(ctx, observer, name, version, traceID, parentSpanID)
			} else {
				ctx, finish = witness.Instance(ctx, observer, name, version)
			}
			defer finish()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
