# Witness

#### _Better than OTEL_

See [`MIGRATION.md`](./MIGRATION.md) for breaking changes between versions.

## Install

Witness is pre-1.0. The root module carries the API; every observer and
adapter is a separate module, so you pull only the dependencies you import.

The API is split in two: `witness` is what application code calls, and
`witness/core` — same module — holds the data model and the observer
contract. Write an `Observer` or a `Printer` against `core`; call
`witness.Info` and friends from everything else.

```sh
go get github.com/imakiri/witness@v0.30.0
go get github.com/imakiri/witness/record@v0.20.0

# whichever observers you actually use
go get github.com/imakiri/witness/printers@v0.1.0
go get github.com/imakiri/witness/observers/stdlog@v0.23.0
go get github.com/imakiri/witness/observers/otlp@v0.2.0
```

It's a data model, an observability API and set of its implementations. It combines metrics, logs and traces into one
data entity called event.

Features:

* Push-only data flow
* Distributed. No internal data dependencies, each observer is independent
* Custom event types. You can make your own event types, tailored for your application
* Metric events carry a single `value` record — a counter delta or a histogram observation. Clients may emit one event
  per increment/observation, or batch counter increments into a single event with a larger value

---

## Data model

Core entity is an event. It has an id — event_id (uuid v7) — and two kinds of dimension:
time (event_date) and space (span_id, uuid v7). There is exactly one time dimension and
any number of space dimensions.

Time is the simple one: an observer-assigned real time.

Space is the tricky one. A span_id is a place — a function call, a running instance, a
message hand-off. An event carries every span it is inside, plus any it merely references.

Any event can also have any number of records: key/value pairs, key always a string.

### Span roles

The span_ids of one event are not interchangeable. Each carries a role, so a query can
tell "the event happened *in* this span" from "the event happened somewhere *under* it":

| flag           | role                                                             |
|----------------|------------------------------------------------------------------|
| `own` (1)      | the event happened directly in this span — exactly one per event |
| `parent` (2)   | direct parent of the own span                                    |
| `ancestor` (4) | an enclosing scope above the parent                              |
| `instance` (8) | root span of the process that emitted the event                  |
| `link` (16)    | a span this event *references* without being inside it           |

The first four are the emitter's own chain, ordered root → leaf; links come after it.
Roles are derived from that shape, never stored — today's own span is tomorrow's parent.

### One owner per span

A process only ever emits events from spans **it opened**. It never opens a span another
process opened. Two processes writing `start`/`finish` into one span_id would make that
span's duration read "from the caller's start to the callee's finish" — a number nobody
asked for.

Sides connect by *referencing* a shared span_id instead: it rides on the event as a
`link`, and both sides emit events carrying it. `WHERE $1 = ANY(event_span_ids)` still
returns both, which is all a link ever had to do.

There is no trace_id. A trace is a connected component of the event↔span graph, not a
column — a scalar cannot describe an event that legitimately belongs to two traces at
once. There is no service_name either: the service is the `instance` span, and its
`span:instance:online` event carries the name.

### Generic script execution

| event_id                             | event_date               | event_type          | event_message                  | event_span_ids                                                                 |
|--------------------------------------|--------------------------|---------------------|--------------------------------|--------------------------------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start  | called main function           | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | log:info            | done some task                 | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4097-14a7-7846-9bdd-a4c2a1147441 | 2026-05-19T14:15:07.801Z | span:general:start  | called sub-function foo        | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-564d-724f-a404-a4186aa9f5ea | 2026-05-19T14:16:33.184Z | span:general:finish | returned from sub-function foo | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-8af2-7eb9-b0ae-78af1482b941 | 2026-05-19T14:16:52.107Z | span:general:finish | returned from function main    | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |

In the two-span rows the first span_id is `parent|instance` and the second is `own`.

### Concurrent job processing

Two task handlers run in parallel under main:

| event_id                             | event_date               | event_type             | event_message        | event_span_ids                                                                 |
|--------------------------------------|--------------------------|------------------------|----------------------|--------------------------------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start     | called main function | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | log:info               | preparing tasks      | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |
| 019e4097-14a7-7846-9bdd-a4c2a1147441 | 2026-05-19T14:15:07.801Z | span:wait_group:start  | called task handler  | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4097-14a7-7846-9bdd-a4c2a1147442 | 2026-05-19T14:15:07.901Z | span:wait_group:start  | called task handler  | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e40a6-4ffd-747f-b070-db87ac5857e6 ] |
| 019e4098-564d-724f-a404-a4186aa9f5ea | 2026-05-19T14:16:33.184Z | span:wait_group:finish | task done            | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e4097-86b7-7584-b87d-07347f21f563 ] |
| 019e4098-8af2-7eb9-b0ae-78af1482b941 | 2026-05-19T14:16:52.107Z | span:wait_group:finish | task done            | [ 019e4094-8426-770e-b9ce-032cf328bcf6, 019e40a6-4ffd-747f-b070-db87ac5857e6 ] |
| 019e40a6-ecc4-7ef1-949e-c1754431d89b | 2026-05-19T14:32:28.997Z | span:general:finish    | all tasks done       | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ]                                       |

### Messaging

Producer and consumer are separate processes with separate instance spans. They share only
the message's span_id (`019e4097-…f563`), carried in the message itself. Neither *enters*
it — both reference it, marked `link`. A single
`WHERE '019e4097-86b7-7584-b87d-07347f21f563' = ANY(event_span_ids)` returns both sides:

| event_type                     | event_message   | event_span_ids                                         | span_flags                    |
|--------------------------------|-----------------|--------------------------------------------------------|-------------------------------|
| span:instance:online           | producer        | [ 019e4094-…bcf6 ]                                     | own\|instance                 |
| span:internal_message:sent     | task dispatched | [ 019e4094-…bcf6, **019e4097-…f563** ]                 | own\|instance, link           |
| span:instance:offline          | producer        | [ 019e4094-…bcf6 ]                                     | own\|instance                 |
| span:instance:online           | consumer        | [ 019e40be-…194a ]                                     | own\|instance                 |
| span:general:start             | handle task     | [ 019e40be-…194a, 019e40bf-…7a3c ]                     | parent\|instance, own         |
| span:internal_message:received | task received   | [ 019e40be-…194a, 019e40bf-…7a3c, **019e4097-…f563** ] | ancestor\|instance, own, link |
| span:general:finish            | handle task     | [ 019e40be-…194a, 019e40bf-…7a3c ]                     | parent\|instance, own         |
| span:instance:offline          | consumer        | [ 019e40be-…194a ]                                     | own\|instance                 |

The consumer emits the reference *inside* the span that handles the message, so a query on
the message span_id lands directly on the handler.

### Metrics

Counter and histogram events share one shape: a single `value` record. For counters it is
an increment delta passed to `Add(value)`; for histograms a single observation passed to
`Observe(value)`. Emit one event per operation, or batch counter increments into a single
event with a larger delta:

| event_id                             | event_date               | event_type          | event_message                 | event_span_ids                           | event_records                                        |
|--------------------------------------|--------------------------|---------------------|-------------------------------|------------------------------------------|------------------------------------------------------|
| 019e4094-0991-7d53-b481-ccb7a206350a | 2026-05-19T14:11:55.897Z | span:general:start  | called main function          | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] |                                                      |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | metric:counter      | http_requests_total           | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 1, "route": "/users", "status": 200 }     |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.144Z | metric:histogram    | http_request_duration_seconds | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 0.014, "route": "/users", "status": 200 } |
| 019e4096-3c7c-7773-ac5d-1fa06d15dc3b | 2026-05-19T14:14:14.244Z | metric:histogram    | http_request_duration_seconds | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] | { "value": 0.087, "route": "/users", "status": 200 } |
| 019e40a6-ecc4-7ef1-949e-c1754431d89b | 2026-05-19T14:32:28.997Z | span:general:finish | main returned                 | [ 019e4094-8426-770e-b9ce-032cf328bcf6 ] |                                                      |

---

## API

Everything a witness call can do falls into four shapes.

**1. Emit a point event in the current span.** The span chain is untouched.

```go
witness.Info(ctx, "cache warm", record.Int("entries", n))
witness.ErrorNetworkF(ctx, "fetch prices", err) // also returns a wrapped error
```

**2. Open a child span.** Returns a context whose current span is the new one, plus a
`Finish` that closes it.

```go
ctx, finish := witness.Span(ctx, "settle batch")
defer finish()
```

`Service` and `Worker` are the same with their own event types. `SpanStart` takes the
span_id from the caller, for ids that must exist before the span does.

**3. Open a root — a process.**

```go
ctx, finish := witness.Instance(context.Background(), observer, "ledger", "v1.4.0")
defer finish()
```

`Instance` and `Test` are the **only** constructors, and nothing sits above an instance.
Call it once, at process start; an inbound request is a span under it, not an instance of
its own. `Test` is the same bound to a `testing.TB`, so failures point at the test's line.

**4. Reference a span without entering it.** One side mints the shared id and hands it to
whatever carrier it has; the other references the same id. Neither opens or closes it.

```go
// sender
linkID := witness.Link(ctx, "job dispatch")
carrier.Set("x-link", linkID.String())

// receiver, inside its own span
witness.LinkTo(ctx, linkID, "job dispatch")
```

`InternalMessage{Sent,Received}` and `ExternalMessage{Sent,Received}` are the same pair
with message event types — `*Sent` mints and returns the id, `*Received` takes it.

---

## Propagation

Witness is transport-agnostic. The sender puts a span_id into whatever carrier it has —
HTTP header, message envelope field, gRPC metadata — and the receiver references it from
inside its own span. The data model only cares that both sides emit events whose
`event_span_ids` contain the shared span_id.

Over HTTP the `propagation` package does it with a W3C `traceparent`:

```go
// sender
msgID := witness.ExternalMessageSent(ctx, "POST /settle")
propagation.Inject(req.Header, msgID)

// receiver
ctx, finish := witness.Span(instanceCtx, "POST /settle")
defer finish()
if upstream, ok := propagation.Extract(r.Header); ok {
witness.ExternalMessageReceived(ctx, upstream, "POST /settle")
}
```

Witness has no trace_id, so the traceparent's 16-byte trace-id field carries the whole
span uuid and `Extract` recovers it exactly — no truncation, no suffix matching.

## OTLP export

`observers/otlp` turns witness spans into OTel spans and ships them to any
OTLP collector — Jaeger, Tempo, Grafana Cloud, etc. Combine with other
observers via `multi`:

```go
tp, _ := otlp.NewTraceProvider(ctx, otlp.ProviderConfig{
Protocol: otlp.ProtocolGRPC,
Endpoint: "otel-collector:4317",
Insecure: true,
})
otlpObs, _ := otlp.NewObserver(otlp.Config{Provider: tp})
defer otlpObs.Shutdown(ctx)

printer, _ := printers.NewPretty()
stdObs, _ := stdlog.NewObserver(printer)

ctx, finish := witness.Instance(ctx,
multi.NewObserver(stdObs, otlpObs),
"my_service", "v1")
defer finish()
```

The OTel trace_id is the first 16 bytes of the chain's first span_id — the
instance root, so every span of one process shares a trace. The OTel span_id
is the last 8 bytes of the current one. Both are raw byte copies, so the
same UUID shows up in Jaeger and in the Postgres tables.

A witness `link` becomes an OTel **span link** (`Span.AddLink`), which is the
right primitive for it: the peer's half of a shared span is not above ours,
it is the same span seen from the other side.

The two sides do not share an OTel trace_id — witness has none to propagate —
so a collector shows one trace per service joined by a link rather than a
single end-to-end trace. The witness-side link is exact and lossless.

## Notes

* A span is a point in the space dimension, not a duration. Events attached to a span_id form a
  line through time at that point in space. An event has one time value and any number of space
  values (span_ids) — that is how context connections are made.
* One owner per span: only the process that opened a span emits `start`/`finish` for it. Other
  processes reference it as a `link`. Two owners would make the reconstructed duration span both.
* `span:*:start` and `span:*:finish` are just conventional events that delimit a duration on a
  span_id. Nothing in the model requires them; a span_id can carry any number of events of any
  type. Duration, when needed, is computed at query time by pairing the start and finish events
  on the shared span_id.
* If a process dies before emitting a finish event, the span is left open, not lost — every event
  emitted on it is still there. Auto-close is an observer-side concern, not a data-model one.