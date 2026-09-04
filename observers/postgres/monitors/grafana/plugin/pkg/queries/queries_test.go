package queries

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/jackc/pgx/v5/pgxpool"
)

// seedSQL builds one distributed request across two processes, entirely in
// SQL so this package needs no dependency on witness itself.
//
//	service-a  instance a0
//	           └── POST /order        s1   ── message:sent ──────────┐
//	                   └── validate   s2                        link  l1
//	service-b  instance b0                                            │
//	           └── POST /settle       s3   ── message:received ┘
//	                   └── log:error
//
// span_flags follow the model: chain roles are positional (1 own, 2 parent,
// 4 ancestor, 8 instance) and a link carries 16 and nothing else.
const seedSQL = `
TRUNCATE witness.records, witness.spans, witness.events;

-- witness.event_types is written by the observer at start-up from
-- core.Events(); this package seeds SQL directly, so it states the types the
-- seed uses. Error flags must match core.EventType.IsError().
INSERT INTO witness.event_types (event_type, event_type_name, is_error) VALUES
  (11,  'log:info',              false),
  (20,  'span:general:start',    false),
  (-20, 'span:general:finish',   false),
  (21,  'span:instance:online',  false),
  (-21, 'span:instance:offline', false),
  (24,  'span:message:sent',     false),
  (-24, 'span:message:received', false),
  (13,  'log:error',              true)
ON CONFLICT (event_type) DO UPDATE
SET event_type_name = excluded.event_type_name, is_error = excluded.is_error;

INSERT INTO witness.events (event_id, event_date, event_type, event_message, event_caller) VALUES
  ('00000000-0000-4000-8000-000000000001', now() - interval '10 s', 21,  'service-a',    'a.go:1'),
  ('00000000-0000-4000-8000-000000000002', now() - interval  '9 s', 20,  'POST /order',  'a.go:2'),
  ('00000000-0000-4000-8000-000000000003', now() - interval  '8 s', 11,  'validating',   'a.go:3'),
  ('00000000-0000-4000-8000-000000000004', now() - interval  '8 s', 20,  'validate',     'a.go:4'),
  ('00000000-0000-4000-8000-000000000005', now() - interval  '7 s', -20, 'validate',     'a.go:4'),
  ('00000000-0000-4000-8000-000000000006', now() - interval  '6 s', 24,  'call b',       'a.go:5'),
  ('00000000-0000-4000-8000-000000000011', now() - interval  '6 s', 21,  'service-b',    'b.go:1'),
  ('00000000-0000-4000-8000-000000000012', now() - interval  '5 s', 20,  'POST /settle', 'b.go:2'),
  ('00000000-0000-4000-8000-000000000013', now() - interval  '5 s', -24, 'call b',       'b.go:3'),
  ('00000000-0000-4000-8000-000000000014', now() - interval  '4 s', 13, 'ledger write failed' , 'b.go:4'),
  ('00000000-0000-4000-8000-000000000015', now() - interval  '3 s', -20, 'POST /settle', 'b.go:2'),
  ('00000000-0000-4000-8000-000000000016', now() - interval  '2 s', -21, 'service-b',    'b.go:1'),
  ('00000000-0000-4000-8000-000000000007', now() - interval  '2 s', -20, 'POST /order',  'a.go:2'),
  ('00000000-0000-4000-8000-000000000008', now() - interval  '1 s', -21, 'service-a',    'a.go:1');

INSERT INTO witness.spans (event_id, span_id, span_flags) VALUES
  -- service-a: a0 instance, s1 request, s2 nested
  ('00000000-0000-4000-8000-000000000001', '00000000-0000-4000-9000-0000000000aa',  9),
  ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-9000-0000000000aa', 10),
  ('00000000-0000-4000-8000-000000000002', '00000000-0000-4000-9000-000000000051',  1),
  ('00000000-0000-4000-8000-000000000003', '00000000-0000-4000-9000-0000000000aa', 10),
  ('00000000-0000-4000-8000-000000000003', '00000000-0000-4000-9000-000000000051',  1),
  ('00000000-0000-4000-8000-000000000004', '00000000-0000-4000-9000-0000000000aa', 12),
  ('00000000-0000-4000-8000-000000000004', '00000000-0000-4000-9000-000000000051',  2),
  ('00000000-0000-4000-8000-000000000004', '00000000-0000-4000-9000-000000000052',  1),
  ('00000000-0000-4000-8000-000000000005', '00000000-0000-4000-9000-0000000000aa', 12),
  ('00000000-0000-4000-8000-000000000005', '00000000-0000-4000-9000-000000000051',  2),
  ('00000000-0000-4000-8000-000000000005', '00000000-0000-4000-9000-000000000052',  1),
  ('00000000-0000-4000-8000-000000000006', '00000000-0000-4000-9000-0000000000aa', 10),
  ('00000000-0000-4000-8000-000000000006', '00000000-0000-4000-9000-000000000051',  1),
  ('00000000-0000-4000-8000-000000000006', '00000000-0000-4000-9000-0000000000cc', 16),
  ('00000000-0000-4000-8000-000000000007', '00000000-0000-4000-9000-0000000000aa', 10),
  ('00000000-0000-4000-8000-000000000007', '00000000-0000-4000-9000-000000000051',  1),
  ('00000000-0000-4000-8000-000000000008', '00000000-0000-4000-9000-0000000000aa',  9),
  -- service-b: b0 instance, s3 request
  ('00000000-0000-4000-8000-000000000011', '00000000-0000-4000-9000-0000000000bb',  9),
  ('00000000-0000-4000-8000-000000000012', '00000000-0000-4000-9000-0000000000bb', 10),
  ('00000000-0000-4000-8000-000000000012', '00000000-0000-4000-9000-000000000053',  1),
  ('00000000-0000-4000-8000-000000000013', '00000000-0000-4000-9000-0000000000bb', 10),
  ('00000000-0000-4000-8000-000000000013', '00000000-0000-4000-9000-000000000053',  1),
  ('00000000-0000-4000-8000-000000000013', '00000000-0000-4000-9000-0000000000cc', 16),
  ('00000000-0000-4000-8000-000000000014', '00000000-0000-4000-9000-0000000000bb', 10),
  ('00000000-0000-4000-8000-000000000014', '00000000-0000-4000-9000-000000000053',  1),
  ('00000000-0000-4000-8000-000000000015', '00000000-0000-4000-9000-0000000000bb', 10),
  ('00000000-0000-4000-8000-000000000015', '00000000-0000-4000-9000-000000000053',  1),
  ('00000000-0000-4000-8000-000000000016', '00000000-0000-4000-9000-0000000000bb',  9);

INSERT INTO witness.records (event_id, record_key, record_value) VALUES
  ('00000000-0000-4000-8000-000000000014', 'err', 'disk full');
`

// TestQueries runs every query builder against a real database seeded with a
// two-service trace. It is env-gated on WITNESS_TEST_DSN and expects
// 000_schema.up.sql plus monitors/grafana/views.up.sql to be applied.
//
// The queries are SQL strings, so nothing but running them catches a view
// rename or a column that no longer exists.
func TestQueries(t *testing.T) {
	dsn := os.Getenv("WITNESS_TEST_DSN")
	if dsn == "" {
		t.Skip("WITNESS_TEST_DSN not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, seedSQL); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var root string
	if err := pool.QueryRow(ctx,
		`SELECT root_span_id::text FROM witness.trace_roots`).Scan(&root); err != nil {
		t.Fatalf("trace_roots must yield exactly one root for the seeded trace: %v", err)
	}
	// The receiving side's span carries an inbound link, so it must not be a
	// root — one distributed request, one root.
	if root != "00000000-0000-4000-9000-000000000051" {
		t.Fatalf("root = %s, want the originating request span", root)
	}

	tr := backend.TimeRange{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)}

	for _, tc := range []struct {
		name    string
		want    int
		resp    func() backend.DataResponse
		wantMin bool
	}{
		{name: "traces", want: 1, resp: func() backend.DataResponse {
			return RunTraces(ctx, pool, &TracesReq{}, tr, 50)
		}},
		{name: "traces/service", want: 1, resp: func() backend.DataResponse {
			return RunTraces(ctx, pool, &TracesReq{Service: "service-a"}, tr, 50)
		}},
		{name: "traces/search", want: 1, resp: func() backend.DataResponse {
			return RunTraces(ctx, pool, &TracesReq{Search: "order"}, tr, 50)
		}},
		{name: "traces/no-match", want: 0, resp: func() backend.DataResponse {
			return RunTraces(ctx, pool, &TracesReq{Search: "nothing-here"}, tr, 50)
		}},
		// The walk crosses the process boundary: s1, s2 on service-a and s3
		// on service-b. Instance spans are not part of it.
		{name: "trace", want: 3, resp: func() backend.DataResponse {
			return RunTrace(ctx, pool, &Trace{RootSpanID: root})
		}},
		{name: "trace/traceID-alias", want: 3, resp: func() backend.DataResponse {
			return RunTrace(ctx, pool, &Trace{TraceID: root})
		}},
		// 2 service nodes + 1 edge.
		{name: "service-map", want: 3, resp: func() backend.DataResponse {
			return RunServiceMap(ctx, pool, &ServiceMap{RootSpanID: root})
		}},
		{name: "logs/trace", want: 2, resp: func() backend.DataResponse {
			return RunLogs(ctx, pool, &LogsReq{RootSpanID: root}, tr, 100)
		}},
		{name: "logs/service", want: 1, resp: func() backend.DataResponse {
			return RunLogs(ctx, pool, &LogsReq{RootSpanID: root, Service: "service-b"}, tr, 100)
		}},
		{name: "logs/span", want: 1, resp: func() backend.DataResponse {
			return RunLogs(ctx, pool, &LogsReq{SpanID: "00000000-0000-4000-9000-000000000053"}, tr, 100)
		}},
		// Events whose own span is in the component: s1 has 4, s2 has 2,
		// s3 has 4. The two instances' own events are not part of it — an
		// instance is a process, not a request.
		{name: "search/trace", want: 10, resp: func() backend.DataResponse {
			return RunSearch(ctx, pool, &Search{RootSpanID: root}, tr, 100)
		}},
		{name: "search/message", want: 1, resp: func() backend.DataResponse {
			return RunSearch(ctx, pool, &Search{Message: "ledger"}, tr, 100)
		}},
		{name: "search/record", want: 1, resp: func() backend.DataResponse {
			return RunSearch(ctx, pool, &Search{Records: []RecordFilter{{Key: "err", Value: "disk full"}}}, tr, 100)
		}},
		// span_flags & 1 tells "in this span" from "under it": s1 has 4 of
		// its own events, and does not absorb s2's or s3's.
		{name: "search/own-span", want: 4, resp: func() backend.DataResponse {
			return RunSearch(ctx, pool, &Search{
				SpanID:  "00000000-0000-4000-9000-000000000051",
				OwnOnly: true,
			}, tr, 100)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := tc.resp()
			if r.Error != nil {
				t.Fatalf("query failed: %v", r.Error)
			}
			got := 0
			for _, f := range r.Frames {
				got += f.Rows()
			}
			if got != tc.want {
				t.Errorf("rows = %d, want %d", got, tc.want)
			}
		})
	}
}

// batchSeedSQL is one process fanning n requests into a batch worker:
//
//	instance a0
//	├── POST /order  s1 ── message:sent ── l1 ─┐
//	├── POST /order  s2 ── message:sent ── l2 ─┤
//	└── worker       w                                  │
//	        └── batch    b ── message:received ┘ (both links)
//
// Both halves live in the same instance, which is exactly what link_edges
// used to exclude.
//
// s1's `sent` is written *after* the batch recorded receiving it, and s2's
// hand-off was retried on one msgID: neither may change what the views say.
const batchSeedSQL = `
TRUNCATE witness.records, witness.spans, witness.events;

-- witness.event_types is written by the observer at start-up from
-- core.Events(); this package seeds SQL directly, so it states the types the
-- seed uses. Error flags must match core.EventType.IsError().
INSERT INTO witness.event_types (event_type, event_type_name, is_error) VALUES
  (11,  'log:info',              false),
  (20,  'span:general:start',    false),
  (-20, 'span:general:finish',   false),
  (21,  'span:instance:online',  false),
  (-21, 'span:instance:offline', false),
  (24,  'span:message:sent',     false),
  (-24, 'span:message:received', false),
  (13,  'log:error',              true)
ON CONFLICT (event_type) DO UPDATE
SET event_type_name = excluded.event_type_name, is_error = excluded.is_error;

INSERT INTO witness.events (event_id, event_date, event_type, event_message, event_caller) VALUES
  ('00000000-0000-4000-8100-000000000001', now() - interval '9 s',  21,  'batcher',     'q.go:1'),
  ('00000000-0000-4000-8100-000000000002', now() - interval '8 s',  20,  'POST /order', 'q.go:2'),
  -- written *after* the worker recorded receiving it: the sent event is
  -- emitted once the send is acked, and event order is not guaranteed.
  ('00000000-0000-4000-8100-000000000003', now() - interval '2 s',  24,  'enqueue',     'q.go:3'),
  ('00000000-0000-4000-8100-000000000004', now() - interval '3 s', -20,  'POST /order', 'q.go:2'),
  ('00000000-0000-4000-8100-000000000005', now() - interval '8 s',  20,  'POST /order', 'q.go:2'),
  ('00000000-0000-4000-8100-000000000006', now() - interval '8 s',  24,  'enqueue',     'q.go:3'),
  ('00000000-0000-4000-8100-000000000007', now() - interval '7 s', -20,  'POST /order', 'q.go:2'),
  ('00000000-0000-4000-8100-000000000008', now() - interval '6 s',  20,  'settler',     'q.go:4'),
  ('00000000-0000-4000-8100-000000000009', now() - interval '5 s',  20,  'batch',       'q.go:5'),
  ('00000000-0000-4000-8100-00000000000a', now() - interval '5 s', -24,  'dequeue',     'q.go:6'),
  ('00000000-0000-4000-8100-00000000000b', now() - interval '4 s', -20,  'batch',       'q.go:5'),
  -- request 2's enqueue was retried on the same msgID: two sends, one
  -- receive, and still one edge.
  ('00000000-0000-4000-8100-00000000000e', now() - interval '7 s',  24,  'enqueue',     'q.go:3'),
  -- three spans with no start event of their own: one that only ever
  -- finished, one that only logged, one that only handed a message off.
  ('00000000-0000-4000-8100-00000000000f', now() - interval '4 s', -20,  'leftover',    'q.go:9'),
  ('00000000-0000-4000-8100-000000000010', now() - interval '4 s',  11,  'still here',  'q.go:10'),
  ('00000000-0000-4000-8100-000000000011', now() - interval '6 s',  24,  'enqueue',     'q.go:11');

INSERT INTO witness.spans (event_id, span_id, span_flags) VALUES
  ('00000000-0000-4000-8100-000000000001', '00000000-0000-4000-9100-0000000000aa',  9),
  -- request s1 and its hand-off
  ('00000000-0000-4000-8100-000000000002', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000002', '00000000-0000-4000-9100-000000000051',  1),
  ('00000000-0000-4000-8100-000000000003', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000003', '00000000-0000-4000-9100-000000000051',  1),
  ('00000000-0000-4000-8100-000000000003', '00000000-0000-4000-9100-0000000000c1', 16),
  ('00000000-0000-4000-8100-000000000004', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000004', '00000000-0000-4000-9100-000000000051',  1),
  -- request s2 and its hand-off
  ('00000000-0000-4000-8100-000000000005', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000005', '00000000-0000-4000-9100-000000000052',  1),
  ('00000000-0000-4000-8100-000000000006', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000006', '00000000-0000-4000-9100-000000000052',  1),
  ('00000000-0000-4000-8100-000000000006', '00000000-0000-4000-9100-0000000000c2', 16),
  ('00000000-0000-4000-8100-000000000007', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000007', '00000000-0000-4000-9100-000000000052',  1),
  -- worker w, then batch b under it, whose one event links both requests
  ('00000000-0000-4000-8100-000000000008', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000008', '00000000-0000-4000-9100-0000000000f0',  1),
  ('00000000-0000-4000-8100-000000000009', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-000000000009', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-000000000009', '00000000-0000-4000-9100-0000000000b0',  1),
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000b0',  1),
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000c1', 16),
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000c2', 16),
  ('00000000-0000-4000-8100-00000000000b', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-00000000000b', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-00000000000b', '00000000-0000-4000-9100-0000000000b0',  1),
  ('00000000-0000-4000-8100-00000000000e', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-00000000000e', '00000000-0000-4000-9100-000000000052',  1),
  ('00000000-0000-4000-8100-00000000000e', '00000000-0000-4000-9100-0000000000c2', 16),
  -- finish-only span fe, events-only span e0, both under the worker
  ('00000000-0000-4000-8100-00000000000f', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-00000000000f', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-00000000000f', '00000000-0000-4000-9100-0000000000fe',  1),
  ('00000000-0000-4000-8100-000000000010', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-000000000010', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-000000000010', '00000000-0000-4000-9100-0000000000e0',  1),
  -- s3 never emitted a start either, and hands off on link c3
  ('00000000-0000-4000-8100-000000000011', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000011', '00000000-0000-4000-9100-000000000053',  1),
  ('00000000-0000-4000-8100-000000000011', '00000000-0000-4000-9100-0000000000c3', 16),
  -- ... which the same fan-in event took in
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000c3', 16);
`

// TestBatchFanIn checks the in-process queue shape end to end in SQL: the
// fan-in event produces one link edge per request, and each request's trace
// walk reaches the batch span. Both fail if link_edges goes back to requiring
// two different instances.
func TestBatchFanIn(t *testing.T) {
	dsn := os.Getenv("WITNESS_TEST_DSN")
	if dsn == "" {
		t.Skip("WITNESS_TEST_DSN not set; skipping integration test")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, batchSeedSQL); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const (
		s1 = "00000000-0000-4000-9100-000000000051"
		s2 = "00000000-0000-4000-9100-000000000052"
		b  = "00000000-0000-4000-9100-0000000000b0"
	)

	var edges int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM witness.link_edges WHERE to_span_id = $1`, b).Scan(&edges); err != nil {
		t.Fatalf("link_edges: %v", err)
	}
	if edges != 3 {
		t.Errorf("link edges into the batch span = %d, want one per request", edges)
	}
	// Request 2 sent twice on one msgID; the retry must not double its edge,
	// and the edge must date from the first attempt, not the last.
	var rowsFor2 int
	var from, to time.Time
	if err := pool.QueryRow(ctx,
		`SELECT count(*), min(from_at), min(to_at) FROM witness.link_edges
		  WHERE from_span_id = $1 AND to_span_id = $2`, s2, b).Scan(&rowsFor2, &from, &to); err != nil {
		t.Fatalf("link_edges: %v", err)
	}
	if rowsFor2 != 1 {
		t.Errorf("retried hand-off = %d edges, want 1 per pair of spans", rowsFor2)
	}
	// ... dated from the first attempt, so the edge measures the whole wait.
	if want := 3 * time.Second; to.Sub(from) < want {
		t.Errorf("edge covers %v, want at least %v — it starts at the retry, not the first send", to.Sub(from), want)
	}

	// Both request spans gave work away and took none: they are roots. The
	// batch span took work, so it is not one — whatever the order the events
	// were written in.
	var roots []string
	rows, err := pool.Query(ctx, `SELECT root_span_id::text FROM witness.trace_roots ORDER BY started_at`)
	if err != nil {
		t.Fatalf("trace_roots: %v", err)
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan: %v", err)
		}
		roots = append(roots, id)
	}
	rows.Close()
	var seen = map[string]bool{}
	for _, r := range roots {
		seen[r] = true
	}
	if !seen[s1] || !seen[s2] {
		t.Errorf("trace_roots = %v, want both request spans", roots)
	}
	if seen[b] {
		t.Errorf("the batch span received without sending first; it is not a root")
	}

	// Spans with no start event are still spans: one that only finished, one
	// that only logged, one that only handed a message off. All three must
	// appear, and the finish-only one takes its name from its finish event.
	const (
		fe = "00000000-0000-4000-9100-0000000000fe"
		e0 = "00000000-0000-4000-9100-0000000000e0"
		s3 = "00000000-0000-4000-9100-000000000053"
	)
	for _, tc := range []struct{ span, wantName string }{
		{fe, "leftover"},
		{e0, ""},
		{s3, ""},
	} {
		var name *string
		if err := pool.QueryRow(ctx,
			`SELECT span_name FROM witness.span_pairs WHERE span_id = $1`, tc.span).Scan(&name); err != nil {
			t.Errorf("span_pairs must hold %s even with no start event: %v", tc.span, err)
			continue
		}
		if got := ""; name != nil && *name != tc.wantName || name == nil && tc.wantName != got {
			t.Errorf("span_pairs.span_name for %s = %v, want %q", tc.span, name, tc.wantName)
		}
	}

	// Parenthood comes off any event, not off a start event.
	for _, tc := range [][2]string{{"00000000-0000-4000-9100-0000000000f0", fe},
		{"00000000-0000-4000-9100-0000000000f0", e0}} {
		var ok bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM witness.span_children
			                 WHERE parent_span_id = $1 AND child_span_id = $2)`,
			tc[0], tc[1]).Scan(&ok); err != nil {
			t.Fatalf("span_children: %v", err)
		}
		if !ok {
			t.Errorf("span_children misses %s -> %s: the child has no start event", tc[0], tc[1])
		}
	}

	// A sender with no start event still names its service, so its edge
	// survives the service map's NOT NULL filter.
	var svc *string
	if err := pool.QueryRow(ctx,
		`SELECT from_service_name FROM witness.link_edges WHERE from_span_id = $1`, s3).Scan(&svc); err != nil {
		t.Fatalf("link_edges from a start-less sender: %v", err)
	}
	if svc == nil {
		t.Error("from_service_name is NULL for a span with no start event")
	}

	for _, root := range []string{s1, s2} {
		var reached bool
		if err := pool.QueryRow(ctx, traceWalkCTE+
			`SELECT EXISTS (SELECT 1 FROM trace_spans WHERE span_id = $2)`,
			[]string{root}, b).Scan(&reached); err != nil {
			t.Fatalf("walk from %s: %v", root, err)
		}
		if !reached {
			t.Errorf("walk from request %s does not reach the batch span", root)
		}
	}
}
