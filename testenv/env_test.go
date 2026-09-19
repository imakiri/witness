package testenv

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/imakiri/witness/observers/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestEnv brings the stack up and keeps two services emitting events into it
// until you stop it with Ctrl-C. It asserts nothing — it is something to
// look at in Grafana.
//
//	WITNESS_TESTENV=1 go test -v -timeout 0 -run TestEnv ./testenv
func TestEnv(t *testing.T) {
	if os.Getenv("WITNESS_TESTENV") == "" {
		t.Skip("set WITNESS_TESTENV=1 to run the demo stack; see testenv/README.md")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := Start(ctx, t.Logf)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	defer env.Close(context.WithoutCancel(ctx))

	cfg, err := pgxpool.ParseConfig(env.DSN)
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	cfg.MaxConns = 8

	// One observer for both services — it is a sink, and two pools buy
	// nothing. CollectionMaxSize is both the channel buffer and the largest
	// batch: raising it to absorb load would also make one SendBatch huge,
	// and a single over-long value aborts the whole batch. More workers is
	// the knob that actually helps.
	obs, err := postgres.NewObserver(postgres.Config{
		ConnectionTimeout:  10 * time.Second,
		CollectionDuration: 250 * time.Millisecond,
		CollectionMaxSize:  1024,
		BatchTimeout:       5 * time.Second,
		ShutdownTimeout:    5 * time.Second,
		Database:           cfg,
	})
	if err != nil {
		t.Fatalf("observer: %v", err)
	}
	defer obs.Close()

	if err := createTopic(ctx, env.Brokers); err != nil {
		t.Fatalf("create topic: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); RunProcessor(ctx, obs, env.Brokers) }()
	go func() { defer wg.Done(); RunIngest(ctx, obs, env.Brokers) }()

	t.Logf("")
	t.Logf("Grafana:  %s   (home dashboard: Witness / Trace)", env.GrafanaURL)
	t.Logf("Postgres: %s", env.DSN)
	t.Logf("Kafka:    %v", env.Brokers)
	t.Logf("Stop with Ctrl-C.")

	// Dropped() counts events the observer's channel could not take. Without
	// this line a full channel looks like holes in Grafana and reads as
	// witness losing data.
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Logf("stopping")
			wg.Wait()
			return
		case <-ticker.C:
			t.Logf("running; events dropped so far: %d", obs.Dropped())
		}
	}
}
