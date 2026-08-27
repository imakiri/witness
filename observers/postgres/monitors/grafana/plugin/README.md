# Witness Grafana data source plugin

Backend-only Grafana plugin that lets you browse the `witness.*` Postgres
schema written by `observers/postgres`. Four query types:

| Type    | What it shows                                                       |
| ------- | ------------------------------------------------------------------- |
| `search` | Universal event search: filter by id, message, caller, event_type, record key/value. Returns a logs frame. |
| `trace`  | Reconstructs one distributed trace from its root span_id, including children grafted via `cross_service_edges`. Returns a Tempo-style trace frame. |
| `logs`   | Logs panel: `log:*` + `error:*` events by default. Filter by event_type, caller, message substring. |
| `table`  | Spans table from `witness.span_pairs`, with start time / duration. Optionally only roots. |

## Prerequisites

Apply to the Postgres database your `postgres.Observer` writes to:

```sh
psql "$WITNESS_DB" \
  -f ../migration.up.sql \
  -f ../migration_v2.up.sql \
  -f ../views.up.sql
```

`migration_v2.up.sql` adds the `parent_trace_id` / `parent_span_id` columns,
the `pg_trgm` extension, and the FTS / substring indexes that the search and
trace queries rely on.

## Layout

```
plugin/
├── plugin.json          # legacy copy (kept for back-compat with old loaders)
├── src/
│   ├── plugin.json      # authoritative — copied into dist/ by webpack
│   ├── module.ts        # frontend entrypoint (DataSourcePlugin)
│   ├── datasource.ts    # DataSourceWithBackend wrapper
│   ├── types.ts         # mirrors pkg/plugin/query.go QueryModel
│   ├── README.md
│   └── components/
│       ├── ConfigEditor.tsx
│       └── QueryEditor.tsx
├── pkg/
│   ├── main.go          # datasource.Manage entrypoint
│   ├── pgpool/          # parses jsonData / secureJsonData → pgxpool
│   ├── plugin/          # backend handlers: QueryData, CheckHealth, CallResource
│   └── queries/         # one file per query type
├── package.json         # frontend build deps
├── tsconfig.json
└── Magefile.go          # delegates to grafana-plugin-sdk-go/build
```

## Build

### Backend

```sh
mage -v          # builds ./dist/gpx_witness_<os>_<arch> for every supported target
mage -v build    # current host only
mage -v clean    # wipe dist/
```

Outputs land under `./dist/`. The `executable` field in `plugin.json` is
`gpx_witness`; Grafana picks the per-OS suffix at runtime.

### Frontend

The frontend uses the standard `@grafana/create-plugin` webpack layout. The
shared `.config/` directory (`webpack/`, `jest/`, etc.) is not committed —
bootstrap it once with:

```sh
npx @grafana/create-plugin@latest update    # writes .config/ + Dockerfile etc.
npm install
npm run build                                # production bundle into dist/
npm run dev                                  # watch mode
```

`npm run build` copies `src/plugin.json` and the JS bundle into `./dist/`,
where `mage build` already placed the Go binary — Grafana loads them
together from one directory.

## Install into Grafana

Copy the entire `dist/` directory to Grafana's plugins path under
`imakiri-witness-datasource/`, then either:

* set `app_mode = development` and add
  `allow_loading_unsigned_plugins = imakiri-witness-datasource` to
  `grafana.ini`, **or**
* sign it with `npx @grafana/sign-plugin@latest` (requires a Grafana Cloud
  access policy).

Restart Grafana, add a new data source of type **Witness**, and supply:

| Field | Example |
| ----- | ------- |
| URL   | `postgres://witness@localhost:5432/witness?sslmode=disable` |
| Max connections | `4` |
| Password (optional, overrides URL) | `…` |

## Cross-service trace stitching

A trace shows up in the `trace` view rooted at the **originating instance's
root span_id**. Service A injects this via `propagation.Inject`; Service B
calls `witness.InstanceContinue(ctx, obs, "service-b", v, traceID,
parentSpanID)`, which writes its `span:instance:online` event with
`parent_trace_id = <A's root span_id>` and `parent_span_id = <A's current
span_id>`. The `cross_service_edges` view exposes that link; the recursive
CTE in `pkg/queries/trace.go` walks both same-event co-occurrence (inside
one process) and cross-service edges (between processes) to assemble the
full waterfall.

## Health check

The plugin's `CheckHealth` runs `SELECT 1` and `SELECT 1 FROM
witness.events LIMIT 1`. If the second fails, the data source page tells
you to apply the migrations.
