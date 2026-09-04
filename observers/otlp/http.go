package otlp

import (
	"context"
	"net/http"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
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
	spans := core.From(req.Context()).SpanIDs()
	if len(spans) > 0 {
		req = req.Clone(req.Context())
		Inject(req.Header, spans[len(spans)-1])
	}
	return t.base.RoundTrip(req)
}

// Middleware opens one span per incoming request under the process's
// witness Context, which instanceCtx must carry — call witness.Instance
// once at startup and pass its context here. An instance is a process, not
// a request: opening one per request would claim a new process on every
// call and put SpanFlagInstance on thousands of spans.
//
// When the request carries a W3C traceparent, the span_id it names is
// *referenced* from inside the request span with witness.ExternalMessageReceived
// — not entered. Both processes then emit events carrying that span_id, so
// one query on it returns both sides, while each span's start and finish
// stay with the process that owns it.
func Middleware(instanceCtx context.Context) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			name := r.Method + " " + r.URL.Path
			ctx, finish := witness.Span(core.From(instanceCtx).To(r.Context()), name)
			defer finish()
			if upstreamSpanID, ok := Extract(r.Header); ok {
				witness.ExternalMessageReceived(ctx, upstreamSpanID, name)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
