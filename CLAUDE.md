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
holds `propagation/`), `adapters/log`, `observers/{multi,otlp,postgres,prometheus,stdlog,tee,test}`, `printers`, `record`, `examples`, `examples/distributed`.
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

- **`witness`** (root) — the call API and nothing else: `Info`/`Warn`/`Debug`/`Error*`, `Span`/`Service`/`Worker`/`SpanStart`/`SpanFinish`, `Instance`/`Test`, `Link`/`LinkTo`, `*Message{Sent,Received}`. 27 functions plus three aliases (`Record`, `EventType`, `Finish`) so its own signatures read without a second import. It declares no types of its own.
- **`witness/core`** — the data model and plumbing: `Context` (+ `With`/`From`/`To`), `Event`, `Observer`/`NilObserver`, `EventType` and its 28 constructors and registry, `SpanFlags`, `Record`, `Printer`/`Appender`/`PrintFlags`, `Caller`/`SetCallDepth`.

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
    2. Open a child (`Span`/`Service`/`Worker`/`SpanStart`) — appends, returns a Context whose current span is the new
       one.
    3. Replace the chain with a fresh instance root (`Instance`/`Test`) — the *only* constructors, and they ignore any
       witness state on the incoming ctx entirely.
    4. Reference a foreign span without entering it (`Link`/`LinkTo`, the message helpers) — appended to that one event
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
  built-in type missing from `events` is a type those consumers silently drop** — that bug hid `span:wait_group:*` and
  every `metric:*` from `stdlog`. `MustNewEventType` / `MustNewErrorEventType` append to the same slice at registration
  time; the second sets the error flag, which is what routes an event to `stdlog`'s error writer and
  trips `test.WithFailOnError`. Both panic on an `i` already registered — `EventTypesCompare` orders on `i` alone, so a
  collision would make a type filter's binary search match the wrong type. The `i` range `(-1000, +1000)` is reserved;
  custom types must use `MustNewEventType` with `|i| >= 1000` and a name ≤128 runes. By convention paired start/finish
  use opposite-sign codes (e.g. `+20`/`-20`).
- **`Printer` / `Appender` / `PrintFlags`** (`core/printer.go`) — rendering is decoupled from transport. An observer that
  writes text takes a `Printer` (`Print(io.Writer, Event, PrintFlags)`) or
  an `Appender` (`Append([]byte, Event, PrintFlags) []byte`) and a `PrintFlags` bitmask selecting which columns to
  emit (`PrintAll` / `PrintNone` at the ends). Implementations live in the `printers` module: `printers.Pretty` (aligned
  columns, widths grown under a mutex as events arrive) and `printers.JSON`.
- **`propagation`** (`propagation/`, in the root module) — W3C `traceparent` `Inject`/`Extract` over
  an `http.Header`. `Inject` takes one span_id and puts it whole into the traceparent's 16-byte trace-id field (witness
  has no trace_id to spend there), so `Extract` recovers the exact uuid; feed it into `LinkTo` or a `*MessageReceived`
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
  *references* the upstream span_id with `LinkTo` or `*MessageReceived`), `Test` (`Instance` bound to a `testing.TB`,
  named `tb.Name()`, versioned `"test"`), `Service`/`Worker` (named long-running or concurrent sub-spans), and `Link` (
  mints a shared span_id, emits `span:link`, returns the id for the carrier) / `LinkTo` (references one that arrived),
  and `InternalMessage{Sent,Received}` / `ExternalMessage{Sent,Received}` — the same pair with message event
  types; `*Sent` mints and returns the msgID, `*Received` takes it. Emit `*Received` inside the span that handles the
  message.
- **`caller`** (`core/caller.go`) — attaches the source location of the witness call to every event, as `file:line`. Uses
  a sync.Pool of PC slices sized by `SetCallDepth` (default 16). `Caller(skip)` counts frames above its own caller:
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
    - The `Finish` closures returned by `Span`/`Service`/`Worker`/`SpanStart`/`Instance`/`Test` do **not** walk the
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
  span_flags), `witness.records`. **One migration: `000_schema.up.sql` / `000_schema.down.sql`**,
  named `NNN_description.{up,down}.sql`. The old `migration.up.sql` + `migration_v2..v6` chain was collapsed into it —
  pre-1.0, and v0.31 dropped four columns every earlier version wrote. The in-place upgrade `ALTER` block lives in that
  file's header comment. A schema change means a new numbered pair *and* the Grafana views together; `postgres_test.go`
  embeds `000_schema.up.sql`, so a rename breaks the integration tests. The Grafana monitor's views, plugin
  queries and dashboard were rebuilt on `span_flags` in the same release — see below.
- **`prometheus`** — declarative: counters and histograms must be pre-declared in `Config`. Events whose `eventMessage`
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
  special-cases it.
- `trace_roots` is the UI's handle on a trace: an entry-point span (its parent is an instance) carrying no inbound
  `*_message:received`. Exactly one per distributed request, on the originating side.
- **A trace is walked at query time**, seeded from a root — `traceWalkCTE` in `plugin/pkg/queries/walk.go`, mirrored in
  the dashboard's raw SQL. The walk is **directed** (parent→child, sender→receiver). Plain co-occurrence over
  `witness.spans` would merge a whole process into one component, because the instance span sits in every one of its
  events.
- The plugin's request types keep `traceID` as an alias for `rootSpanID`, so saved dashboards and links keep working.
- The dashboard variable is still named `selected_trace`; it now holds a root span_id.

The `event_type_names` view mirrors the integer constants in `events.go` — if you add or change a built-in event type,
update both.

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

- Span identity is a uuid v7 — keep using `uuid.Must(uuid.NewV7())` so identifiers are time-sortable.
- A span is a *point* in the space dimension, not a duration. `span:*:start` / `span:*:finish` are just two events
  sharing a span_id; duration is reconstructed at query time. Don't bake duration into the data model.
- **One owner per span.** Only the process that opened a span emits `start`/`finish` for it; every other process
  *references* it as a link. Two owners make the reconstructed duration span both of them.
- Roles are derived, never stored. If you find yourself wanting to persist what a span *is* on the `Context`, check
  whether position already says it — `SpanFlagInstance` used to be a stored bool and `SpanFlagLink` a stored per-span
  flag; both turned out to be derivable once the model was tightened.
- Propagation is transport-agnostic by design: the sender puts a span_id into whatever carrier it has (HTTP header,
  message envelope, gRPC metadata), the receiver *references* it from inside its own span
  with `LinkTo` / `*MessageReceived`. Nothing in `witness` should know about specific transports.
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
- **No `event_types` table.** Names live in Go and are re-mirrored by hand in
  the `event_type_names` view; types registered at runtime via
  `MustNewEventType` never reach Postgres at all, so a SQL consumer cannot name
  them. An upsert at observer start would close both.
- **No retention or partitioning.** Append-only, three tables growing linearly,
  `records` fastest.

### Event types

- **The sign convention is load-bearing in SQL and enforced nowhere.**
  `views.up.sql` hardcodes `event_type BETWEEN 20 AND 29` (open) and
  `BETWEEN -29 AND -20` (close), so **custom span types (`|i| >= 1000`, paired
  ±) are invisible** to `span_starts`/`span_finishes`/`span_pairs`/
  `span_children` — no duration, no parenthood. Carrying open/close/point as a
  field on `EventType` rather than encoding it in the sign of the id is the fix
  direction.
- **Dead or half-wired types.** `EventTypeLog()` (1) and `EventTypeMetric()` (3)
  are registered and never emitted — they are categories dressed as types.
  `EventTypeLogFatal()` (14) and `EventTypeLogErrorDevice()` (102) have no entry
  point in `witness.go`. `EventTypeMetricGauge()` (30) is registered but
  `observers/prometheus` switches only on Counter and Histogram, so a gauge
  event is silently dropped by the only observer that consumes metrics.
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
