# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test

This is a Go workspace (`go.work`) containing the root module plus several independent sub-modules. Every package is
built/tested via its own module — run `go` commands either from the root (uses workspace) or from inside the specific
sub-module directory.

```sh
# NOTE: `go build ./...` / `go test ./...` from the root only covers the ROOT
# module's packages, not the sub-modules — a broken sub-module stays green.
# To cover everything, loop over the module directories:
for d in . adapters/log observers/{multi,otlp,postgres,prometheus,stdlog,tee,test} \
         printers propagation record examples examples/distributed; do
  (cd "$d" && go vet ./... && go test ./...)
done

# single sub-module
cd observers/postgres && go test ./...

# single test
go test -run TestCaller ./...
```

`examples/` declares its own ad-hoc module (`module examples`) and is meant to be run
directly: `cd examples && go run simple.go`.

Sub-modules currently in the workspace: `.` (root, which also
holds `propagation/`), `adapters/log`, `observers/{multi,otlp,postgres,prometheus,stdlog,tee,test}`, `printers`, `record`, `examples`, `examples/distributed`, `testenv`.
When adding a new sub-module, register it in `go.work` *and* in `scripts/tidy.sh`'s module list, or the workspace
and `scripts/release.sh` will both miss it.

`record/` is its own module (`github.com/imakiri/witness/record`) holding the public
helpers (`record.String`, `record.Int`, `record.Marshaller`, …). The root package keeps a *separate*,
unexported `record` struct because `witness → record/` would close a cycle (`record` imports `witness` for the `Record`
interface).

`observers/postgres/monitors/grafana/plugin` is deliberately *outside* the workspace (Grafana SDK deps): build it
with `GOWORK=off go build ./...`.

Sub-modules pin a version of the root via `require github.com/imakiri/witness vX.Y.Z`. Locally the workspace overrides
this with the working copy; releases must bump the version pins after publishing root.

## Architecture

Witness is a single-entity observability data model: every metric, log line, span boundary, and message hand-off is the
same kind of object — an **event** with one time dimension (`event_date`) and N space dimensions (`event_span_ids`). The
README has the full data-model rationale and example tables; read it before changing event shapes.

### Package split

Two packages in the root module, split by audience:

- **`witness`** (root) — the call API and nothing else: `Info`/`Warn`/`Debug`/`Error`/`ErrorRF`/`ErrorOrInfo`/`Fatal`/`Panic`, `Span`/`SpanStart`/`SpanFinish`, `Instance`/`Test`, `Link`, `Sent`/`Received`/`ReceivedAll`, `Handle`/`HandleAll`, `Count`/`Gauge`/`Sample`. 22 functions plus three aliases (`Record`, `EventType`, `Finish`) so its own signatures read without a second import. It declares no types of its own.
- **`witness/core`** — the data model and plumbing: `Context` (+ `With`/`From`/`To`), `Event`, `Observer`/`NilObserver`, `EventType` and its 18 constructors and registry, `SpanFlags`, `Record`, `Printer`/`Appender`/`PrintFlags`, `Caller`/`SetCallDepth`.

  There is deliberately **no `witness.Observe`** for custom event types. It took a `core.EventType`, which only `core` can produce, so the caller already imported `core` and the wrapper bought nothing. Emit one directly:

  ```go
  c := core.From(ctx)
  c.Observe(myEventType, "cache evicted", core.Caller(0), records...)
  ```

**The boundary is forced, not chosen.** `Context.Observe` constructs an `Event`, so `Event`, `Observer`, `EventType`, `SpanFlags` and `Record` must live with `Context` — putting them in `witness` while `Context` sits in `core` closes an import cycle. What is *not* forced, and is the point: `witness` depends on `core`, never the reverse, so the call API can change without touching the observer contract and vice versa.

Consequence worth knowing: most observers no longer import `witness` at all — `multi`, `tee`, `test`, `stdlog`, `prometheus`, `printers` and `record` need only `core`. If you find yourself adding `"github.com/imakiri/witness"` to an observer, check whether you actually want `core`.

`caller.go` and the unexported `record` struct stayed in `witness`: the caller contract is about *entry points*, which all live there, and `core.Context.Observe` takes the location as a parameter rather than walking the stack itself. `core.Context` deliberately has no `Info`/`Warn`/`Debug`/`Error` methods — they duplicated the top-level helpers and were the only thing in `core` that would have needed the caller machinery.

### Core types (package `witness/core`)

- **`Observer`** (`core/observer.go`) — the only interface backends implement: `Observe(Event)`, where `Event` is a struct
  carrying `SpanIDs` (**`[chain..., links...]`, not purely a chain — select by flag, not position**), `SpanFlags` (
  parallel; per-span role bitmask — own/parent/ancestor/instance/link —
  see `core/span_flags.go`), `EventID`, `EventDate`, `EventType`, `EventMessage`, `EventCaller`, and `Records`. `NilObserver`
  is the zero-cost no-op.
- **`Context`** (`core/context.go`) — value type carrying `(observer, spanIDs, t)`. Three fields; **every derived `Context`
  must copy all three** — a dropped `t` silently breaks test line attribution. Stored in `context.Context` under an
  internal key; retrieved with `From(ctx)`, attached with `With(ctx, c)` / `c.To(ctx)`.

  `spanIDs` is the chain of spans **this process owns**, ordered root→leaf; the last element is the current span. Every
  role is positional and derived per event, nothing is stored:

  | position | role |
                        |---|---|
  | `i == 0` | `\|= SpanFlagInstance` — nothing sits above an instance |
  | `i == n-1` | `SpanFlagOwn` |
  | `i == n-2` | `SpanFlagParent` |
  | otherwise | `SpanFlagAncestor` |
  | past the chain | `SpanFlagLink`, and only that |

  **A witness call does one of four things:**

    1. Emit a point event in the current span (`Info`, `Error`, `Observe`, metrics) — chain untouched.
    2. Open a child (`Span`/`SpanStart`/`Handle`) — appends, returns a Context whose current span is the new
       one.
    3. Replace the chain with a fresh instance root (`Instance`/`Test`) — the *only* constructors, and they ignore any
       witness state on the incoming ctx entirely.
    4. Reference a foreign span without entering it (`Link`, the message helpers) — appended to that one event
       *after* the chain with `SpanFlagLink` and nothing else; the Context is unchanged.

  **A process never enters a span another process opened.** Two processes writing start/finish into one span_id
  makes `span_pairs.duration` measure "caller's start to callee's finish". The shared span_id is referenced instead, and
  a query on it still returns both sides — which is all a link ever had to do.

  **There is no `trace_id` and no `service_name`.** A trace is a connected component of the event↔span graph, not a
  column: a scalar cannot describe an event that legitimately belongs to two traces at once. The service is the chain's
  first span, whose `span:instance:online` event carries the name (`Context.InstanceSpanID()`).

  Gone with them, and why — do not reintroduce without re-reading this
  list: `Event.{TraceID,ParentTraceID,ParentSpanID,ServiceName}`; `witness.Trace` (identical to `Span` once there was no
  trace_id to mint); `witness.InstanceContinue` (existed to adopt an upstream trace_id, and in practice was called *per
  inbound request*, claiming a new process on every call); `NewContext`/`NewTestContext` (a root span with
  no `span:instance:online` event — nothing named the process, no `SpanFlagInstance`); `Join`/`Context.Join`
  and `Context.rolesUnknown` (a merged chain has no single own span, so every role it reported was a guess — the
  relation it expressed is a link).

- **`EventType`** (`core/events.go`) — `(errorFlag, int64, string)` triple. All built-in types are
  functions (`EventTypeLogInfo()`, `EventTypeSpanStart()`, …) that must *also* be listed in the package-level `events`
  slice; `Events()` returns a copy of it and is what type-filtering observers and column-width calculations read. **A
  built-in type missing from `events` is a type those consumers silently drop** — that bug hid every `metric:*` from `stdlog`. `MustNewEventType` / `MustNewErrorEventType` append to the same slice at registration
  time; the second sets the error flag, which is what routes an event to `stdlog`'s error writer and
  trips `test.WithFailOnError`. Both panic on an `i` already registered — `EventTypesCompare` orders on `i` alone, so a
  collision would make a type filter's binary search match the wrong type. The `i` range `(-1000, +1000)` is reserved;
  custom types must use `MustNewEventType` with `|i| >= 1000` and a name ≤128 runes. By convention paired start/finish
  use opposite-sign codes (e.g. `+20`/`-20`).
- **`EventTypeFilter`** (`core/observer.go`) — optional interface on an Observer: `Accepts(EventType) bool`, consulted
  by `Context.Wants` **before an event is built**, i.e. before the stack walk, the uuid and the records slice. A
  declined event costs ~12 ns and zero allocations instead of ~290; an Observer that does not implement it accepts
  everything (the only safe default — a guessing filter drops data silently). `NilObserver` declines everything, so a
  ctx with no witness state is free. `multi` accepts if *any* member does, which is worth knowing before wondering why
  a filter downstream saved nothing: one unfiltered member makes the fan-out unfiltered. `stdlog` answers from
  `WithTypes`; `prometheus` takes metric types only. The hot entry points (`Info`/`Warn`/`Debug`, `Count`/`Gauge`/
  `Sample`) check it first; correctness does not depend on that — `Context.ObserveLinked` checks again, so an observer
  that declines a type never sees it whatever the call path.
- **`Printer` / `Appender` / `PrintFlags`** (`core/printer.go`) — rendering is decoupled from transport. An observer that
  writes text takes a `Printer` (`Print(io.Writer, Event, PrintFlags)`) or
  an `Appender` (`Append([]byte, Event, PrintFlags) []byte`) and a `PrintFlags` bitmask selecting which columns to
  emit (`PrintAll` / `PrintNone` at the ends). Implementations live in the `printers` module: `printers.Pretty` (aligned
  columns, widths grown under a mutex as events arrive) and `printers.JSON`.
- **`propagation`** (`propagation/`, in the root module) — W3C `traceparent` `Inject`/`Extract` over
  an `http.Header`. `Inject` takes one span_id and puts it whole into the traceparent's 16-byte trace-id field (witness
  has no trace_id to spend there), so `Extract` recovers the exact uuid; feed it into `Link` or a `*MessageReceived`
  inside the request's own span — the upstream span is referenced, never entered. `observers/otlp/propagation.go` is a
  deprecated shim over this.
- **`Record`** (`core/record.go`, and module `record/`) — interface (`AppendKey`, `AppendValue`, `KeyEqual`). The
  root package has a minimal internal `record` struct; the `github.com/imakiri/witness/record` module is the public
  toolkit (`String`, `Int`, `Float`, `Bool`, `Bytes`, `Stringer`, `Error`, plus `Marshaller` for reflection-based
  struct→records).
- **Top-level helpers** (`witness.go`) — `Observe` (arbitrary event type; takes no ids, dates or
  flags — `eventID`/`eventDate` are generated inside `Context.Observe` and every span role is
  derived), `Info/Warn/Debug/Error`, the `*F` error variants that also `return fmt.Errorf(...)`, `Span` (creates child
  span, returns ctx + `Finish` closure), `SpanStart` (`Span` with a caller-supplied span_id, for ids that must exist
  before the span; returns ctx + `Finish`) / `SpanFinish` (closes a span by id when start and finish are not lexically
  paired), `Instance` (root span representing a process lifetime — **call it once, at process start**; with `Test`, the
  *only* way to construct a `Context`. An inbound request is an ordinary `Span` under the instance which then
  *references* the upstream span_id with `Received`), `Test` (`Instance` bound to a `testing.TB`,
  named `tb.Name()`, versioned `"test"`), and `Link` (
  relates a caller-supplied span_id to this span, emitting `span:link`; both sides of a hand-off call it with the same
  id),
  and `Sent` / `Received` / `ReceivedAll` — the same pair with message event types. Both take the msgID: it must exist
  before the send (it travels in the carrier), so the caller mints it, and the event says the hand-off *happened*, so
  emit `Sent` after the send succeeded. Emit `Received` inside the span that handles the message. There is **no
  internal/external split** — it was two event types nothing ever distinguished; a peer outside the system is a record,
  not a type. `Handle` / `HandleAll` are `Span` + `Received` / `ReceivedAll` in one call: a child span for the work the
  message triggered, with the message recorded inside it, so the received half cannot land on a span that outlives the
  work.

  **A hand-off is one direction and has no reply half.** One side gives (`Link` before the send, `Sent` after it), the
  other takes (`Received`, or `Handle`). An answer travelling back is another hand-off with its own id — and for a
  synchronous call it is no event at all: the round trip is the calling span's own duration, which is why `Request`-
  shaped sugar for the calling side was considered and dropped. The reply event types that briefly existed
  (`*_message:replied` / `:reply_received`, ±26/±27) are gone: they existed only to stop a caller's inbound reply from
  reading as "someone triggered this span" — and a caller that emits nothing on the way back cannot have that problem
  in the first place.
- **Test attribution: `tb.Helper()` must be written in the frame you want skipped.** `testing.TB.Helper` marks the
  function that *calls* it, so a wrapper — `func (c Context) Helper() { c.t.Helper() }` — marks only itself and leaves
  every caller looking like the origin. That was the bug: with a real `witness.Test` context, every `t.Logf` from an
  observer pointed at `core/context.go` instead of the test's own line. **Inlining does not help** — verified:
  `gcflags=-m` reports the wrapper inlined and the attribution still lands on the wrapper's body, because
  `runtime.CallersFrames` expands inline frames and `testing` matches on the logical function. So `core.Context.TB()`
  returns the `testing.TB` and each frame writes its own `if tb := c.TB(); tb != nil { tb.Helper() }`: every entry
  point, every `Finish` closure, `instance`, `Context.Observe`, `Context.ObserveLinked`, plus the observer's own
  `Observe`. `TestHelperAttribution` guards it by running `go test -v` on `internal/helperprobe` and checking every
  line points at the probe file — it cannot be checked in-process, since `testing.TB` is unimplementable outside
  `testing`.
- **`caller`** (`core/caller.go`) — attaches the source location of the witness call to every event, as `file:line`.
  Walks exactly **one** frame (`runtime.Callers` applies `skip` itself, so everything past the first entry was always
  discarded) and caches `file:line` by pc in a `sync.Map`, because symbolisation is the expensive half and its answer
  never changes for a given pc. That took `Caller` from 527 ns / 4 allocs to ~110 ns / 0 allocs — the pc buffer is a
  one-element local array and symbolisation lives in its own function, because `CallersFrames` keeps the slice it is
  given and would otherwise move that array to the heap on every call (a `sync.Pool` of slices is not the fix: `Put`
  boxes the slice header and allocates too). It matters because
  it was 99% of the cost of a witness call — see `bench_test.go`. `SetCallDepth` is a deprecated no-op; there is no
  depth to configure. `Caller(skip)` counts frames above its own caller:
  **0 is the line on which `Caller` is written, 1 is that line's caller.** Every entry point in `witness` passes 1,
  because it reports on someone else's behalf; code emitting an event directly passes 0. Getting this backwards
  attributes the event one frame too high and the mistake is invisible until `TestCallers` runs.

  **The caller contract:** *the reported location is the line on which the witness entry
  point (`Info`, `Error`, `Span`, `SpanFinish`, …) is written.* Everything in `caller.go` exists to uphold that one
  sentence, and `TestCallers` in `caller_test.go` checks it across every entry point and call shape — add a subtest
  there when you add an entry point. The rule is absolute: if a shape does not satisfy it, fix `caller` or declare the
  shape unsupported — do not weaken the sentence. Consequences worth knowing:

    - The location is always `file:line`. There used to be an `EnableDebug()` flag selecting between `file:line` and the
      enclosing function's name; the name cannot express the rule, so the flag and its two formats are
      gone. `frame.File` is the *builder's* absolute path — build with `-trimpath` if that must not reach telemetry.
    - `caller` reports the **immediate** frame above `skip`; it does no filtering. A call inside a closure, a goroutine
      literal, or an `http.HandlerFunc` reports the line inside that literal. An earlier version skipped
      anonymous `.func` frames looking for a named one, which made calls in goroutine literals report `runtime.goexit`
      and calls in subtests report `testing.go` — that is the bug the contract was written to close, so do not
      reintroduce frame filtering.
    - A house wrapper (`func (l *myLogger) Info(m string) { witness.Info(l.ctx, m) }`) is attributed to
      the `witness.Info` line inside the wrapper. That is the rule holding, not an exception: `witness.Info` really is
      written there. There is no way to skip a *further* frame — `Context.Observe` takes a caller string and the package
      exports nothing that produces one.
    - It uses `runtime.CallersFrames`, never `FuncForPC`/`FileLine` on a raw pc: only `CallersFrames` does the
      return-address adjustment and expands inlined frames. Without it the line drifts by a build-flags-dependent
      amount.
    - The `Finish` closures returned by `Span`/`SpanStart`/`Handle`/`HandleAll`/`Instance`/`Test` do **not** walk the
      stack. Each constructor captures its own call site eagerly into `at` and the closure reuses it, so a span's start
      and finish events report the same line — the line that opened the span. (`Test` delegates to the
      unexported `instance`, which takes `at` as a parameter for exactly this reason.)
    - **One usage is unsupported**, because the runtime makes the rule impossible to uphold there: deferring a witness
      helper *directly* — `defer witness.Info(...)`, `defer witness.SpanFinish(...)`. The body runs at function exit,
      and a deferred call's own pc is not reachable from the stack (`_defer.pc` holds it but is unexported, and
      open-coded defers allocate no `_defer` record outside a panic). Write `defer func() { witness.Info(...) }()`
      instead — a deferred closure has its own frame sitting on the witness call and reports it correctly. Constructors
      returning a `Finish` are immune.

### Observers (`observers/`)

Each observer is its own Go module so users only pull in the dependencies they actually use.

- **`stdlog`** — synchronous writer; takes a `witness.Printer` (normally `printers.Pretty`) plus options: `WithFlags`
  selects columns, `WithTypes` restricts to a set of event types, `WithWriter`/`WithErrorWriter` set the destinations
  for non-error and error (`EventType.IsError`) events. `WithErrorWriter` defaults to `WithWriter`, which defaults
  to `os.Stdout` — errors are not split out unless asked; pass `io.MultiWriter(os.Stdout, os.Stderr)` to send them to
  both. `WithUsingStdErr` is the deprecated `WithErrorWriter(os.Stderr)`. Without `WithTypes` there is *no* type
  filtering — deliberately, because snapshotting `witness.Events()` at construction dropped every type registered by a
  later `MustNewEventType`.
- **`multi`** — fan-out to several observers. `tee` is the same thing, deprecated in its favour.
- **`test`** — routes events into `t.Logf`; `WithFailOnError` fails the test on any `EventType.IsError()` event.
- **`postgres`** — async batching observer. `worker` goroutines (one per `pgxpool` MaxConn) drain a buffered channel
  into a `pgx.Batch` until either `CollectionMaxSize` events or `CollectionDuration` elapses, then ship the
  batch. `Observe` never blocks — a full channel drops the event and bumps `Dropped()`; `Close` is idempotent and drains
  under `ShutdownTimeout`. Three tables: `witness.events`, `witness.spans` (event_id ↔ span_id ↔
  span_flags), `witness.records`, plus a **derived span cache** the writer maintains: `witness.span_facts` (one row
  per span: name, start, finish, instance, first/last event, event count) and `witness.span_edges` (one row per
  *backwards* edge — `parent`, or `link` for a hand-off — carrying the cut it imposes and the receiving event that
  gates it). `flush` writes a batch as **three multi-row `INSERT ... SELECT unnest(...)` statements plus
  `SELECT witness.merge_span_cache($ids)`**, all in one `pgx.Batch`, so a reader never sees events whose spans are not
  summarised yet. The merge costs about as much as the inserts (24 ms against 26.5 ms per 1024-event batch, ~24 µs per
  event) and takes the event trail from **3145 ms to 2.5 ms** on half a million events, returning the same rows. The
  cache is ~15% of the base tables and is **not truth**: `witness.rebuild_span_cache()` recreates it from the events in
  ~5 s per 500k events, and nothing but `merge_span_cache` may write to it. **Every merge in it is commutative**
  (`least`/`greatest`/`coalesce`), which is what makes it correct under unordered, optional events — a start after its
  finish, a finish that never comes, a `sent` written after the matching `received` all fold to the same row. The one
  exception is `event_count`, which sums: replaying a batch would double it, so a writer that retries must rebuild
  instead. See `docs/storage.md` for the measurements and the alternatives that were rejected. **One migration: `000_schema.up.sql` / `000_schema.down.sql`**,
  named `NNN_description.{up,down}.sql`. The old `migration.up.sql` + `migration_v2..v6` chain was collapsed into it —
  pre-1.0, and v0.31 dropped four columns every earlier version wrote. The in-place upgrade `ALTER` block lives in that
  file's header comment. A schema change means a new numbered pair *and* the Grafana views together; `postgres_test.go`
  embeds `000_schema.up.sql`, so a rename breaks the integration tests. The Grafana monitor's views, plugin
  queries and dashboard were rebuilt on `span_flags` in the same release — see below.
- **`prometheus`** — declarative: counters, gauges and histograms must be pre-declared in `Config`. Events whose `eventMessage`
  doesn't match a configured metric name are silently dropped. The `value` record is the delta/observation; other
  records are matched against `LabelKeys`. Exposes `.Handler()` for the `/metrics` endpoint.
- **`otlp`** — exports witness events as OpenTelemetry spans over OTLP (gRPC or HTTP) to a collector. Maintains
  a `sync.Map` registry of witness `span_id → otel trace.Span`; start events call `tracer.Start`, finish events
  call `span.End`, log/error events become `AddEvent` / `SetStatus(Error)` / `RecordError`. `traceID` is the raw 16
  bytes of `SpanIDs[0]`; `spanID` is the last 8 bytes of the current witness span_id (no string
  formatting — `cleanTraceID`-style dash-stripping was a bug in the reference impl). A witness span
  carrying `SpanFlagLink` becomes an OTel **span link** via `Span.AddLink`, not a parent — the peer's half of a shared
  span is the same span seen from the other side, not one above it. **`currentSpanID`/`parentSpanID` select by flag,
  never by position**: `SpanIDs` ends with links, so reading `SpanIDs[n-1]` would register spans under a link's id and
  leak every one of them from the eviction-less registry. The two sides therefore have different OTel trace_ids (witness
  has no trace_id to make them equal): a collector shows two traces joined by a link. The witness-side link, a shared
  span_id row in `witness.spans`, is exact. Cross-process propagation lives in the root `propagation`
  package (`otlp.Inject`/`otlp.Extract` are deprecated shims). `NewTraceProvider` returns errors instead
  of `log.Fatal` — never use `log.Fatal` from inside an observer. The registry has no eviction: a witness span whose
  finish event never arrives keeps its otel span alive for the Observer's lifetime. That is intentional — witness spans
  have no timeout, and force-ending one would invent a duration the data model does not carry.

`observers/postgres/monitors/grafana` ships a Grafana dashboard, a datasource plugin and SQL views, all rebuilt on
`span_flags` in v0.31:

- `instances` / `event_instances` replace the dropped `service_name` column — the service *is* the span with
  `span_flags & 8`, and its `span:instance:online` event carries the name.
- `span_starts` / `span_finishes` match on `span_flags & 1` (own), so an ancestor listed in a start event is no longer
  mistaken for a span starting. They also accept custom span types (`|event_type| >= 1000`).
- `span_children` states the parent outright via `span_flags & 2` on the child's start event. **The old
  "latest-started co-occurring span" heuristic is gone — do not bring it back.**
- `span_links` / `link_edges` replace `cross_service_edges`. A cross-process hop is an ordinary link row, so nothing
  special-cases it — and neither is an **in-process** one: `link_edges` joins the two halves on the shared link_span_id
  and takes its direction from the event_type sign alone. It deliberately does *not* require the two sides to be
  different instances; that predicate added no direction and disconnected every in-process queue (a request span
  enqueues, a worker span dequeues) from the trace walk. Self-loops (one span on both sides) are excluded, and the
  service-map edge query drops same-service edges so an in-process hop is not drawn as a service calling itself.
- `trace_roots` is the UI's handle on a trace: an entry-point span (its parent is an instance) that no one else
  *triggered*, i.e. one with no inbound `span:message:received` (-24) in `span_link_sides`. A span that only gave work
  away is a root; a span that took work is not. This rule is only safe because **a hand-off has no reply half** — a
  caller emits nothing on the way back, so it never reads as triggered by its own callee. Earlier attempts to allow a
  reply (comparing "sent before received", then a dedicated pair of reply event types) both existed solely to work
  around that, and both went away with the reply itself.
- **A long-lived worker span is also a root** — parent is the instance, nothing triggered it — so a batching process
  shows one extra traces row whose walk fans out into every batch it ever ran. So is a span sitting under an instance
  that never emitted a start (only events, or only a finish): it is a span like any other, and nothing triggered it
  either. Known and accepted: the data is right,
  and every discriminator tried (exclude a root whose subtree receives) also excludes real entry points. Filter it in
  the panel by name if it bothers you.

- **A trace is walked at query time**, seeded from a root — `traceWalkCTE` in `plugin/pkg/queries/walk.go`, mirrored in
  the dashboard's raw SQL. The walk is **directed** (parent→child, sender→receiver). Plain co-occurrence over
  `witness.spans` would merge a whole process into one component, because the instance span sits in every one of its
  events.
- The plugin's request types keep `traceID` as an alias for `rootSpanID`, so saved dashboards and links keep working.
- The dashboard variable is still named `selected_trace`; it now holds a root span_id.

`event_type_names` is a view over `witness.event_types`, a table the postgres observer upserts from `core.Events()`
at start-up — names and the error flag live in Go and are copied in, so custom types registered with
`MustNewEventType` are named in SQL too. Nothing here may hardcode a list of ids again: the plugin's
`errorEventTypes`/`logEventTypes` are subqueries over that table for the same reason. A type registered *after* the
observer is built is not in the table — register custom types in `init()`.

`queryType: event-trail` (`plugin/pkg/queries/trail.go`) answers the other question the model makes cheap: given one
event, what led to it. It **walks `witness.span_edges`** — the writer's cache — rather than deriving the edge set at
query time; the walk touches about a hundred edges, and rebuilding the set to find them was the entire cost. The walk goes *backwards* over pairs of (span, cut), where the cut is `(event_date, event_id)` —
the id breaks the tie, because `event_date` is microseconds while `time.Now()` is nanoseconds. Two edges, both read off
structure: **parent** (a span's causes are its parent's events up to the child's start event; a child with no start
keeps the child's own cut rather than inventing a beginning) and **link** (a span that took a hand-off was caused by
the giving side up to the *first* giving event, the same one `link_edges` dates the hand-off from, so a retry does not
move the cut). Timestamps enter in exactly one place — an inbound hand-off counts only if its receiving event is at or
before the current cut — and both sides of that comparison are events of the same span, so no clock is ever compared
across processes. Where a span is reached by two paths the **earliest** cut wins: the cone is what provably preceded
the event. `SinceMinutes` (default 60) bounds it from the seed event's own date backwards, and the bound applies to
**hand-off edges, not to parenthood**: a hand-off older than the window did not cause the event we are asking about,
while belonging to a parent is not an event and does not age — the parent's own events are cut by the window anyway.
Widening the window can therefore *move* the cut rather than only adding rows, when it opens a path through an older
span. `TestEventTrail` pins both rules on `batchSeedSQL`, and both were checked by mutation (drop the guard, or
take the latest cut, and it fails). Each row also carries the **edge its span was reached by** — `viaSpanID`,
`edgeFromEventID`, `edgeToEventID` — because a view drawing a hand-off as a line between two lanes needs the span on
the other end and the two events the line runs between; for a parent edge there is no event on the parent's side
(opening a child emits nothing there), so `edgeFromEventID` is empty and `edgeToEventID` is the child's start.
`instanceSpanID` is there for the same reason: lanes are laid out per process, and two instances of one service share
a name. **The cone goes strictly backwards** — what led to the event, never what followed it. The query deliberately avoids `span_pairs`, `event_instances`,
`event_records_json` and `span_children`: each is built over the whole database before a row can be read out of it,
and the trail knows the handful of spans and events it wants — names, service and records come from lateral lookups
by id, and the parent relation from a two-column `DISTINCT` over `witness.spans`. That plus `back_edges AS
MATERIALIZED` (a plain CTE is re-planned into the recursive term and re-evaluated on every iteration) took a trail
from 6.2s to ~3.3s, the second measured on 14x more data. It still scales with table size rather than cone size —
`span_starts` and the edge set are global — which is the next thing to attack if it matters.

The plugin lives outside the workspace: `GOWORK=off go build ./...`. Its queries are SQL strings, so nothing but
running them catches a renamed view — `pkg/queries/queries_test.go` is that check. It seeds a two-service trace in pure
SQL and asserts row counts for every query type; env-gated on `WITNESS_TEST_DSN`, skipped otherwise:

```sh
docker run -d --rm --name wpg -e POSTGRES_PASSWORD=witness -e POSTGRES_USER=witness \
  -e POSTGRES_DB=witness -p 55432:5432 postgres:16-alpine
export WITNESS_TEST_DSN='postgres://witness:witness@localhost:55432/witness?sslmode=disable'
psql "$WITNESS_TEST_DSN" -f observers/postgres/000_schema.up.sql \
                         -f observers/postgres/monitors/grafana/views.up.sql
(cd observers/postgres && go test ./...)                       # observer integration tests
(cd observers/postgres/monitors/grafana/plugin && GOWORK=off go test ./...)   # view/query tests
```

There are two dashboards. `witness-overview.json` is raw SQL against the stock Postgres datasource. `witness-trace.json`
is the Jaeger-style waterfall and needs the **plugin**: only a backend datasource can return a frame typed
`PreferredVisualization: trace`, which is what Grafana's traces panel renders. `queries.RunTrace` builds that frame,
re-parenting a span reached across a process boundary onto the *sending* span (link preferred over chain parent) so
the tree stays connected where witness records a link. What lands in which section of the trace view: `tags` →
"Span attributes", the records of **both** lifecycle events *and* of the span's own `span:message:received` (what a
caller attaches on the way out is as much an attribute as what it attached on the way in, and the records given to
`witness.Handle`/`HandleAll` land on the received event, which is the only place they exist — reading the start alone
left every Handle-opened span with an empty attributes panel). Only the receiving half: a `sent` or a `link` describes
a message this span gave away, so its records stay marks on the bar; `serviceTags` → "Resource attributes", which is the *instance* — its
name, its online time, its span_id and the records of its own `span:instance:online` / `:offline` events (the section
heading is Grafana's own string, not ours); `logs` → the marks on the bar, every event whose own span it is except the
two that are the bar itself, each carrying its event type as a field. **Times are fractional milliseconds** —
`startTime`, `duration` and each mark's timestamp. Rounding any of them to whole milliseconds drifts a mark up to 1ms
against a bar drawn from fractional values, which on a 6ms span reads as an event outside its own span; that was a
real bug, not a hypothetical. The plugin's `dist/module.js` is hand-written — the backend
does all the work, so the frontend only registers a `DataSourceWithBackend` and interpolates dashboard variables into
the query JSON; there is no query editor UI, and provisioned dashboards carry their queries.

`testenv` is the same stack under testcontainers, and the one to reach for now: one test function starts Postgres
(schema + views as init scripts), Kafka and Grafana with this plugin built from the working copy, runs two services
that keep emitting until Ctrl-C, and removes everything after. Run it with
`WITNESS_TESTENV=1 go test -count=1 -v -timeout 0 -run TestEnv ./testenv` — every flag is load-bearing and
`testenv/README.md` says why (chiefly: a hang-until-Ctrl-C test still caches its PASS, so without `-count=1` the second
run starts nothing and replays the first run's output, and without `WITNESS_TESTENV` it skips so a workspace-wide
`go test ./...` does not hang forever). Two settings there were found the hard way: Postgres needs `ShmSize` raised
above the 64MB default or the trace query fails with `could not resize shared memory segment`, and the host bind
mounts (plugin `dist`, `provisioning`, the staged dashboard) go through `HostConfigModifier`, since testcontainers'
`Mounts` API is volumes-only.

`scripts/demo.sh up` brings the whole thing up — Postgres on :55432, this Grafana on :3000 with the dashboard and
datasource provisioned, the three services from `examples/distributed` writing into it, and some traffic — and
`scripts/demo.sh down` removes it. It builds the services rather than `go run`ning them, because `go run`'s pid is the
toolchain's and killing it leaves the service holding its port. The datasource URL comes from `WITNESS_PG_URL`;
Grafana expands env vars in provisioning files.

The dashboard panels do not go through the plugin — they are raw SQL against the Postgres datasource, so a view change
means editing `dashboards/witness-overview.json` (and its `dashboard.json` copy) too. Two traps found the hard way, both
of which only appear at runtime: `USING (event_id)` breaks once `witness.spans` is joined in (use an explicit `ON`), and
an empty dashboard variable cast as `''::uuid` is constant-folded into an error — write
`COALESCE(nullif('$var', '')::uuid, column)`.

### Adapters (`adapters/`)

`adapters/log` bridges the stdlib `log.Logger` into Witness: it constructs a `log.Logger` whose `io.Writer` parses the
standard log header (`Llongfile|Lmicroseconds|Lmsgprefix`, so: time then `file:line:`, no date). The caller is the last
whitespace-separated field of the header with its trailing `:` trimmed — do not index header fields positionally, the
flags decide how many there are and re-emits each line as a witness event under a configurable `EventType`. The trick is
a UUID prefix used as a delimiter to split header from body — don't change the `log.New` flags without preserving that
split.

### Conventions to preserve

- `witness.Info/Error/Span/...` are variadic in `Record`. Their `Finish` counterparts are
  closures: `defer finish(record.Int("j", j))` evaluates its arguments *at the defer statement*, capturing pre-return
  values. Always write `defer func() { finish(record.Int("j", j)) }()` when the record depends on the result.

- **Events are independent. Nothing may require that two of them exist, or that they arrived in order.** This is the
  model's central asymmetry with span-based tracing, and every layer has to hold it:
    - A span may have a `start` and no `finish` (process died, span still open), a `finish` and no `start` (ingestion
      began mid-flight, the writer was down, the batch carrying it was dropped), **or neither while still carrying
      events of its own**. `witness.span_pairs` therefore takes its universe from `witness.spans` — every span that
      owns an event — and treats lifecycle events as optional decoration; `span_children` reads parenthood off *any*
      event of the child, since every event carries the whole chain; `span_instances` places a span in a process from
      any one of its events. Three views assumed "a span starts" independently and all three had to be rewritten —
      re-check the assumption before adding a fourth.
    - Order is not guaranteed either: a hand-off's `sent` is legitimately written *after* the matching `received`
      (emitting it only once the broker acked is the correct thing to do), and an event may be delayed arbitrarily.
      **Never infer a role, a direction or a pairing from timestamps** — the event type says which side of a hand-off
      a span is on (`span:link` 2 and `span:message:sent` 24 give, `span:message:received` -24 takes) and that is the
      only thing that may. `link_edges` may report `to_at < from_at`; that means "cannot say", not "negative".
    - Consequence for observers: `observers/otlp` can only attach to a span it saw start, so a finish or a log event
      for an unknown span_id is dropped (it checks the registry and returns) — correct for OTel, whose model has no
      such span, and worth remembering when otlp output looks thinner than Postgres.
- Span identity is a uuid v7 — keep using `uuid.Must(uuid.NewV7())` so identifiers are time-sortable.
- A span is a *point* in the space dimension, not a duration. `span:*:start` / `span:*:finish` are just two events
  sharing a span_id; duration is reconstructed at query time. Don't bake duration into the data model.
- **One owner per span.** Only the process that opened a span emits `start`/`finish` for it; every other process
  *references* it as a link. Two owners make the reconstructed duration span both of them.
- Roles are derived, never stored. If you find yourself wanting to persist what a span *is* on the `Context`, check
  whether position already says it — `SpanFlagInstance` used to be a stored bool and `SpanFlagLink` a stored per-span
  flag; both turned out to be derivable once the model was tightened.
- **Fan-in is a link set, not a merged chain.** A batch worker handling n requests emits **one**
  `witness.ReceivedAll(ctx, msgIDs, ...)` in a span opened for the batch: the event carries the worker's
  chain plus n `SpanFlagLink` ids. Do not re-link every subsequent event (rows × n for no query power), do not fan in an
  event about a single item, and do not give the long-lived worker span the links — put them on a per-batch child span,
  or every request's trace walk drags in the worker's whole history. `core.ObserveLinked` takes the slice; the plural
  helper is `ReceivedAll`. See `examples/batch` and `TestBatchFanIn` in `witness_test.go`.
- **Emit `Sent` after the send succeeded, not before.** Mint the id (`uuid.Must(uuid.NewV7())`), put it in the
  carrier, send, and emit on success — a failed attempt is an error event in the caller's span, not a link nobody will
  ever answer. The old mint-and-emit `InternalMessageSent` could only fire before the send, which is why it is gone. Retries reuse the same msgID and add an `attempts` record
  rather than minting one id per attempt: at-least-once delivery then still draws one edge, and `link_edges` (built on
  `span_link_sides`, one row per pair of spans) dates it from the *first* attempt, so it measures the whole wait.
  A dangling link — sent, never received — is harmless by construction: it yields no `link_edges` row, so the walk
  stops there, and `trace_roots` only looks at inbound receives. It is still a row per attempt in `witness.spans`,
  which is the reason not to mint a fresh id per retry.
- **There is one kind of span.** `witness.Service` / `witness.Worker` and their event types (22 `span:service:*`,
  23 `span:wait_group:*` — note the name never matched the function) are gone. Nothing branched on them: `otlp` routed
  both through the same start/finish handlers and the views matched the whole 20..23 range, so the only difference was
  a string the span's *name* already carries. "Service" was worse than redundant — in this model the service **is** the
  instance span at the head of the chain, the one `instances` names, not a child span below it. A program wanting
  machine-readable span kinds registers a paired custom type (`MustNewEventType`, `|i| >= 1000`, ±) — the views accept
  that range and `witness.event_types` names it.
- **There is no reply half.** A hand-off goes one way: one side gives, the other takes, and an answer is either
  another hand-off with its own id or no event at all — for a synchronous call the round trip is the calling span's
  own duration, which is cheaper and truer than any pair of events. Do not reintroduce a reply: it makes the caller's
  span read as triggered by its callee, and every fix for that (timestamp order, dedicated reply types) has been tried
  and removed. Note also that two spans calling `Link` on one id draw no edge — both emit type 2, and an edge needs a
  taking half (-24).
- **`span:link` (2) is the *sending* side.** `span_link_sides` counts it in `sent_at`, which makes `Link` the way to
  say "about to hand this id off": emit it before the send (`Sent` belongs after), and the edge to whoever calls
  `Received` exists even if the process dies mid-send — the price is a link with no receiving half when the send never
  happened (`SELECT * FROM witness.span_link_sides WHERE sent_at IS NOT NULL AND received_at IS NULL` finds them).
  The corollary is a trap worth stating: **the receiving side of a message must call `Received`, never `Link`**, or it
  is read as a second sender.
- Propagation is transport-agnostic by design: the sender puts a span_id into whatever carrier it has (HTTP header,
  message envelope, gRPC metadata), the receiver *references* it from inside its own span
  with `Link` / `Received`. Nothing in `witness` should know about specific transports.
- **Metrics are events like everything else.** `Count` (delta), `Gauge` (absolute value now) and `Sample` (one
  observation of a distribution) emit `metric:counter` / `metric:gauge` / `metric:histogram`, with the metric name in
  `EventMessage` and the number in a record keyed `value`, placed **first** so an observer taking the first `value`
  record gets the API's number rather than a label the caller happened to name that. `Sample` is deliberately not
  called `Observe` (taken by the observer contract) or `Histogram` (that is the aggregate, not the event) — witness
  emits one sample and leaves bucketing, rates and quantiles to whatever reads them.

  **There is no in-process aggregation**, and adding one is a bigger decision than it looks: the premise of the model
  is that every metric, log line and span boundary is the same kind of object, and a pre-aggregated counter is not an
  event — it has no single date, no caller, no span. `observers/prometheus` already aggregates client-side, Postgres
  aggregates in SQL. For a call site too hot for one event per increment, in order: let the observer decline the type
  (`EventTypeFilter`, ~12 ns), batch at the call site (`Count(ctx, name, 100)` is one event for a hundred increments),
  and only then consider a counter registry in `core`. One emitted metric event costs ~290 ns and 5 allocations, so a
  million increments a second is about a third of a core — measure against that before designing anything.
- Records are append-only key/value pairs where the value is rendered through `AppendValue([]byte)` rather
  than `String()` — this lets implementations stream into a caller-provided buffer with no allocation. New `Record`
  implementations should follow the same pattern.

## Known issues and deliberate gaps

Findings from a full model/schema review. None are bugs in flight — they are
things a future change is likely to trip over. Verified against the code at
the time of writing; re-check before acting.

### Postgres observer

- **One bad row discards the whole batch, silently.** `record_value` is
  `varchar(1022)`, `record_key`/`service_name`-style columns are `varchar(127)`,
  and nothing in Go checks a length before insert. Postgres raises `value too
  long` rather than truncating; `queueEvent`'s `.Exec` callback returns the
  error; `flush` does `SendBatch(...).Close()`, which pgx v5 runs as one
  implicit transaction, so the first failure aborts everything queued with it —
  up to `CollectionMaxSize` events. `flush` only `log.Println`s, and `Dropped()`
  counts channel overflow, not this. Highest-severity item in the schema.
- **`spans_lookup` is `UNIQUE (event_id, span_id)`.** `withChildSpan` and
  `Context.observe` both dedup, so the current helpers are safe; any new path
  that can put one span_id twice into one event takes the whole batch down via
  the mechanism above.
- **No tie-break on `event_date`.** It is `timestamp` (microseconds) while
  `time.Now()` is nanoseconds, and `span_starts`/`span_finishes` are
  `DISTINCT ON (span_id) ... ORDER BY event_date`, so same-microsecond events
  resolve arbitrarily. `event_id` is uuid v7 — `ORDER BY event_date, event_id`
  is deterministic and free. `event_date timestamp DEFAULT NOW()` is also a dead
  default that would lie (ingest time ≠ event time); `queueEvent` always
  supplies the value.
- **`records` is weakly typed and weakly constrained.** No `UNIQUE (event_id,
  record_key)`, `record_key` nullable, every value stored as text —
  `observers/prometheus` parses the `value` record back to float on every
  event, and SQL-side aggregation over metric values needs a cast.
- **No retention or partitioning.** Append-only, three tables growing linearly,
  `records` fastest.

### To consider: a monotonic clock on the event (important)

`event_date` is wall clock — `time.Now()` on the emitting process, stored as
`timestamp`. Every duration in the system is a subtraction of two of those:
`span_pairs.duration`, `link_edges`' wait, the bar the trace waterfall draws.
Wall clock is not monotonic: NTP steps it, leap seconds are smeared, a VM
resumes with a corrected clock, and containers inherit whatever the host did.
A step between a span's start and its finish shows up as a duration that
jumped or went negative, and nothing in the model can tell that from a real
one.

The direction to look at is carrying a **second, monotonic reading** on the
event — Go's `time.Time` already holds one, but it is dropped by every
serialisation, so it would have to be an explicit field (nanoseconds since
process start is the obvious encoding). Points worth settling before doing it:

- It is only comparable **within one instance**. Two processes' monotonic
  clocks share no origin, so a cross-process wait (`link_edges`) still has to
  use wall clock, and the schema would carry two answers whose disagreement is
  itself information.
- Which one the views prefer: same-span and same-instance durations should
  come from the monotonic pair when both events have it, and fall back to
  wall clock otherwise — every event is optional, so "both have it" is not
  guaranteed.
- Cost: one `int8` per event row and one field on the wire, on a table that is
  already the fastest-growing thing in the schema.

### Event types

- **The sign convention is load-bearing in SQL and enforced nowhere in Go.**
  `EventType` carries no open/close/point field, so SQL has to infer it. It
  does so in exactly one place now — `witness.span_start_types` /
  `witness.span_finish_types` in `000_schema.up.sql`, read by both
  `merge_span_cache` and `span_starts`/`span_finishes`, so the cache and the
  views cannot disagree. A custom type counts as a span boundary only if the
  **opposite sign is registered too**: `MustNewEventType` enforces `|i| >=
  1000` and nothing else, and an unpaired custom type is a point event that
  would otherwise name the span after itself and date it from itself.
  Pairing is inferred from `witness.event_types`, so a type registered after
  the observer was built reads as a point event until the next start-up or
  `rebuild_span_cache()` — one more reason custom types belong in `init()`.
  Carrying the role as a field on `EventType` remains the fix direction; it
  would replace the inference, not just the two views.
- **Dead or half-wired types.** `EventTypeLog()` (1) and `EventTypeMetric()` (3)
  are registered and never emitted — they are categories dressed as types.
  `EventTypeMetricGauge()` (30) is registered but
  every observer that consumes metrics handles all three of gauge, counter
  and histogram.
- **`mustNewEventType` appends to the package-level `events` slice without a
  lock** while `Events()` and `printers.Pretty`'s width growth read it. Safe
  only if registration happens in `init()`; nothing enforces that.

### Repository state


- `MIGRATION.md`'s top entry (`v0.31`) is a single consolidated entry for the
  whole model rework. Earlier drafts of it described intermediate steps that
  later changes undid (entering borrowed spans, a durable `SpanFlagLink`, a
  `spanFlags` parameter on `Observe`); those were collapsed out deliberately
  because v0.31 is unreleased and a reader upgrading from v0.30 should not be
  told to do something and then undo it. Keep it that way until v0.31 ships.
- Sub-module `go.mod` files still pin `github.com/imakiri/witness v0.30.x`.
  The workspace overrides that locally; bump the pins after publishing the root
  module.
