# Witness

#### _Better than OTEL_

Upgrading from v0.x: see [`MIGRATION.md`](./MIGRATION.md).

It's a data model, an observability API and set of its implementations. It combines metrics, logs and traces into one
data entity called event.

Features:

* Push-only data flow
* Distributed. No internal data dependencies, each observer is independent
* Custom event types. You can make your own event types, tailored for your application
* Metric events carry a single `value` record — a counter delta or a histogram observation. Clients may emit one event per increment/observation, or batch counter increments into a single event with a larger value

---

## Data model

Core entity is an event. It has an id - event_id (uuid v7) and two types of dimensions:
time - event_date and space - span_id (uuid v7). There is only one time dimension, and any number of space dimensions

Time dimension is a simple one - an observer-assigned real time.

Space dimension is a tricky one. One span_id represents some place or a context in which event has happened, for ex.
a specific function call, a working instance, a network call. An event can have any number of associated span_ids.

Any event can have any number of records - key-value pairs associated with an event, where key is always a string.

Generic script execution:

| event_id                             | event_date               | event_type          | event_message                  | event_span_ids                                                                 |
|--------------------------------------|--------------------------|---------------------|--------------------------------|--------------------------------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start  | called main function           | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | log:info            | done some task                 | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4097-14a7-7846-9bdd-a4c2a1147441 | 2026-05-19T14:15:07.801Z | span:general:start  | called sub-function foo        | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-564d-724f-a404-a4186aa9f5ea | 2026-05-19T14:16:33.184Z | span:general:finish | returned from sub-function foo | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-8af2-7eb9-b0ae-78af1482b941 | 2026-05-19T14:16:52.107Z | span:general:finish | returned from function main    | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |

Concurrent job processing. Two task handlers run in parallel under main:

| event_id                             | event_date               | event_type             | event_message        | event_span_ids                                                                 |
|--------------------------------------|--------------------------|------------------------|----------------------|--------------------------------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start     | called main function | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | log:info               | preparing tasks      | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4097-14a7-7846-9bdd-a4c2a1147441 | 2026-05-19T14:15:07.801Z | span:wait_group:start  | called task handler  | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4097-14a7-7846-9bdd-a4c2a1147442 | 2026-05-19T14:15:07.901Z | span:wait_group:start  | called task handler  | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e40a6-4ffd-747f-b070-db87ac5857e6 ] |
| 019e4098-564d-724f-a404-a4186aa9f5ea | 2026-05-19T14:16:33.184Z | span:wait_group:finish | task done            | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-8af2-7eb9-b0ae-78af1482b941 | 2026-05-19T14:16:52.107Z | span:wait_group:finish | task done            | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e40a6-4ffd-747f-b070-db87ac5857e6 ] |
| 019e40a6-ecc4-7ef1-949e-c1754431d89b | 2026-05-19T14:32:28.997Z | span:general:finish    | all tasks done       | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |

Messaging. Producer and consumer live under different root spans, linked by a span_id carried in
the message itself (`019e4097-86b7-7584-b87d-07347f21f563`). A single
`WHERE 019e4097-86b7-7584-b87d-07347f21f563 = ANY(event_span_ids)` returns both,
reconnecting the two otherwise-disjoint traces:

| event_id                             | event_date               | event_type                     | event_message        | event_span_ids                                                                 |
|--------------------------------------|--------------------------|--------------------------------|----------------------|--------------------------------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start             | called main function | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | span:internal_message:sent     | message sent         | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e40a6-ecc4-7ef1-949e-c1754431d89b | 2026-05-19T14:32:28.997Z | span:general:finish            | main returned        | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e40bf-11ad-772a-9ecf-01984ac963bf | 2026-05-19T14:58:51.953Z | span:general:start             | service starts       | [ 019e40be-e103-70c7-b12f-e249b490194a ]                                       |
| 019e40bf-41a4-79e6-8224-2d1f67e21073 | 2026-05-19T14:59:02.507Z | span:internal_message:received | message received     | [ 019e40be-e103-70c7-b12f-e249b490194a, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e40bf-77ec-793c-8705-ca3f4511e23d | 2026-05-19T14:59:18.789Z | span:general:finish            | service finishes     | [ 019e40be-e103-70c7-b12f-e249b490194a ]                                       |

Metrics. Counter and histogram events share the same shape: a single `value` record. For
counters it is an increment delta passed to `Add(value)`; for histograms it is a single
observation passed to `Observe(value)`. Clients can emit one event per operation, or batch
counter increments into a single event with a larger delta:

| event_id                             | event_date               | event_type          | event_message                 | event_span_ids                           | event_records                                        |
|--------------------------------------|--------------------------|---------------------|-------------------------------|------------------------------------------|------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start  | called main function          | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] |                                                      |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | metric:counter      | http_requests_total           | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 1, "route": "/users", "status": 200 }     |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | metric:histogram    | http_request_duration_seconds | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 0.014, "route": "/users", "status": 200 } |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.244Z | metric:histogram    | http_request_duration_seconds | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 0.087, "route": "/users", "status": 200 } |
| 019e40a6-ecc4-7ef1-949e-c1754431d89b | 2026-05-19T14:32:28.997Z | span:general:finish | main returned                 | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] |                                                      |

---

## Propagation

Witness is transport-agnostic. To continue a span on the receiving side of any boundary (HTTP
request, message queue, gRPC call), the sender places the span_id into a carrier of its choosing
(HTTP header, message envelope field, gRPC metadata) and the receiver chains it into its own
witness context. The data model only cares that both sides emit events whose `event_span_ids`
contain the shared span_id — nothing else is required to reconnect the trace at query time.

## OTLP export

`observers/otlp` turns witness spans into OTel spans and ships them to any
OTLP collector — Jaeger, Tempo, Grafana Cloud, etc. Combine with other
observers via `tee`:

```go
tp, _ := otlp.NewTraceProvider(ctx, otlp.ProviderConfig{
    Protocol: otlp.ProtocolGRPC,
    Endpoint: "otel-collector:4317",
    Insecure: true,
})
otlpObs, _ := otlp.NewObserver(otlp.Config{Provider: tp})
defer otlpObs.Shutdown(ctx)

ctx, finish := witness.Instance(ctx,
    tee.NewObserver(stdlog.NewObserver(), otlpObs),
    "my_service", "v1")
defer finish()
```

The trace_id is the first 16 bytes of the root witness span_id; the span_id
is the last 8 bytes of the current one. Both are raw byte copies, so the
same UUID appears in Jaeger and in the Postgres tables. For cross-process
propagation use `otlp.Inject` / `otlp.Extract` over W3C `traceparent`.

## Notes

* A span is a point in the space dimension, not a duration. Events attached to a span_id form a
  line through time at that point in space. An event has one time value and any number of space
  values (span_ids) — that is how context connections are made.
* `span:*:start` and `span:*:finish` are just conventional events that delimit a duration on a
  span_id. Nothing in the model requires them; a span_id can carry any number of events of any
  type. Duration, when needed, is computed at query time by pairing the start and finish events
  on the shared span_id.
* If a process dies before emitting a finish event, the span is left open, not lost — every event
  emitted on it is still there. Auto-close is an observer-side concern, not a data-model one.