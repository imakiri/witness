# Storage: what the current schema costs, and what the alternatives are

Written after measuring the v0.31 schema under the `testenv` load (two services,
140 requests/second, ~10 minutes of traffic). Everything numbered here was run,
not estimated; where a number is inflated by how it was produced, it says so.

## 1. What we measured

Volumes after ~10 minutes:

| table | rows | note |
|---|---|---|
| `witness.spans` | 1 189 541 | 2.36 rows per event — the chain, one row per (event, span) |
| `witness.events` | 503 652 | |
| `witness.records` | 276 613 | |
| spans (distinct) | 92 853 | 5.4 events per span |

Sizes were measured after a full-table `UPDATE` (used to age rows out of a time
window for the benchmark), which rewrites every row, so on-disk figures were up
to 2x inflated and are not quoted here. The row counts are exact, and the ratio
is the load-bearing one: **the chain costs 2.4 rows per event**, and it is the
largest table in the schema.

Query costs on that database, 503k events:

| what | time |
|---|---|
| every event of one span (`spans` → `events`, by id) | **0.074 ms** |
| one span through `witness.span_starts` | **223 ms** |
| the same span from a materialised projection | **0.072 ms** |
| event trail (causal cone), 60-minute window | **3.1 s** |
| event trail, unbounded | **3.7–4.4 s** |

The layout is excellent at what it was designed for and catastrophic at
everything derived from it — a factor of **3000** between the raw lookup and the
same fact read through a view. Nothing about that is Postgres's fault:
`span_starts` is `DISTINCT ON (span_id)` over every lifecycle event in the
database, and a predicate on one span does not push through it.

## 2. Where the trail's 3 seconds go

Self time of the heaviest nodes, 60-minute window:

```
793 ms  Materialize
733 ms  Nested Loop
709 ms  CTE Scan on back_edges      (17 069 rows)
504 ms  Index Scan on spans
350 ms  Seq Scan on events          (30 910 rows)
224 ms  Seq Scan on spans           (15 713 rows)
```

`back_edges` is the whole of the edge set inside the window, built from scratch
on every request, so that a walk can then touch about a hundred rows of it. The
denormalised `event_date` (added in this round) cuts what is scanned to the
window — measurably: `spans c` drops from 136k rows to 15k — but the shape of
the work does not change. **Edges and span facts are recomputed per query
because nothing stores them.** That, not the storage layout, is what costs the
three seconds.

## 3. The access patterns that have to be fast

From the view API, in the order they are asked:

1. **Find an event** — by record key/value, date, message substring, event type,
   or span. Today: indexed and fast, except when the answer must carry a span
   name or a service, which are derived (the 223 ms).
2. **Every event of a span**, where the span is one step of a procedure (an API
   request, a batch). Today: 0.074 ms. This is the pattern the schema is built
   for.
3. **The causal cone of an event**, strictly backwards. Today: 3.1 s.
4. **A trace as a waterfall.** Same problem as 3, smaller.
5. **Archive and retention.** At this rate the tables grow by hundreds of
   millions of rows per day. Nothing in the schema drops anything.

## 4. Schema alternatives, same engine

### A. What we have, plus the time window (done)

Events, spans (chain), records; everything above a raw event is a view.
Bounded walks made the scans proportional to the window instead of the table.
Keeps every property of the model. Leaves the 3 s.

### B. Derived projections: `span_proj` and `span_edges`

One row per span (instance, parent, name, start, finish, service) and one row
per edge (parent or hand-off, with its two events). Measured, on the same
database: **92 853 rows, 15 MB, built from the views in 3.4 seconds, looked up
in 0.072 ms**. A full rebuild of the entire projection costs less than one
trail query costs today.

The cone then stops being a query over the raw tables and becomes a graph walk
over an indexed edge table — hundreds of rows touched instead of tens of
thousands.

**This does not weaken the model.** Events remain the only truth; the
projection is a cache that can be dropped and rebuilt at any time, and nothing
writes to it except the process that derives it from events. The rule to keep
is the one that already governs `span_flags`: derived, never authoritative.

Open questions: maintained incrementally by the observer (it knows the span
roles at write time) or refreshed by a job; what happens to a span whose finish
arrives after the refresh (answer: the projection is late, not wrong — events
are independent and a span with no finish is legal).

### C. Flatten the chain onto the event

`witness.spans` exists because an event names N spans. But of those N, three are
singletons — own, parent, instance — and the rest are rare: ancestors (depth
above the parent) and links (a hand-off, usually one, occasionally n for a
fan-in). So:

```
events(event_id, event_date, event_type, event_message, event_caller,
       own_span_id, parent_span_id, instance_span_id,
       ancestor_span_ids uuid[], link_span_ids uuid[])
```

- "every event of a span" — one index on one table, no join;
- "children of a span" — `parent_span_id = X`, direct, no `span_flags & 2`;
- "the instance of an event" — a column, not a join;
- 1.19M rows of `spans` disappear; only fan-in links stay in an array.

Costs: `link_span_ids` needs GIN, a fan-in event stays awkward, and every view
is rewritten. This is the "one table with everything" option, and the reason it
is attractive is not that joins are slow — it is that **the chain is a fixed
shape wearing a variable-length costume**.

### D. Records as `jsonb` on the event

276k rows and a table become one column. Search by key/value goes to a GIN
index on `jsonb_path_ops`. Loses the trigram index on record values (an
expression index brings it back for one key at a time). Worth doing only
together with C, and only if record search stays as coarse as it is now.

### B, measured

Built on the benchmark database (503 652 events, 92 853 spans), after a
`VACUUM FULL` so the base figures are honest:

| | rows | size |
|---|---|---|
| `witness.events` | 503 652 | 114 MB |
| `witness.spans` | 1 189 541 | 264 MB |
| `witness.records` | 276 613 | 50 MB |
| **base total** | | **428 MB** |
| `witness.span_proj` | 92 853 | 20 MB |
| `witness.span_edges` | 182 329 | 33 MB |
| **cache total** | | **53 MB — 12.4% of base** |

Per row: 891 B per event of base, 231 B per span and 188 B per edge of cache,
1.96 edges per span. So the ratio is not a constant, it is
`570 B / (891 B × events-per-span)`: **12% at the 5.4 events per span this
workload produces, ~32% at 2 events per span, ~6% at 10**. The cache scales
with the *shape* of the traces, not with their volume.

Full rebuild from the views: **5.6 s** for the whole database (~11 s per
million events). Incremental maintenance is cheaper still and is what the
observer would do.

The trail rewritten against the two tables, same seed, same 60-minute window:

```
views:        3145 ms
projections:     2.5 ms      -- 1250x, and the same 238 rows, event for event
```

The result sets were diffed and are identical, so this is a pure change of
where the work happens: the edge set is read instead of rebuilt, and the span
facts are a primary-key lookup instead of a `DISTINCT ON` over the table.

**Recommendation: B first** — it is small, rebuildable, needs no migration of
existing data, and it is the only one of the four that addresses the measured
problem. C is the bigger win on volume and the bigger change; do it when the
model has settled. D is optional.

### B, paid at insert

The projection does not have to be a job. Everything it holds is derivable
from the event being written, and the observer already knows all of it: the
chain, the roles, the type, the date. Measured on a 1024-event batch — what
the observer actually ships — against a cache that already holds 92 853 spans
and 182 329 edges, so every row takes the `ON CONFLICT` path:

```
base write   events 10.6 ms + spans 10.2 ms + records 5.7 ms  = 26.5 ms
cache merge  span_proj 10.6 + span_edges 6.7 + link_sides 7.0 = 24.3 ms
```

**The write roughly doubles**: +24 µs per event, ~3% of a core at the 1200
events/second this demo produces, ~24% at 10 000/second. Against 3145 ms → 2.5 ms
on the read.

Three statements per **batch**, not per event — and that is a precondition
worth naming: the observer currently queues one statement per event, per span
row and per record, so a 1024-event batch is several thousand statements. The
merge is written against the batch as a set, so the write path wants to become
one multi-row `INSERT` per table first. That is a win on its own.

**The merges are commutative, which is what makes this correct under witness's
model**: `first_at` is a `least`, `last_at` a `greatest`, the start the
earliest and the finish the latest — and where an id accompanies a date the
two are chosen together by comparing `(date, id)` as a tuple, never column by
column, because a per-column `coalesce` keeps whichever batch arrived first
and would pair one event's date with another's id. A start arriving
after its finish, a finish that never arrives, a `sent` written after the
matching `received` — none of them change the result, because none of the
merges depends on order. Only `event_count` is non-idempotent: a batch applied
twice would double it. The observer never retries a failed batch today, but
that is the one field to think about before it does.

The two halves of a hand-off arrive independently and may be hours apart, so
`span_link_sides` is upserted per half and the *edge* stays a join on
`link_span_id`. Materialising the edge at write would mean reading the other
half during the write, and it may not exist yet.

### The cache under Timescale, measured

Partition the cache by **the span's own creation time, taken from the uuid v7
in `span_id`** — not by `first_at`. `first_at` moves when a late event arrives,
which would move the row to another chunk and break its identity; the v7
timestamp is fixed when the span is created and is already in the key. In
PG16 it is one expression (`uuid_extract_timestamp()` from PG18 onwards):

```sql
to_timestamp((('x' || substr(replace(span_id::text,'-',''),1,12))::bit(48)::bigint) / 1000.0)
```

On TimescaleDB 2.29 / PG 16, all of it works:

- hypertable on `span_proj` with `PRIMARY KEY (span_started, span_id)` — the
  partitioning column is in the key, which is what Timescale requires;
- `INSERT … ON CONFLICT DO UPDATE` merges across chunks, including a late
  event moving `first_at` backwards;
- `drop_chunks` retires old cache chunks, so retention runs on the cache in
  step with the events it summarises;
- compression works on cache chunks, **and an upsert into an already compressed
  chunk still merges correctly** — which matters, because a late event is legal
  in this model at any distance.

That makes the cache a first-class, time-partitioned, archivable artefact
rather than a rebuild-on-demand cache. And it is what makes an *archived* cone
possible at all: the edges are stored, so a cold Parquet partition can be
walked without re-deriving anything.

## 5. Engines

### Postgres, as now

Everything above applies. Timescale was spiked separately (see CLAUDE.md): it
gives retention and compression, but not query speed for our shapes — the hot
queries are id lookups and graph walks with no time predicate, and a continuous
aggregate cannot express our views (no `DISTINCT ON`, one hypertable per view).

### DuckDB

Right tool, wrong position in the pipeline. It is columnar, embedded, reads
Parquet directly, supports `WITH RECURSIVE`, and would answer "search all events
by record/message/date" over a month of archive far faster and far smaller than
Postgres does. But it is **single-writer, single-process**: the observer writes
continuously from several services while Grafana reads. That is exactly the
workload DuckDB does not do.

The shape that works: **hot in Postgres, cold in Parquet, queried by DuckDB.**
Events older than the retention window are exported to date-partitioned Parquet
and dropped from Postgres; the archive is immutable, which is the one thing our
data model guarantees. The cone crosses the boundary badly (a walk that leaves
the hot window would have to continue in the archive), so the honest rule is:
**the cone lives in the hot window, search lives in both**.

### ClickHouse

Would swallow this ingest rate without noticing, compresses far better than
Postgres, and is strong at exactly pattern 1 (search) and 2 (events of a span).
Weak where we are weakest: recursive walks (recursive CTEs are young), joins,
and any incremental maintenance of a projection. Choosing it would mean
committing to materialised edges (option B) as the only way the cone can work —
which is where we are heading anyway.

## 6. What was built

Option B, paid at insert, is in the schema and the observer:

- `witness.span_facts`, `witness.span_edges` and `witness.merge_span_cache()` /
  `witness.rebuild_span_cache()` in `observers/postgres/000_schema.up.sql`;
- the observer writes each batch as three multi-row inserts plus the merge, in
  one `pgx.Batch`;
- `queries.RunEventTrail` walks `span_edges` and reads names from `span_facts`.

Measured on the same 503 652-event database after the rewrite: **cache 65 MB
against 428 MB of base tables (15.2%)**, rebuild 5 s, and the trail **2.4 ms**
returning the same 238 rows the view-based query returned in 3145 ms.

`span_edges` is indexed backwards only (`to_span_id`), because the walk that
exists goes backwards; a forward index costs 13 MB per 180k edges and nothing
reads it yet.

`witness.link_sides` was designed and then dropped: keeping the two halves of a
hand-off as their own table added 55 MB and bought nothing, because the second
half can find the first through `witness.spans` by the link's span_id, which is
an index hit. The edge is written by whichever half arrives second.

## 7. What to measure next

A spike in the shape of the Timescale one, on the benchmark database that
already exists:

1. Build `span_proj` and `span_edges`, rewrite the trail against them, and
   measure. Expectation from the numbers above: milliseconds, not seconds. If
   it is not at least 10x, option B is wrong and C is the answer.
2. Export one day of events to Parquet, query it with DuckDB for pattern 1, and
   compare against the same query in Postgres. That decides whether the archive
   needs its own engine or is just cold Postgres partitions.
