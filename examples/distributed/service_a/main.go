// Service A: opens a witness Instance, exposes /work which calls Service B
// and/or Service C while injecting a W3C traceparent. Picks a random fan-out
// pattern per request so the dashboard shows several distinct trace shapes.
package main

import (
	"bytes"
	"context"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/observers/postgres"
	"github.com/imakiri/witness/propagation"
	"github.com/imakiri/witness/record"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := envOr("WITNESS_DB", "postgres://witness:witness@localhost:5432/witness?sslmode=disable")
	bURL := envOr("SERVICE_B_URL", "http://localhost:8081/sub")
	cURL := envOr("SERVICE_C_URL", "http://localhost:8082/compute")
	addr := envOr("LISTEN", ":8080")

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

	ctx, finish := witness.Instance(context.Background(), obs, "service-a", "1.0")
	defer finish()

	client := &http.Client{Timeout: 5 * time.Second}

	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// One span per request. Downstream services rejoin it by the
			// span_id carried in the traceparent header — there is no
			// trace_id, the shared span_id is the link.
			reqCtx := witness.From(ctx).To(r.Context())
			workCtx, finishWork := witness.Span(reqCtx, "handle-work")
			defer finishWork()

			// Pick a fan-out pattern: 0 = call only B, 1 = call only C,
			// 2 = call both in sequence. Combined with service_b's
			// occasional chain to C this gives four distinct trace shapes
			// in the service map.
			mode := rand.Intn(3)
			if mode == 0 || mode == 2 {
				callPeer(workCtx, client, bURL, "service-b", "POST service-b /sub", "hello from A")
			}
			if mode == 1 || mode == 2 {
				callPeer(workCtx, client, cURL, "service-c", "POST service-c /compute", "compute=42")
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("ok\n"))
		}),
	}

	witness.Info(ctx, "service-a listening", record.String("addr", addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func callPeer(ctx context.Context, client *http.Client, url, peer, msgName, body string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		witness.Error(ctx, "build request "+peer, err)
		return
	}

	// The send mints the shared span_id and hands it back for the carrier;
	// the peer references the same id and a query on it returns both sides.
	msgID := witness.ExternalMessageSent(ctx, msgName, record.String("url", url))
	propagation.Inject(req.Header, msgID)

	resp, err := client.Do(req)
	if err != nil {
		witness.Error(ctx, "call "+peer, err)
		return
	}
	resp.Body.Close()
	witness.ExternalMessageReceived(ctx, msgID, "response from "+peer,
		record.Int("status", resp.StatusCode))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
