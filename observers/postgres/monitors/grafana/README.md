# Witness — Grafana monitor

SQL views + a Grafana dashboard for exploring witness spans and logs that
land in the Postgres observer.

This is a **dashboard-based** view of trace data, not a flame chart. Grafana's
native trace UI is wired to Tempo/Jaeger/Zipkin, not PostgreSQL. What you get
here is: pick a span_id, see all events that happened inside it, jump to its
direct children, navigate from recent root spans.

## Prerequisites

- Witness Postgres observer is configured and writing to the `witness` schema
  (see `../../migration.up.sql`).
- A Grafana instance with the **PostgreSQL** data source enabled.
- The Postgres role used by Grafana has `USAGE` on the `witness` schema and
  `SELECT` on the views below.

## Install

1. Apply the views:

   ```sh
   psql "$WITNESS_DB_URL" -f views.up.sql
   ```

   `views.up.sql` is idempotent — safe to re-run. `views.down.sql` removes
   them.

2. In Grafana, add a PostgreSQL data source pointing at the witness DB.

3. Import `dashboard.json`. At import time, Grafana will prompt for the
   `DS_POSTGRES` data source — pick the one you just added.

## Use

The dashboard takes one variable: **Span ID**. Workflow:

1. Look at *Recent root spans* (bottom right). Each row is a span that has
   no parent in the witness graph — typically an instance/service/main root.
2. Copy a `Span ID` from that table into the **Span ID** variable at the top.
3. The rest of the dashboard scopes to that span:
   - **Selected span duration** — `finished_at - started_at`, NULL if the
     span hasn't closed.
   - **Events under span** — count of all events whose `event_span_ids`
     contain this span_id, in the current time range.
   - **Errors under span** — same scope, filtered to `log:error`/`log:fatal`
     (event_type 13, 14) and the `log:error:*` subtypes (100–104).
   - **Direct children** — count of immediate child spans.
   - **Events under span** (logs panel) — every event under this span_id,
     time-sorted, with severity colouring driven by `event_type`. Each row
     shows the event message, the registered event-type name, and the
     captured caller.
   - **Direct child spans** — table of immediate children with their names,
     start times, and durations.

## What the views give you

| View                          | Purpose                                                     |
|-------------------------------|-------------------------------------------------------------|
| `witness.span_starts`         | Earliest "open" event per span_id                           |
| `witness.span_finishes`       | Latest "close" event per span_id                            |
| `witness.span_pairs`          | Start + finish joined, with duration (NULL if open)         |
| `witness.span_children`       | Parent/child relation derived from span chain co-occurrence |
| `witness.event_records_json`  | Per-event records aggregated to a JSONB column              |
| `witness.event_type_names`    | Integer event_type → string name (mirrors `events.go`)      |

These are general-purpose; use them from ad-hoc SQL in Explore or build
your own panels on top.

## Known limitations

- **No flame chart.** PostgreSQL data source can't drive Grafana's trace
  visualization. For that, run a Jaeger HTTP shim on top of the witness DB
  and point Grafana's Jaeger data source at it. Not built here yet.
- **Span parent inference relies on timing.** `span_children` picks the
  most-recently-started co-occurring span as the parent. This is correct
  when start events are emitted strictly before their children's start
  events, which is how the witness API does it. If you bulk-import or
  replay events with skewed timestamps, parentage can flip.
- **Dangling spans** (no finish event) show `finished_at = NULL` and
  `duration = NULL`. They are not auto-closed at query time.
- **Custom event types.** If you register new event types via
  `witness.MustNewEventType`, extend `witness.event_type_names` and the
  `CASE` in the logs-panel SQL accordingly.

## Extending

Useful next panels you can add:

- **State timeline** over `span_pairs` rows under the selected parent —
  rough Gantt approximation. Needs two-row-per-span output (start, finish).
- **Top callers / messages** — `event_caller` or `event_message` aggregated
  over the selected span and its descendants. Requires a recursive CTE
  over `span_children`.
- **Records inspection** — join `event_records_json` into the logs panel
  to expose per-event key/value records.
