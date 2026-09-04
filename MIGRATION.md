# Upgrading

Witness is pre-1.0. Breaking changes are called out here, newest first;
everything not listed is additive.

## v0.31 — one entity, one owner: the span model tightened

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

New `Link` / `LinkTo`, emitting `span:link` (event type 2, already excluded
from the lifecycle views as "a cross reference, not a lifecycle event"):

```go
// side that creates the shared point
linkID := witness.Link(ctx, "job dispatch")
carrier.Set("x-link", linkID.String())

// side that receives it
witness.LinkTo(ctx, linkID, "job dispatch")
```

`*MessageSent` now mints the id and returns it, instead of taking one:

```go
// Old
msgID := uuid.Must(uuid.NewV7())
witness.InternalMessageSent(ctx, msgID, "job")

// New
msgID := witness.InternalMessageSent(ctx, "job")
```

Emit `*MessageReceived` inside the span that handles the message, so that
span is what a query on msgID finds on this side.

### `Join` is gone

`Join` (and `Context.Join`) merged the span chains of unrelated contexts.
The result had no single own span — the tail was another context's span,
not the one the event happened in — so every role it reported was a guess,
and it silently mislabelled a foreign root as `own`.

The relation it existed for is a link. Express it with `Link` / `LinkTo`:
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
        witness.ExternalMessageReceived(ctx, upstreamSpanID, "POST /work")
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

### `Observe` loses `eventID` and `eventDate`

```go
// Old
witness.Observe(ctx, uuid.Must(uuid.NewV7()), time.Now(), myEventType, "message", records...)
witness.From(ctx).Observe(uuid.Must(uuid.NewV7()), time.Now(), myEventType, "message", caller, records...)

// New
witness.Observe(ctx, myEventType, "message", records...)
witness.From(ctx).Observe(myEventType, "message", caller, records...)
```

The id is always a fresh uuid v7 and the date always `time.Now()` at the
call — every caller already passed exactly that, and accepting them let two
events claim one identity or an event claim a time its process never saw.
Span roles are not passed either: they are derived.

There is no replacement for supplying a past `eventDate`. To import events
recorded elsewhere, build the `witness.Event` yourself and hand it to the
`Observer`.

### `SpanStart` / `SpanFinish`

`SpanStart` is `Span` with the span_id supplied by the caller — for when the
id must exist before the span does, because it is going into an envelope or
a header. It returns `(context.Context, Finish)`. The span is this process's
own; to point at a span another process owns, use `Link` / `LinkTo`.

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
`witness.spans.span_flags int8`) plus the two new partial indexes. Rows
written before it keep `span_flags = 0`, which reads as "the producer did
not report roles" — they are not backfilled, because the ordering the roles
are derived from was never stored.

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
  services and concurrent workers.
- `witness.InternalMessageSent` / `InternalMessageReceived` and the
  matching `ExternalMessage*` pair for message-passing events.
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
