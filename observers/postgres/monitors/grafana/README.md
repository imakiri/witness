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
#    Fresh deploy: one consolidated schema file.
psql "$WITNESS_DB" \
  -f ../../schema.up.sql \
  -f ./views.up.sql

# Upgrading an existing install that was created with the original
# migration.up.sql? Run the incremental patches instead:
#   psql "$WITNESS_DB" -f ../../migration_v2.up.sql -f ../../migration_v3.up.sql -f ./views.up.sql

# 2. Start Grafana with the dashboards & datasource provisioned in.
docker run -d --name witness-grafana \
  -p 3000:3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  --add-host=host.docker.internal:host-gateway \
  -v "$(pwd)/provisioning:/etc/grafana/provisioning" \
  -v "$(pwd)/dashboards:/var/lib/grafana/dashboards" \
  grafana/grafana:11.3.0

# 3. Open http://localhost:3000 → Dashboards → Witness → Witness — overview, logs & traces
```

The provisioning files assume Postgres at `host.docker.internal:5432`. Edit
`provisioning/datasources/witness.yaml` for other addresses.

## How the dashboard reads witness data

Everything keys on `witness.events.trace_id` — the per-request identifier
that `witness.Trace(ctx, "handle-work")` mints on the entry side and that
`witness.InstanceContinue(..., parentTraceID, ...)` adopts on the receiver
side. With this column populated, every panel scopes to a single request
by setting the dashboard's `request_filter` toggle to `on` and selecting a
`trace_id` from the dropdown. Before `migration_v3`, request membership
had to be reconstructed at query time by walking `witness.spans` and
`witness.cross_service_edges`; the dashboard uses neither once `trace_id`
is present.

For request-level visualisations (trace graph, flat span list) the
dashboard reads:

* `witness.span_pairs` — opens & closes joined by span_id, with `duration`.
* `witness.span_children` — parent ↔ child derived from co-occurring spans.
* `witness.cross_service_edges` — `parent_trace_id → child_root_span_id`
  links produced by every `InstanceContinue` call.

For global panels (volume histogram, recent requests, stats) the dashboard
reads `witness.events` directly.

## Customising panels for your team

* Each panel's SQL is in the JSON under `targets[*].rawSql`. Keep these as
  reviewable SQL (no string-glue), and use `${trace_id}` / `${message}` etc.
  for the variables defined in `templating.list[*].name`.
* When adding a new event type via `witness.MustNewEventType`, also add a
  row to `witness.event_type_names` in `views.up.sql` so the dashboard's
  filter dropdown surfaces it.
* The PostgreSQL datasource ignores `format: "trace"` and `"logs"` — use
  `"table"` and let the Logs panel auto-detect the `time`/`body` columns.
