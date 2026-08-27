// Service B: extracts the W3C traceparent from incoming requests, uses
// witness.InstanceContinue to graft a child instance under the upstream
// span, and (sometimes) chains the call onward to service-c.
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

	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			traceID, parentSpanID, _ := propagation.Extract(r.Header)

			ctx, finish := witness.InstanceContinue(r.Context(), obs, "service-b", "1.0", traceID, parentSpanID)
			defer finish()

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

	bootCtx, finishBoot := witness.Instance(context.Background(), obs, "service-b-boot", "1.0")
	witness.Info(bootCtx, "service-b listening", record.String("addr", addr))
	finishBoot()

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
	c := witness.From(ctx)
	propagation.Inject(req.Header, c.TraceID(), c.CurrentSpanID())

	msgID := uuid.Must(uuid.NewV7())
	witness.ExternalMessageSent(ctx, msgID, "POST service-c /compute (from B)", record.String("url", url))
	resp, err := client.Do(req)
	if err != nil {
		witness.Error(ctx, "call service-c", err)
		return
	}
	resp.Body.Close()
	witness.ExternalMessageReceived(ctx, msgID, "response from service-c",
		record.Int("status", resp.StatusCode))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
