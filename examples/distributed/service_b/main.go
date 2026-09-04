// Service B: opens one witness Instance for the process, then enters the
// span_id each incoming request carries in its W3C traceparent, so its
// events land in the very span service-a opened. Sometimes chains the call
// onward to service-c.
package main

import (
	"bytes"
	"context"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
	"github.com/imakiri/witness/observers/postgres"
	"github.com/imakiri/witness/propagation"
	"github.com/imakiri/witness/record"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := envOr("WITNESS_DB", "postgres://witness:witness@localhost:5432/witness?sslmode=disable")
	cURL := envOr("SERVICE_C_URL", "http://localhost:8082/compute")
	addr := envOr("LISTEN", ":8081")

	pgcfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("pgxpool.ParseConfig: %v", err)
	}
	pgcfg.MaxConns = 4

	obs, err := postgres.NewObserver(postgres.Config{
		ConnectionTimeout:  5 * time.Second,
		CollectionDuration: 250 * time.Millisecond,
		CollectionMaxSize:  256,
		Database:           pgcfg,
	})
	if err != nil {
		log.Fatalf("postgres.NewObserver: %v", err)
	}
	defer obs.Close()

	client := &http.Client{Timeout: 5 * time.Second}

	// One instance per process, not per request: an instance *is* the
	// process. Requests are spans under it.
	instanceCtx, finishInstance := witness.Instance(context.Background(), obs, "service-b", "1.0")
	defer finishInstance()

	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCtx := core.From(instanceCtx).To(r.Context())

			ctx, finish := witness.Span(reqCtx, "POST /work")
			defer finish()

			// The upstream span_id is referenced, not entered: this process
			// never opens a span another one owns. Both sides emit events
			// carrying it, so one query on it reconnects them.
			if upstreamSpanID, ok := propagation.Extract(r.Header); ok {
				witness.Received(ctx, upstreamSpanID, "POST /work")
			}

			handle(ctx)

			// 50% chance to chain onward to service-c so the service map
			// sometimes shows a→b→c, sometimes just a→b.
			if rand.Intn(2) == 0 {
				callServiceC(ctx, client, cURL)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ack\n"))
		}),
	}

	witness.Info(instanceCtx, "service-b listening", record.String("addr", addr))

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func handle(ctx context.Context) {
	ctx, finish := witness.Span(ctx, "process-request")
	defer finish()
	witness.Info(ctx, "doing work", record.String("phase", "compute"))
	time.Sleep(10 * time.Millisecond)
	witness.Info(ctx, "work done")
}

func callServiceC(ctx context.Context, client *http.Client, url string) {
	ctx, finish := witness.Span(ctx, "chain-to-c")
	defer finish()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString("from B"))
	if err != nil {
		witness.Error(ctx, "build request service-c", err)
		return
	}
	var msgID = uuid.Must(uuid.NewV7())
	propagation.Inject(req.Header, msgID)

	resp, err := client.Do(req)
	if err != nil {
		witness.Error(ctx, "call service-c", err)
		return
	}
	// Emitted after the call succeeded: a send that failed handed nothing off.
	witness.Sent(ctx, msgID, "POST service-c /compute (from B)", record.String("url", url))
	resp.Body.Close()
	witness.Info(ctx, "response from service-c",
		record.Int("status", resp.StatusCode))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
