# Witness — Grafana monitor

A drop-in Grafana setup for exploring `witness.*` Postgres data: per-event
logs view (Loki-style), per-request trace graph, log volume histogram,
cross-service edges, and aggregate stats. Uses the stock PostgreSQL
datasource — no custom plugin required to consume the data this way (a
custom plugin lives under `plugin/` for richer integration, but is not
needed for the dashboard).

## Layout

```
observers/postgres/monitors/grafana/
├── README.md                                       (you are here)
├── views.up.sql / views.down.sql                   SQL views the dashboard reads
├── dashboards/
│   └── witness-overview.json                       the dashboard
├── provisioning/
│   ├── datasources/witness.yaml                    PostgreSQL datasource pre-wired
│   └── dashboards/witness.yaml                     loads everything in dashboards/
├── dashboard.json                                  legacy v1 dashboard (kept for compat)
└── plugin/                                         custom Grafana backend plugin (optional)
```

## Quick start with Docker

```sh
# 1. Apply schema (run once against your witness DB)
psql "$WITNESS_DB" \
  -f ../../000_schema.up.sql \
  -f ./views.up.sql

# Re-applying over an install from before v0.31? Run views.down.sql first —
# it also drops cross_service_edges and trace_services, which are gone.

# 2. Start Grafana with the dashboards & datasource provisioned in.
docker run -d --name witness-grafana \
  -p 3000:3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  -e WITNESS_PG_URL=host.docker.internal:5432 \
  --add-host=host.docker.internal:host-gateway \
  -v "$(pwd)/provisioning:/etc/grafana/provisioning" \
  -v "$(pwd)/dashboards:/var/lib/grafana/dashboards" \
  grafana/grafana:11.3.0

# 3. Open http://localhost:3000 → Dashboards → Witness → Witness — overview, logs & traces
```

The datasource URL comes from `WITNESS_PG_URL` — Grafana expands environment
variables in provisioning files — so point it wherever your database is.

## How the dashboard reads witness data

There is no `trace_id`. A trace is a connected component of the event <-> span
graph, walked at query time from a **root span**: an entry-point span whose
parent is an instance and which took no inbound message. `witness.trace_roots`
lists them; the dashboard's `selected_trace` variable holds one, and every
downstream panel walks out from it with the recursive CTE in each panel's SQL
(mirrored in `plugin/pkg/queries/walk.go`).

The walk is directed — parent -> child via `witness.span_children`, giver ->
taker via `witness.link_edges` — because plain co-occurrence in
`witness.spans` would merge a whole process into one component: every event of
a process carries that process's instance span.

Views the panels read:

* `witness.trace_roots` — one row per request, on the originating side.
* `witness.span_pairs` — start and finish joined by span_id, with `duration`.
  Both halves are optional: a span may have started and not finished, or have
  been observed only by its finish, or carry events and no lifecycle at all.
* `witness.span_children` — parent stated by `span_flags & 2` on any event of
  the child, not guessed from timestamps.
* `witness.link_edges` — hand-offs, in-process ones included: one row per pair
  of spans sharing a link id, dated from the first send.
* `witness.instances` / `witness.event_instances` — which process emitted what.
  A service *is* its instance span; there is no `service_name` column.
* `witness.event_type_names` — a view over `witness.event_types`, the table the
  Postgres observer upserts from `core.Events()` at start-up. Custom types
  registered with `MustNewEventType` appear there automatically.

## Dashboards

* **Witness — traces, services, logs** (`dashboards/witness-overview.json`) —
  raw SQL against the stock Postgres datasource: trace list, service graph,
  span table, logs. Needs no plugin.
* **Witness — trace waterfall** (`dashboards/witness-trace.json`) — the
  Jaeger-style waterfall, which *does* need the plugin: only a backend
  datasource can return a frame typed as a trace, and that is what the
  waterfall renders. Pick a trace in the table at the top; the row link sets
  the `root` variable and the panel below draws it. Filters: `service` (a
  service that took part) and `search` (substring of the root span's name).

## Demo

`scripts/demo.sh up` from the repo root brings up Postgres, this Grafana with
both dashboards and both datasources provisioned, the plugin built and
mounted, and the three services in `examples/distributed` writing real events
into it, then drives some traffic. `scripts/demo.sh down` removes it all.

Do not point `WITNESS_TEST_DSN` at the demo database: the plugin's query
tests `TRUNCATE` the event tables, which leaves the running services without
their `span:instance:online` rows — every later event then has no service
name and no trace root.

## Customising panels for your team

* Each panel's SQL is in the JSON under `targets[*].rawSql`. Keep these as
  reviewable SQL (no string-glue), and use `${trace_id}` / `${message}` etc.
  for the variables defined in `templating.list[*].name`.
* A new event type registered with `core.MustNewEventType` needs no SQL: the
  observer upserts it into `witness.event_types` at start-up, and the name and
  error flag follow. Register custom types in `init()`, before the observer is
  built.
* The PostgreSQL datasource ignores `format: "trace"` and `"logs"` — use
  `"table"` and let the Logs panel auto-detect the `time`/`body` columns.

---
