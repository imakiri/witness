package otlp

import (
	"net/http"

	"github.com/imakiri/witness"
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
	spans := witness.From(req.Context()).SpanIDs()
	if len(spans) > 0 {
		req = req.Clone(req.Context())
		Inject(req.Header, spans[0], spans[len(spans)-1])
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
			if traceID, parentSpanID, ok := Extract(r.Header); ok {
				ctx, finish = witness.InstanceContinue(ctx, observer, name, version, traceID, parentSpanID)
			} else {
				ctx, finish = witness.Instance(ctx, observer, name, version)
			}
			defer finish()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
