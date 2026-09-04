// Service C: pure-compute service. Called by service_a (and sometimes by
// service_b) via POST /compute carrying a W3C traceparent. Exists so the
// dashboard's service map has more than a single edge to draw.
package main

import (
	"context"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/imakiri/witness"
	"github.com/imakiri/witness/core"
	"github.com/imakiri/witness/observers/postgres"
	"github.com/imakiri/witness/propagation"
	"github.com/imakiri/witness/record"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dbURL := envOr("WITNESS_DB", "postgres://witness:witness@localhost:5432/witness?sslmode=disable")
	addr := envOr("LISTEN", ":8082")

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

	bootCtx, finishBoot := witness.Instance(context.Background(), obs, "service-c-boot", "1.0")
	witness.Info(bootCtx, "service-c listening", record.String("addr", addr))
	finishBoot()

	// One instance per process, not per request: an instance *is* the
	// process. Requests are spans under it.
	instanceCtx, finishInstance := witness.Instance(context.Background(), obs, "service-c", "1.0")
	defer finishInstance()

	srv := &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqCtx := core.From(instanceCtx).To(r.Context())

			ctx, finish := witness.Span(reqCtx, "POST /compute")
			defer finish()

			// The upstream span_id is referenced, not entered: this process
			// never opens a span another one owns. Both sides emit events
			// carrying it, so one query on it reconnects them.
			if upstreamSpanID, ok := propagation.Extract(r.Header); ok {
				witness.ExternalMessageReceived(ctx, upstreamSpanID, "POST /compute")
			}

			compute(ctx)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("computed\n"))
		}),
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("listen: %v", err)
	}
}

func compute(ctx context.Context) {
	ctx, finish := witness.Span(ctx, "compute")
	defer finish()
	witness.Info(ctx, "computing payload", record.String("phase", "warmup"))
	time.Sleep(time.Duration(3+rand.Intn(8)) * time.Millisecond)
	witness.Info(ctx, "computation done", record.Int("result", rand.Intn(1000)))
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
