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
//	           └── POST /order        s1   ── external_message:sent ──┐
//	                   └── validate   s2                        link  l1
//	service-b  instance b0                                            │
//	           └── POST /settle       s3   ── external_message:received ┘
//	                   └── log:error:storage
//
// span_flags follow the model: chain roles are positional (1 own, 2 parent,
// 4 ancestor, 8 instance) and a link carries 16 and nothing else.
const seedSQL = `
TRUNCATE witness.records, witness.spans, witness.events;

INSERT INTO witness.events (event_id, event_date, event_type, event_message, event_caller) VALUES
  ('00000000-0000-4000-8000-000000000001', now() - interval '10 s', 21,  'service-a',    'a.go:1'),
  ('00000000-0000-4000-8000-000000000002', now() - interval  '9 s', 20,  'POST /order',  'a.go:2'),
  ('00000000-0000-4000-8000-000000000003', now() - interval  '8 s', 11,  'validating',   'a.go:3'),
  ('00000000-0000-4000-8000-000000000004', now() - interval  '8 s', 20,  'validate',     'a.go:4'),
  ('00000000-0000-4000-8000-000000000005', now() - interval  '7 s', -20, 'validate',     'a.go:4'),
  ('00000000-0000-4000-8000-000000000006', now() - interval  '6 s', 25,  'call b',       'a.go:5'),
  ('00000000-0000-4000-8000-000000000011', now() - interval  '6 s', 21,  'service-b',    'b.go:1'),
  ('00000000-0000-4000-8000-000000000012', now() - interval  '5 s', 20,  'POST /settle', 'b.go:2'),
  ('00000000-0000-4000-8000-000000000013', now() - interval  '5 s', -25, 'call b',       'b.go:3'),
  ('00000000-0000-4000-8000-000000000014', now() - interval  '4 s', 103, 'ledger write failed', 'b.go:4'),
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
