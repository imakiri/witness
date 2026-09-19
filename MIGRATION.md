# Upgrading

Witness is pre-1.0. Breaking changes are called out here, newest first;
everything not listed is additive.

## v0.31 — one entity, one owner: the span model tightened

### The data model moved to `witness/core`

`witness` is now the call API and nothing else — `Info`, `Span`, `Instance`,
`Link` and their kin. Everything an observer or a piece of plumbing needs
lives in `github.com/imakiri/witness/core`, in the same module:

| moved to `core` |
|---|
| `Context`, `With`, `From`, `Context.To` |
| `Event`, `Observer`, `NilObserver` |
| `EventType`, its 28 constructors, `MustNewEventType`, `Events`, `EventTypesCompare` |
| `SpanFlags` and its constants |
| `Record` |
| `Printer`, `Appender`, `PrintFlags` |

```go
// Old
func (o *MyObs) Observe(event witness.Event) { ... }

// New
import "github.com/imakiri/witness/core"

func (o *MyObs) Observe(event core.Event) { ... }
```

Application code is mostly unaffected: `witness.Info(ctx, "msg", record.String(...))`
still compiles, because `Record`, `EventType` and `Finish` remain in `witness`
as aliases of the core types. Only code that names `witness.Event`,
`witness.Observer`, `witness.Context`, an `EventType` constructor or a
`Print*` flag has to change — that is, observers and printers.

Most observers end up not importing `witness` at all.

`core.Context` lost its `Info` / `Warn` / `Debug` / `Error` methods; they
duplicated the top-level functions. `core.Context.Observe` still takes the
caller location as a parameter — the caller machinery stayed in `witness`
with the entry points whose lines it reports.

New on `core.Context`, all of it plumbing the `witness` package needs:
`NewInstance`, `WithChildSpan`, `ObserveLinked`, `Helper`.

This release reworks the span model as a whole. Read it end to end; the
pieces depend on each other.

The through-line: **a process only emits events from spans it owns, and
everything a span means is derivable from the span chain.** Scalars that
stood in for that structure — `trace_id`, `service_name`, the `parent_*`
pair — are gone, and so are the calls that existed only to maintain them.

### `Event` carries span roles

`Event` gained `SpanFlags []SpanFlags`, parallel to `SpanIDs`: entry `i` is
the role `SpanIDs[i]` plays in that event.

```
1  own       the event happened directly in this span (exactly one)
2  parent    direct parent of the own span
4  ancestor  an enclosing scope above the parent
8  instance  root span of the emitting process
16 link      a span this event references without being inside it
```

`SpanIDs` is `[chain..., links...]` — the emitter's chain root→leaf, then
any referenced spans. It is therefore no longer purely a chain: the last
entry is the current span only when the event carries no link. **Select by
`SpanFlags`, not by position.** A nil or short slice means "roles unknown",
not "no roles", so index-guard rather than assuming the lengths match;
hand-built events (tests, custom producers) leave it nil.

Additive for observers that ignore it. `observers/postgres` stores it as
`witness.spans.span_flags int8`.

### Links are referenced, never entered

A process never opens a span another process opened: two processes writing
`start`/`finish` into one span_id makes that span's reconstructed duration
meaningless — `span_starts` takes the earliest open and `span_finishes` the
latest close, so the pair would measure "from the caller's start to the
callee's finish".

A shared span_id rides on the event after the chain, flagged
`SpanFlagLink` and nothing else, and the emitter's Context is unchanged.
Both sides emit events carrying it, so one query on that span_id returns
both — which is all a link ever had to do.

New `Link`, emitting `span:link` (event type 2, already excluded
from the lifecycle views as "a cross reference, not a lifecycle event"):

```go
// side that creates the shared point
linkID := uuid.Must(uuid.NewV7())
witness.Link(ctx, linkID, "job dispatch")
carrier.Set("x-link", linkID.String())

// side that receives it
witness.Link(ctx, linkID, "job dispatch")
```

The four message helpers collapsed into two, and the internal/external
split is gone with them:

```go
// Old
msgID := witness.InternalMessageSent(ctx, "job")   // minted the id
witness.InternalMessageReceived(ctx, msgID, "job")
witness.ExternalMessageSent(ctx, "call")           // and the external twins
witness.ExternalMessageReceived(ctx, msgID, "call")

// New
msgID := uuid.Must(uuid.NewV7())
// ... put msgID in the carrier, send it, and only then:
witness.Sent(ctx, msgID, "job")
witness.Received(ctx, msgID, "job")
```

Two changes in one:

  - **No internal/external variant.** Event types 25 / -25 are gone and
    24 / -24 are renamed `span:message:sent` / `span:message:received`.
    Nothing ever read the difference — every view treats the two as one set,
    and `observers/otlp` routed them through one branch. Where it matters
    (a peer outside your witness system never emits its half, so a link with
    no receiving side is expected rather than lost) say so in a record.
  - **`Sent` takes the id instead of minting it.** The id has to exist
    before the send, because it travels in the carrier; the event says the
    hand-off *happened*, so it belongs after the send succeeded. The
    mint-and-emit call could only ever be emitted before, which left a link
    nobody would answer behind every failed send. A failed send is an error
    in the sender's span, not a link.

Emit `Received` inside the span that handles the message, so that span is
what a query on msgID finds on this side.

`Link` collapsed the same way and for the same reason: it takes the id
instead of minting one, and `LinkTo` is gone — both sides of a link now call
`Link` with the same id. Note that `span:link` counts as the **sending** side
in `span_link_sides`, so `Link` is also how you say "about to hand this off":
emitted before a send, it keeps the edge to the receiver even if the sender
dies before it can emit `Sent`. A receiver must still call `Received`, not
`Link`, or it is read as a second sender.

**A hand-off has one direction and no reply half.** One side gives (`Link`
before the send, `Sent` after it), the other takes (`Received`). An answer
travelling back is another hand-off with its own id — and for a synchronous
call it is no event at all: the round trip is the calling span's own
duration, which is a span with a duration in `span_pairs` rather than two
events whose delta something has to compute.

This is what keeps `trace_roots` simple: a span that took work is not a root,
a span that only gave work away is. A reply would make a caller read as
triggered by its own callee, and both workarounds tried for that — comparing
"who sent first", then a dedicated pair of reply event types — are gone with
it.

`ReceivedAll(ctx, msgIDs, ...)` is the receiving half for a batch,
referencing every msgID the batch took in from one event. It is how a worker
relates to the n requests that fed it — n parents are not expressible, n
links are.

`Handle(ctx, msgID, name)` and `HandleAll(ctx, msgIDs, name)` are `Span` plus
`Received` / `ReceivedAll` in one call, returning `(ctx, Finish)` like every
other span constructor: a child span for the work a message triggered, with
the message recorded inside it. They exist because the received half kept
landing on the dispatching span instead of the handling one, and a
long-lived dispatcher accumulates every message it ever handed on.

### `Service` and `Worker` are gone

Use `Span`. The span's name says what it is:

```go
// Old
ctx, finish := witness.Worker(ctx, "settle_worker")

// New
ctx, finish := witness.Span(ctx, "settle_worker")
```

Event types 22 / -22 (`span:service:*`) and 23 / -23 (`span:wait_group:*` —
the string never matched the function that emitted it) are removed with
them. Nothing read the distinction: `observers/otlp` routed all three kinds
through one start handler and one finish handler, and `span_starts` /
`span_finishes` matched the whole 20..23 range. "Service" also collided with
the model's own vocabulary, where a service is the *instance* span at the
head of the chain — the one `witness.instances` names — not a child span
somewhere below it.

If you want span kinds a query can filter on, register a paired custom type:

```go
var workerStart  = core.MustNewEventType(1000, "span:worker:start")
var workerFinish = core.MustNewEventType(-1000, "span:worker:finish")
```

`|i| >= 1000` with opposite signs is the supported range: the views read it
as a span lifecycle, and `witness.event_types` gives it a name in SQL.

Re-apply `monitors/grafana/views.up.sql`: the lifecycle ranges narrowed from
20..23 / -23..-20 to 20..21 / -21..-20.

### `core.Context.Helper` is gone; `TB()` replaces it

`Helper()` did not do what its name promised. `testing.TB.Helper` marks the
function that calls it, so a method whose body calls `c.t.Helper()` marked
*itself* — the entry points that called it stayed unmarked, and every log
line an observer produced from a `witness.Test` context was attributed to
`core/context.go` rather than to the test's own line. Inlining does not
change this: the logical frame survives.

`Context.TB()` returns the owning `testing.TB` (nil outside a test), and each
frame marks itself:

```go
if tb := c.TB(); tb != nil {
	tb.Helper()
}
```

Every entry point, `Finish` closure, `Context.Observe` and
`Context.ObserveLinked` now does this. Custom observers should keep calling
`t.Helper()` in their own `Observe`, as `observers/test` does. If you called
`Context.Helper()` from your own code, replace it with the shape above.

`TestHelperAttribution` in the root module guards it by running `go test -v`
on `internal/helperprobe`.

### The hot path got cheap, and observers can decline event types

`core.Caller` used to walk 16 stack frames and re-symbolise the same call
site on every event: 527 ns and 4 allocations, which was 99% of the cost of a
witness call. It now walks one frame — `runtime.Callers` applies `skip`
itself, so the other fifteen were always discarded — and caches `file:line`
by pc. `SetCallDepth` is a deprecated no-op; nothing to configure, and
calls to it still compile.

New optional interface, `core.EventTypeFilter`:

```go
type EventTypeFilter interface {
	Accepts(eventType EventType) bool
}
```

An Observer that implements it is asked **before an event is built** — before
the stack walk, the uuid, the records slice. A declined event costs about
12 ns and no allocations; an accepted one about 290. Nothing needs changing
to keep working: an Observer without the method accepts everything.

Implemented by `NilObserver` (declines all — a ctx carrying no witness state
is now free), `multi` (accepts if any member does), `stdlog` (answers from
`WithTypes`) and `prometheus` (metric types only).

`bench_test.go` in the root module holds the measurements.

### Metrics have entry points

`Count(ctx, name, delta)`, `Gauge(ctx, name, value)` and
`Sample(ctx, name, value)` emit `metric:counter`, `metric:gauge` and
`metric:histogram`. Before this, the three metric event types were
registered but had no way to be emitted from `witness` — a program had to
build the event through `core` by hand.

The shape is the one `observers/prometheus` already read: metric name in
`EventMessage`, the number in a record keyed `value` (first in the slice,
ahead of the labels). `observers/prometheus` gained `GaugeDef` and now
handles gauge events, which it used to drop silently; a gauge is `Set`, not
`Add` — the value is absolute.

`Sample` is the distribution one. It is not called `Observe` (that name
belongs to the observer contract) or `Histogram` (that is the aggregate, not
the event): witness records one observation and leaves the bucketing to the
backend.

### Error subtypes are gone; `Fatal` and `Panic` arrived

`log:error:{internal,external,device,storage,network}` (100..104) and their
eight entry points (`ErrorStorage`, `ErrorStorageF`, `ErrorNetwork`,
`ErrorNetworkF`, `ErrorExternal`, `ErrorExternalF`, `ErrorInternal`,
`ErrorInternalF`) are removed. Use `Error` and put the cause in a record:

```go
// Old
witness.ErrorStorage(ctx, "write ledger", err)

// New
witness.Error(ctx, "write ledger", err, record.String("kind", "postgres"))
```

Nothing ever branched on which subtype an event carried — `stdlog`, `test`
and `otlp` all read `EventType.IsError()`, and the SQL side treated the seven
error ids as one flat set. The boundaries between them ("is a failed INSERT
storage, network or external?") are drawn differently by every program, so a
dimension shipped in the library could not be relied on across services. A
record is open-ended where a five-value enum is not, and a program that wants
a closed set of its own has `core.MustNewErrorEventType` — the `|i| >= 1000`
range exists for it, and such types now reach SQL through
`witness.event_types`.

`Fatal(ctx, msg, err)` and `Panic(ctx, msg, cause)` are new. Neither
terminates anything: witness does not exit or re-panic on your behalf. Call
`Panic` from a `recover()`, passing whatever `recover()` returned.
`log:panic` is event type 15; `log:fatal` (14) finally has an entry point.

### `witness.event_types` and the end of hardcoded id lists

New table, written by the postgres observer at start-up from
`core.Events()`: `event_type`, `event_type_name`, `is_error`. Apply
`000_schema.up.sql` (it is part of the same migration) and re-apply
`monitors/grafana/views.up.sql`, where `event_type_names` is now a view over
it instead of a hand-maintained `VALUES` list.

Upgrading an existing install: `000_schema.up.sql` is written for an empty
database and its `CREATE SCHEMA` fails on one that already exists, so follow
the in-place list in that file's header comment — it names this table, the
two `span_*_types` views and the whole span cache. Order: DDL, then the new
binary, then `SELECT witness.rebuild_span_cache()`. `NewObserver` now
upserts the event types at construction and returns an error when the table
is missing, so it will not come up against the old schema; and the rebuild
classifies span boundaries through `witness.span_start_types`, which reads
`witness.event_types` — run before the binary has filled it, it would leave
every historical custom-typed span unnamed and unstarted in the cache, and
nothing revisits those rows afterwards.

This is what makes custom types visible to SQL: `MustNewEventType` /
`MustNewErrorEventType` registrations are upserted like any built-in, so they
are named in the UI and counted as errors. The plugin's `errorEventTypes` and
`logEventTypes` became subqueries over the table, and the dashboards' error
counts with them. Register custom types in `init()` — the upsert runs once,
when the observer is built.

### `Join` is gone

`Join` (and `Context.Join`) merged the span chains of unrelated contexts.
The result had no single own span — the tail was another context's span,
not the one the event happened in — so every role it reported was a guess,
and it silently mislabelled a foreign root as `own`.

The relation it existed for is a link. Express it with `Link`:
the two sides emit events referencing one span_id, and a query on that
span_id returns both. Producer/consumer and parent/child goroutines are the
same shape as any other hand-off.

`Context.rolesUnknown` went with it — roles are always knowable now, so a
`witness`-built event always carries a full `SpanFlags` slice.

### `Instance` and `Test` are the only constructors

`NewContext`, `NewTestContext` and `InstanceContinue` are gone.

`NewContext` produced a root span with no `span:instance:online` event —
nothing named the process and the chain's first span carried no
`SpanFlagInstance`. `InstanceContinue` existed to adopt an upstream
`trace_id`; with that removed, all that was left was putting a foreign span
above the local root, and in practice it was called **per inbound request**,
claiming a fresh process on every call.

```go
// Old
ctx := witness.NewContext(observer).To(context.Background())
wtx := witness.NewTestContext(t, observer)

// New
ctx, finish := witness.Instance(context.Background(), observer, "my_service", "v1.4.0")
defer finish()

ctx, finish := witness.Test(context.Background(), t, observer)
defer finish()
```

`Test` is `Instance` plus the owning test: it binds a `testing.TB` (widened
from `*testing.T`, so benchmarks and fuzz targets work) so every entry point
can call `tb.Helper()`. The instance is named `tb.Name()` and versioned
`"test"`.

**Nothing sits above an instance.** Call `Instance` once, at process start.
`ctx` is used only as the parent `context.Context`; any witness state on it,
test binding included, is discarded rather than inherited.

An inbound request is a span under the instance that *references* the
caller's span:

```go
// Old — an "instance" per request
Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    traceID, parentSpanID, _ := propagation.Extract(r.Header)
    ctx, finish := witness.InstanceContinue(r.Context(), obs, "service-b", "1.0", traceID, parentSpanID)
    defer finish()
    handle(ctx)
})

// New — one instance for the process, a span per request
instanceCtx, finishInstance := witness.Instance(context.Background(), obs, "service-b", "1.0")
defer finishInstance()

Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    ctx, finish := witness.Span(witness.From(instanceCtx).To(r.Context()), "POST /work")
    defer finish()
    if upstreamSpanID, ok := propagation.Extract(r.Header); ok {
        witness.Received(ctx, upstreamSpanID, "POST /work")
    }
    handle(ctx)
})
```

### No `trace_id`, no `service_name`

Removed from `witness.Event`, `witness.Context` and the Postgres schema.
All four columns were scalars standing in for structure the space dimension
already carries. **A trace is a connected component of the event↔span graph,
not a column**: a scalar cannot describe an event that legitimately belongs
to two traces at once, and it was written by inheritance rather than
derived, so it could disagree with the span chain in the same row.

| Gone | Use instead |
|---|---|
| `Event.TraceID`, `.ParentTraceID`, `.ParentSpanID`, `.ServiceName` | the span chain: `SpanIDs` + `SpanFlags` |
| `Context.TraceID()` | — |
| `Context.ServiceName()` | `Context.InstanceSpanID()`, then that span's `span:instance:online` event |
| `witness.Trace()` | `witness.Span()` — without a trace_id to mint it was byte-for-byte the same call |

`Context.InstanceSpanID()` returns the chain's first span, the emitting
process. `Context.RootSpanID()` is a synonym.

### `Observe` moved to `core` and lost `eventID` / `eventDate`

There is no `witness.Observe`. It took an `EventType`, which only `core` can
produce, so its caller already imported `core` and the wrapper added nothing.
Emit a custom event through the Context directly:

```go
// Old
witness.Observe(ctx, uuid.Must(uuid.NewV7()), time.Now(), myEventType, "message", records...)

// New
c := core.From(ctx)
c.Observe(myEventType, "message", core.Caller(0), records...)
```

The id is always a fresh uuid v7 and the date always `time.Now()` at the
call — every caller already passed exactly that, and accepting them let two
events claim one identity or an event claim a time its process never saw.
Span roles are not passed either: they are derived.

`core.Caller(skip)` is the exported form of what the witness entry points use.
skip counts frames above its own caller: **0** reports the line on which
`Caller` is written, **1** the line that called it. Emitting directly wants 0;
a wrapper emitting on someone else's behalf wants 1.

`SetCallDepth` moved to `core` with it.

There is no replacement for supplying a past `eventDate`. To import events
recorded elsewhere, build the `witness.Event` yourself and hand it to the
`Observer`.

### `SpanStart` / `SpanFinish`

`SpanStart` is `Span` with the span_id supplied by the caller — for when the
id must exist before the span does, because it is going into an envelope or
a header. It returns `(context.Context, Finish)`. The span is this process's
own; to point at a span another process owns, use `Link`.

`SpanFinish` closes a span by id, for the shape where start and finish are
not lexically paired.

### `propagation` carries one span_id, whole

```go
// Old
propagation.Inject(h, c.TraceID(), c.CurrentSpanID())
traceID, parentSpanID, ok := propagation.Extract(h)

// New
propagation.Inject(h, c.CurrentSpanID())
parentSpanID, ok := propagation.Extract(h)
```

With no trace_id to carry, the traceparent's 16-byte trace-id field holds
the whole upstream span uuid, so `Extract` recovers it exactly. The old form
kept only the low 8 bytes of the parent span, which is why `views.up.sql`
had to join on `right(span_id::text, 17)`.

`otlp.Inject` / `otlp.Extract` (deprecated shims) changed the same way.

### `otlp`

`Middleware(instanceCtx context.Context)` replaces
`Middleware(observer, name, version)`: it opens one span per request under
the process's instance, named `METHOD /path`, and references the upstream
span_id when a traceparent is present.

A witness link becomes an OTel **span link** (`Span.AddLink`) — the OTel
primitive for exactly this relation: the peer's half of a shared span is not
above ours, it is the same span seen from the other side.

The two sides do not share an OTel trace_id; witness has none to propagate.
A collector shows one trace per service joined by a link. The witness-side
link is exact.

### Postgres

The migration files were collapsed into a single `000_schema.up.sql` /
`000_schema.down.sql` pair — witness is pre-1.0 and this release removes
four columns every earlier version wrote, so an incremental path to a shape
nothing produces any more was not worth carrying.

Apply `000_schema.up.sql` to an empty database. Upgrading an existing
install in place: the header comment of that file has the `ALTER` block
(drop `trace_id`, `parent_trace_id`, `parent_span_id`, `service_name`; add
`witness.spans.span_flags int8` and the denormalised `event_date` on
`witness.spans` / `witness.records`) plus the new indexes, the list of
objects to create verbatim — `witness.event_types`, the `span_*_types`
views and the derived span cache — and the `SELECT
witness.rebuild_span_cache()` that fills the cache. The DDL goes before the
new binary, the rebuild after it. Rows written before it keep `span_flags = 0`,
which reads as "the producer did not report roles" — they are not
backfilled, because the ordering the roles are derived from was never
stored.

Replacement queries for the dropped columns:

```sql
-- every event of one instance (was: WHERE service_name = $1)
SELECT e.* FROM witness.events e
JOIN witness.spans s ON s.event_id = e.event_id
WHERE s.span_id = $1 AND s.span_flags & 8 <> 0;

-- both sides of a hand-off (was: the parent_trace_id join)
SELECT e.* FROM witness.events e
JOIN witness.spans s ON s.event_id = e.event_id
WHERE s.span_id = $1;
```

`event_date` also lost its `DEFAULT NOW()`. It was dead — `queueEvent`
always supplies the value — and would have lied if it ever fired, stamping
ingest time onto an event's time dimension.

### Grafana

The monitor was rebuilt on `span_flags`. `cross_service_edges` and
`trace_services` are replaced by `span_links` / `link_edges` and by
`instances` / `event_instances`; `span_children` now reads the parent off
`span_flags & 2` instead of guessing it from timestamps; a new `trace_roots`
view gives the UI an entry point to walk from.

`trace_roots` excludes a span that received a **request** (-24/-25), not one
that received a *reply* (-26/-27). The old rule dropped the caller of any
synchronous call implemented as an async send/reply pair, rooting its trace on
the worker that answered instead.

`span_pairs`, `span_children` and the new `span_instances` no longer assume a
span has a start event: the span universe now comes from `witness.spans`, a
finish-only span appears (named by its finish event), a span with events and
no lifecycle at all appears, and parenthood is read off any event rather than
off the start. `span_starts` / `span_finishes` also stop matching the message
types — 24..27 are hand-offs that happen *inside* a span, and matching
`20..29` made a lone `*_message:sent` look like a span opening.

`span_link_sides` (new) is one row per (span, link) with the first date of
each half, classified by event type. `link_edges` is built on it, so a
retried send is one edge dated from the first attempt, and its from_at/to_at
may be inverted when events were written out of order.

`link_edges` is now built on `span_link_sides` rather than on raw
`span_links` rows, so a hand-off retried on one msgID is one edge instead of
one per attempt, dated from the first send. It also covers in-process
hand-offs: it joins the two halves of a
link on the shared span_id and takes direction from the event_type sign,
without requiring them to belong to different instances. The old predicate
disconnected a request span from the worker span that dequeued its job, so a
batching service's traces stopped at the queue. Same-service edges are
filtered in the service-map query rather than in the view, which the walk
needs.

A trace has no id, so the panels walk its component at query time from a
**root span id**. The plugin's query types accept `rootSpanID` and keep
`traceID` as an alias, so saved dashboards and links keep working; the
dashboard variable is still called `selected_trace` and now holds that root
span id.

Re-apply `monitors/grafana/views.up.sql` — running `views.down.sql` first
also drops the two views that no longer exist.

## v0.27 — `Observer.Observe` takes an `Event` struct

The interface went from seven positional arguments plus a variadic to a
single struct. The built-in observers were updated in place; only custom
implementations need attention.

Old:

```go
func (o *MyObs) Observe(
    spanIDs []uuid.UUID, eventID uuid.UUID, eventDate time.Time,
    eventType witness.EventType, msg, caller string, records ...witness.Record,
) {
    // ...
}
```

New:

```go
func (o *MyObs) Observe(event witness.Event) {
    // event.SpanIDs, event.EventID, event.EventDate, event.EventType,
    // event.EventMessage, event.EventCaller, event.Records
}
```

The fields on `witness.Event` map one-to-one to the old parameters. Where
the old code took `records ...Record`, `event.Records` is a `[]Record` —
the iteration looks the same.

To find custom implementations in a repo:

```sh
grep -rnE 'func \([^)]+\) Observe\(.*\[\]uuid\.UUID' .
```

`postgres.Event` is gone in the same change; the observer now uses
`witness.Event`. Replace any explicit references.

## Module and toolchain notes

- `record/` is its own module. Keep both requires:

  ```
  require (
      github.com/imakiri/witness        v0.30.0
      github.com/imakiri/witness/record v0.20.0
  )
  ```

- Each observer and adapter is likewise its own module, so you only pull the
  dependencies you actually import.
- `observers/otlp` requires Go 1.25 (transitive OTel constraint). The root
  module still targets Go 1.22, so this only bites if you import `otlp`.

## What's new since v0.20

- `witness.Service`, `witness.Worker` — named sub-spans for long-running
  services and concurrent workers (removed again in v0.31; use `Span`).
- `witness.Sent` / `Received` for message-passing events (introduced in
  v0.21 as the `InternalMessage*` / `ExternalMessage*` quartet; collapsed to
  this pair in v0.31).
- `witness.Printer` / `witness.Appender` and the `printers` module
  (`printers.Pretty`, `printers.JSON`) — rendering split out of the
  observers. `stdlog.NewObserver` now takes a `Printer`.
- `propagation` — W3C `traceparent` inject/extract, feeding
  `witness.SpanStart`.
- `observers/otlp` — exports witness traces over OTLP (gRPC or HTTP) into
  Jaeger / Tempo / any OTLP collector.
- `observers/multi` (replaces the deprecated `tee`) and `observers/test`.
- Postgres observer fixes: clean shutdown (the worker loop was inverted)
  and no more unbounded goroutine spawn under load.
