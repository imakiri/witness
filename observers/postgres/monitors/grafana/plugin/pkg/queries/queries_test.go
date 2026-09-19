package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
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

-- event_date is denormalised on witness.spans; the seed takes it from the
-- events row so the two cannot drift apart here either.
INSERT INTO witness.spans (event_id, event_date, span_id, span_flags)
SELECT v.event_id::uuid, e.event_date, v.span_id::uuid, v.span_flags
  FROM (VALUES
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
  ('00000000-0000-4000-8000-000000000016', '00000000-0000-4000-9000-0000000000bb',  9)
  ) AS v(event_id, span_id, span_flags)
  JOIN witness.events e ON e.event_id = v.event_id::uuid;

INSERT INTO witness.records (event_id, event_date, record_key, record_value)
SELECT v.event_id::uuid, e.event_date, v.record_key, v.record_value
  FROM (VALUES
  ('00000000-0000-4000-8000-000000000014', 'err', 'disk full'),
  -- On the received event, which is where witness.Handle puts the records
  -- it is given: they are span attributes all the same.
  ('00000000-0000-4000-8000-000000000013', 'order_id', 'o-1')
  ) AS v(event_id, record_key, record_value)
  JOIN witness.events e ON e.event_id = v.event_id::uuid;

-- The writer maintains the derived cache; a seed that writes rows straight
-- into the tables has to fold them in the same way, or every query reading
-- witness.span_facts / witness.span_edges sees an empty database.
SELECT witness.rebuild_span_cache();
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

	// Span attributes come from three events, not one: the start, the finish
	// and the span's own message:received. Records passed to witness.Handle
	// land on the last of those and exist nowhere else, so reading only the
	// lifecycle events left every Handle-opened span with an empty
	// attributes panel.
	t.Run("trace/tags-include-received", func(t *testing.T) {
		r := RunTrace(ctx, pool, &Trace{RootSpanID: root})
		if r.Error != nil {
			t.Fatalf("query failed: %v", r.Error)
		}
		tags, ok := spanTags(t, r, "00000000-0000-4000-9000-000000000053")
		if !ok {
			t.Fatal("receiving span not in the trace")
		}
		if tags["order_id"] != "o-1" {
			t.Errorf("tags = %v, want order_id from the received event", tags)
		}
	})
}

// frameField looks a field up by name: a frame's column order is Grafana's
// contract, not this test's.
func frameField(t *testing.T, f *data.Frame, name string) *data.Field {
	t.Helper()
	for _, fl := range f.Fields {
		if fl.Name == name {
			return fl
		}
	}
	t.Fatalf("frame %q has no %q field", f.Name, name)
	return nil
}

// spanTags digs the tags of one span out of a trace frame.
func spanTags(t *testing.T, r backend.DataResponse, spanID string) (map[string]string, bool) {
	t.Helper()
	for _, f := range r.Frames {
		ids, tags := frameField(t, f, "spanID"), frameField(t, f, "tags")
		for i := 0; i < f.Rows(); i++ {
			if id, _ := ids.At(i).(string); id != spanID {
				continue
			}
			raw, _ := tags.At(i).(json.RawMessage)
			var kvs []tagKV
			if err := json.Unmarshal(raw, &kvs); err != nil {
				t.Fatalf("tags: %v", err)
			}
			out := make(map[string]string, len(kvs))
			for _, kv := range kvs {
				out[kv.Key] = fmt.Sprint(kv.Value)
			}
			return out, true
		}
	}
	return nil, false
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
  ('00000000-0000-4000-8100-000000000011', now() - interval '6 s',  24,  'enqueue',     'q.go:11'),
  -- An event of the instance span itself, after the requests started and
  -- before the worker did: it separates the two cuts the instance is
  -- reached with, which is what pins "earliest cut wins".
  ('00000000-0000-4000-8100-000000000012', now() - interval '7 s',  11,  'instance noise', 'q.go:12'),
  -- Older than the trail's default window, on the instance span: the cone
  -- is cut in time, and this is the event that proves the cut happens.
  ('00000000-0000-4000-8100-000000000013', now() - interval '2 h',   11,  'ancient',        'q.go:13'),
  -- A long-lived span that handed the batch work two hours ago and is still
  -- logging now: the edge is older than the window, the span's own events are
  -- not. Only the bound on the *edge* keeps it out of the cone.
  ('00000000-0000-4000-8100-000000000014', now() - interval '2 h',   24,  'ancient enqueue', 'q.go:14'),
  ('00000000-0000-4000-8100-000000000015', now() - interval '2 h',  -24,  'ancient enqueue', 'q.go:14'),
  ('00000000-0000-4000-8100-000000000016', now() - interval '5 s',   11,  'still alive',     'q.go:15');

-- event_date is denormalised on witness.spans; the seed takes it from the
-- events row so the two cannot drift apart here either.
INSERT INTO witness.spans (event_id, event_date, span_id, span_flags)
SELECT v.event_id::uuid, e.event_date, v.span_id::uuid, v.span_flags
  FROM (VALUES
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
  ('00000000-0000-4000-8100-00000000000a', '00000000-0000-4000-9100-0000000000c3', 16),
  ('00000000-0000-4000-8100-000000000012', '00000000-0000-4000-9100-0000000000aa',  9),
  ('00000000-0000-4000-8100-000000000013', '00000000-0000-4000-9100-0000000000aa',  9),
  -- the ancient sender d0, its hand-off on link c9, and the batch taking it
  ('00000000-0000-4000-8100-000000000014', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000014', '00000000-0000-4000-9100-0000000000d0',  1),
  ('00000000-0000-4000-8100-000000000014', '00000000-0000-4000-9100-0000000000c9', 16),
  ('00000000-0000-4000-8100-000000000015', '00000000-0000-4000-9100-0000000000aa', 12),
  ('00000000-0000-4000-8100-000000000015', '00000000-0000-4000-9100-0000000000f0',  2),
  ('00000000-0000-4000-8100-000000000015', '00000000-0000-4000-9100-0000000000b0',  1),
  ('00000000-0000-4000-8100-000000000015', '00000000-0000-4000-9100-0000000000c9', 16),
  ('00000000-0000-4000-8100-000000000016', '00000000-0000-4000-9100-0000000000aa', 10),
  ('00000000-0000-4000-8100-000000000016', '00000000-0000-4000-9100-0000000000d0',  1)
  ) AS v(event_id, span_id, span_flags)
  JOIN witness.events e ON e.event_id = v.event_id::uuid;

-- The writer maintains the derived cache; a seed that writes rows straight
-- into the tables has to fold them in the same way, or every query reading
-- witness.span_facts / witness.span_edges sees an empty database.
SELECT witness.rebuild_span_cache();
`

// TestEventTrail walks the causal cone backwards from one event, on the
// fan-in seed: it has a hand-off written out of order, a retried send and
// three spans with no start event, which are the shapes the cut rules have
// to survive.
func TestEventTrail(t *testing.T) {
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
		instance   = "00000000-0000-4000-9100-0000000000aa"
		worker     = "00000000-0000-4000-9100-0000000000f0"
		batch      = "00000000-0000-4000-9100-0000000000b0"
		s1         = "00000000-0000-4000-9100-000000000051"
		s2         = "00000000-0000-4000-9100-000000000052"
		s3         = "00000000-0000-4000-9100-000000000053"
		ancient    = "00000000-0000-4000-9100-0000000000d0"
		batchStart = "00000000-0000-4000-8100-000000000009"
		batchFinis = "00000000-0000-4000-8100-00000000000b"
		fanIn      = "00000000-0000-4000-8100-00000000000a"
		s1Sent     = "00000000-0000-4000-8100-000000000003"
	)

	// From the batch's finish: its own span, the worker above it, the
	// instance above that, and every request that handed it work. The
	// worker's *other* children — a finish-only span and an events-only one
	// — are siblings, not causes, and must not appear: the walk only ever
	// goes up and back.
	t.Run("fan-in", func(t *testing.T) {
		trail := RunEventTrail(ctx, pool, &EventTrail{EventID: batchFinis}, 500)
		spans, byspan := trailSpans(t, trail)
		want := map[string]bool{instance: true, worker: true, batch: true, s1: true, s2: true, s3: true}
		if !sameSet(spans, want) {
			t.Errorf("spans = %v, want %v", spans, want)
		}
		// s1 handed off last (its `sent` is written after the batch already
		// recorded the receive), so its cut is that send: start, finish and
		// the send itself.
		if got := byspan[s1]; got != 3 {
			t.Errorf("events of s1 = %d, want 3", got)
		}
		// s2 retried on one msgID. The cut is the *first* attempt, the same
		// event link_edges dates the hand-off from, so the retry and the
		// finish after it are outside the cone.
		if got := byspan[s2]; got != 2 {
			t.Errorf("events of s2 = %d, want the start and the first send", got)
		}
		// The instance is reached twice: up from the worker (cut: the
		// worker's start) and up from a request that fed the batch (cut:
		// that request's start, which is earlier). The earliest cut wins, so
		// the instance's own event in between is not a cause of ours — only
		// the online event is.
		if got := byspan[instance]; got != 1 {
			t.Errorf("events of the instance = %d, want only its online event", got)
		}
		// A span is named by its lifecycle events and nothing else. Reading
		// the whole 20..29 range instead would name a request span after the
		// message it handed off, span:message:sent being 24.
		if got := trailName(t, trail, s1); got != "POST /order" {
			t.Errorf("span name = %q, want the name of the span, not of its hand-off", got)
		}
		// The edge each span was reached by, which is what a view draws the
		// hand-off with. For a link: the giving event and the receiving one.
		// For a parent: the child's start event and no event on the parent's
		// side, because opening a child emits nothing there.
		if row := trailRow(t, trail, s1); row["viaSpanID"] != batch ||
			row["edgeFromEventID"] != s1Sent || row["edgeToEventID"] != fanIn {
			t.Errorf("link edge of s1 = %v", row)
		}
		if row := trailRow(t, trail, worker); row["viaSpanID"] != batch ||
			row["edgeFromEventID"] != "" || row["edgeToEventID"] != batchStart {
			t.Errorf("parent edge of the worker = %v", row)
		}
	})

	// The window is a cut, not a hint: an event older than it is not
	// reported even though it is a cause. Widen the window and it comes
	// back. Without the bound the walk reads the whole database, which is
	// what witness.spans.event_date was denormalised to avoid.
	t.Run("window", func(t *testing.T) {
		spans, byspan := trailSpans(t, RunEventTrail(ctx, pool, &EventTrail{EventID: batchFinis}, 500))
		if got := byspan[instance]; got != 1 {
			t.Errorf("events of the instance = %d, want the ancient one cut away", got)
		}
		if spans[ancient] {
			t.Error("an edge older than the window was followed; the span on its far end is still alive, which is exactly why the bound is on the edge and not only on the events")
		}
		wide := RunEventTrail(ctx, pool, &EventTrail{EventID: batchFinis, SinceMinutes: 180}, 500)
		wideSpans, _ := trailSpans(t, wide)
		if !wideSpans[ancient] {
			t.Error("the ancient sender is missing from a 3h window, where its hand-off is inside")
		}
		// Widening the window does not just add rows, it moves the cut: the
		// instance is now also reached through a span that started two hours
		// ago, and the earliest cut wins, so what the instance contributes is
		// its ancient event rather than the online one.
		if got := trailRow(t, wide, instance)["message"]; got != "ancient" {
			t.Errorf("the instance contributes %q in a 3h window, want the event before the earliest cut", got)
		}
	})

	// From the batch's *start*, one event earlier: the fan-in receive has
	// not happened yet at that cut, so no request is a cause of it. This is
	// the guard on the link hop — without it every message a span ever took
	// would be dragged in, whenever it arrived.
	t.Run("before-the-receive", func(t *testing.T) {
		spans, _ := trailSpans(t, RunEventTrail(ctx, pool, &EventTrail{EventID: batchStart}, 500))
		want := map[string]bool{instance: true, worker: true, batch: true}
		if !sameSet(spans, want) {
			t.Errorf("spans = %v, want only the chain above the batch", spans)
		}
	})
}

// trailSpans reduces a trail frame to the set of spans it covers and the
// number of events it returned for each.
func trailSpans(t *testing.T, r backend.DataResponse) (map[string]bool, map[string]int) {
	t.Helper()
	if r.Error != nil {
		t.Fatalf("query failed: %v", r.Error)
	}
	spans, counts := map[string]bool{}, map[string]int{}
	for _, f := range r.Frames {
		ids := frameField(t, f, "spanID")
		for i := 0; i < f.Rows(); i++ {
			id, _ := ids.At(i).(string)
			spans[id] = true
			counts[id]++
		}
	}
	return spans, counts
}

// trailRow returns the string fields of the first trail row for one span.
func trailRow(t *testing.T, r backend.DataResponse, spanID string) map[string]string {
	t.Helper()
	for _, f := range r.Frames {
		ids := frameField(t, f, "spanID")
		for i := 0; i < f.Rows(); i++ {
			if id, _ := ids.At(i).(string); id != spanID {
				continue
			}
			out := map[string]string{}
			for _, fl := range f.Fields {
				if v, ok := fl.At(i).(string); ok {
					out[fl.Name] = v
				}
			}
			return out
		}
	}
	return nil
}

// trailName returns the span name the trail reports for one span.
func trailName(t *testing.T, r backend.DataResponse, spanID string) string {
	t.Helper()
	for _, f := range r.Frames {
		ids, names := frameField(t, f, "spanID"), frameField(t, f, "spanName")
		for i := 0; i < f.Rows(); i++ {
			if id, _ := ids.At(i).(string); id == spanID {
				n, _ := names.At(i).(string)
				return n
			}
		}
	}
	return ""
}

func sameSet(got, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}

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
	// Three requests, plus the long-lived span that handed the batch work two
	// hours ago — an edge is an edge however old it is; it is the *walk* that
	// bounds itself in time, not the view.
	if edges != 4 {
		t.Errorf("link edges into the batch span = %d, want one per sender", edges)
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
