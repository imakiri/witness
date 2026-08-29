# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & test

This is a Go workspace (`go.work`) containing the root module plus several independent sub-modules. Every package is built/tested via its own module — run `go` commands either from the root (uses workspace) or from inside the specific sub-module directory.

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

`examples/` declares its own ad-hoc module (`module examples`) and is meant to be run directly: `cd examples && go run simple.go`.

Sub-modules currently in the workspace: `.` (root, which also holds `propagation/`), `adapters/log`, `observers/{multi,otlp,postgres,prometheus,stdlog,tee,test}`, `printers`, `record`, `examples`, `examples/distributed`. When adding a new sub-module, register it in `go.work` *and* in `scripts/tidy.sh`'s module list, or the workspace and `scripts/release.sh` will both miss it.

`record/` is its own module (`github.com/imakiri/witness/record`) holding the public helpers (`record.String`, `record.Int`, `record.Marshaller`, …). The root package keeps a *separate*, unexported `record` struct because `witness → record/` would close a cycle (`record` imports `witness` for the `Record` interface).

`observers/postgres/monitors/grafana/plugin` is deliberately *outside* the workspace (Grafana SDK deps): build it with `GOWORK=off go build ./...`.

Sub-modules pin a version of the root via `require github.com/imakiri/witness vX.Y.Z`. Locally the workspace overrides this with the working copy; releases must bump the version pins after publishing root.

## Architecture

Witness is a single-entity observability data model: every metric, log line, span boundary, and message hand-off is the same kind of object — an **event** with one time dimension (`event_date`) and N space dimensions (`event_span_ids`). The README has the full data-model rationale and example tables; read it before changing event shapes.

### Core types (root package `witness`)

- **`Observer`** (`observer.go`) — the only interface backends implement: `Observe(Event)`, where `Event` is a struct carrying `SpanIDs`, `EventID`, `EventDate`, `EventType`, `EventMessage`, `EventCaller`, and `Records`. `NilObserver` is the zero-cost no-op.
- **`Context`** (`context.go`) — value type carrying `(observer, spanIDs, traceID, serviceName, t)`. Stored in `context.Context` under an internal key; retrieved with `From(ctx)`, attached with `With(ctx, c)` / `c.To(ctx)`. `Join` merges span-id lists from multiple contexts (sorted+deduped) — that is how disjoint traces (producer/consumer, parent/child goroutines) are stitched together. `traceID` and `serviceName` are set once at `Instance`/`InstanceContinue` and inherited unchanged by every derived Context; `t` is the optional `*testing.T` from `NewTestContext`, used only to call `t.Helper()`. **Every constructor of a derived `Context` must copy all five fields** — a dropped `t` silently breaks test line attribution, a dropped `serviceName` silently NULLs a Postgres column.
- **`EventType`** (`events.go`) — `(errorFlag, int64, string)` triple. All built-in types are functions (`EventTypeLogInfo()`, `EventTypeSpanStart()`, …) that must *also* be listed in the package-level `events` slice; `Events()` returns a copy of it and is what type-filtering observers and column-width calculations read. **A built-in type missing from `events` is a type those consumers silently drop** — that bug hid `span:wait_group:*` and every `metric:*` from `stdlog`. `MustNewEventType` / `MustNewErrorEventType` append to the same slice at registration time; the second sets the error flag, which is what routes an event to `stdlog`'s error writer and trips `test.WithFailOnError`. Both panic on an `i` already registered — `EventTypesCompare` orders on `i` alone, so a collision would make a type filter's binary search match the wrong type. The `i` range `(-1000, +1000)` is reserved; custom types must use `MustNewEventType` with `|i| >= 1000` and a name ≤128 runes. By convention paired start/finish use opposite-sign codes (e.g. `+20`/`-20`).
- **`Printer` / `Appender` / `PrintFlags`** (`printer.go`) — rendering is decoupled from transport. An observer that writes text takes a `Printer` (`Print(io.Writer, Event, PrintFlags)`) or an `Appender` (`Append([]byte, Event, PrintFlags) []byte`) and a `PrintFlags` bitmask selecting which columns to emit (`PrintAll` / `PrintNone` at the ends). Implementations live in the `printers` module: `printers.Pretty` (aligned columns, widths grown under a mutex as events arrive) and `printers.JSON`.
- **`propagation`** (`propagation/`, in the root module) — W3C `traceparent` `Inject`/`Extract` over an `http.Header`; the trace half is the first 16 bytes of a uuid, the span half the last 8. Feed `Extract`'s results into `InstanceContinue`. `observers/otlp/propagation.go` is a deprecated shim over this.
- **`Record`** (`record.go` at root, and module `record/`) — interface (`AppendKey`, `AppendValue`, `KeyEqual`). The root package has a minimal internal `record` struct; the `github.com/imakiri/witness/record` module is the public toolkit (`String`, `Int`, `Float`, `Bool`, `Bytes`, `Stringer`, `Error`, plus `Marshaller` for reflection-based struct→records).
- **Top-level helpers** (`witness.go`) — `Info/Warn/Debug/Error`, the `*F` error variants that also `return fmt.Errorf(...)`, `Span` (creates child span, returns ctx + `Finish` closure), `SpanStart`/`SpanFinish` (manual span_id for cross-process propagation), `Instance` (root span representing a process/service lifecycle), `Service`/`Worker` (named long-running or concurrent sub-spans), and `InternalMessage{Sent,Received}` / `ExternalMessage{Sent,Received}` (message-passing events that share a `msgID` between sender and receiver).
- **`caller`** (`caller.go`) — attaches the source location of the witness call to every event, as `file:line`. Uses a sync.Pool of PC slices sized by `SetCallDepth` (default 16).

  **The caller contract:** *the reported location is the line on which the witness entry point (`Info`, `Error`, `Span`, `SpanFinish`, …) is written.* Everything in `caller.go` exists to uphold that one sentence, and `TestCallers` in `caller_test.go` checks it across every entry point and call shape — add a subtest there when you add an entry point. The rule is absolute: if a shape does not satisfy it, fix `caller` or declare the shape unsupported — do not weaken the sentence. Consequences worth knowing:

  - The location is always `file:line`. There used to be an `EnableDebug()` flag selecting between `file:line` and the enclosing function's name; the name cannot express the rule, so the flag and its two formats are gone. `frame.File` is the *builder's* absolute path — build with `-trimpath` if that must not reach telemetry.
  - `caller` reports the **immediate** frame above `skip`; it does no filtering. A call inside a closure, a goroutine literal, or an `http.HandlerFunc` reports the line inside that literal. An earlier version skipped anonymous `.func` frames looking for a named one, which made calls in goroutine literals report `runtime.goexit` and calls in subtests report `testing.go` — that is the bug the contract was written to close, so do not reintroduce frame filtering.
  - A house wrapper (`func (l *myLogger) Info(m string) { witness.Info(l.ctx, m) }`) is attributed to the `witness.Info` line inside the wrapper. That is the rule holding, not an exception: `witness.Info` really is written there. There is no exported way to skip a frame — `Context.Observe` takes a caller string, but the package exports nothing that produces one.
  - It uses `runtime.CallersFrames`, never `FuncForPC`/`FileLine` on a raw pc: only `CallersFrames` does the return-address adjustment and expands inlined frames. Without it the line drifts by a build-flags-dependent amount.
  - The `Finish` closures returned by `Span`/`Trace`/`Service`/`Worker`/`Instance`/`InstanceContinue` do **not** walk the stack. Each constructor captures its own call site eagerly into `at` and the closure reuses it, so a span's start and finish events report the same line — the line that opened the span. (`InstanceContinue` with no upstream trace delegates to the unexported `instance`, which takes `at` as a parameter for exactly this reason.)
  - **One usage is unsupported**, because the runtime makes the rule impossible to uphold there: deferring a witness helper *directly* — `defer witness.Info(...)`, `defer witness.SpanFinish(...)`. The body runs at function exit, and a deferred call's own pc is not reachable from the stack (`_defer.pc` holds it but is unexported, and open-coded defers allocate no `_defer` record outside a panic). Write `defer func() { witness.Info(...) }()` instead — a deferred closure has its own frame sitting on the witness call and reports it correctly. Constructors returning a `Finish` are immune.
### Observers (`observers/`)

Each observer is its own Go module so users only pull in the dependencies they actually use.

- **`stdlog`** — synchronous writer; takes a `witness.Printer` (normally `printers.Pretty`) plus options: `WithFlags` selects columns, `WithTypes` restricts to a set of event types, `WithWriter`/`WithErrorWriter` set the destinations for non-error and error (`EventType.IsError`) events. `WithErrorWriter` defaults to `WithWriter`, which defaults to `os.Stdout` — errors are not split out unless asked; pass `io.MultiWriter(os.Stdout, os.Stderr)` to send them to both. `WithUsingStdErr` is the deprecated `WithErrorWriter(os.Stderr)`. Without `WithTypes` there is *no* type filtering — deliberately, because snapshotting `witness.Events()` at construction dropped every type registered by a later `MustNewEventType`.
- **`multi`** — fan-out to several observers. `tee` is the same thing, deprecated in its favour.
- **`test`** — routes events into `t.Logf`; `WithFailOnError` fails the test on any `EventType.IsError()` event.
- **`postgres`** — async batching observer. `worker` goroutines (one per `pgxpool` MaxConn) drain a buffered channel into a `pgx.Batch` until either `CollectionMaxSize` events or `CollectionDuration` elapses, then ship the batch. `Observe` never blocks — a full channel drops the event and bumps `Dropped()`; `Close` is idempotent and drains under `ShutdownTimeout`. Three tables: `witness.events`, `witness.spans` (event_id ↔ span_id), `witness.records`. **Fresh deploys apply `schema.up.sql`** (consolidated v1–v4); `migration.up.sql` + `migration_v{2,3,4}.up.sql` are the incremental path for already-deployed installations. `queueEvent` writes `trace_id`, `parent_trace_id`, `parent_span_id` and `service_name`, all of which only exist from v2/v3/v4 onward — adding a column to `Event` means touching `schema.up.sql`, a new `migration_v*`, and the Grafana views together.
- **`prometheus`** — declarative: counters and histograms must be pre-declared in `Config`. Events whose `eventMessage` doesn't match a configured metric name are silently dropped. The `value` record is the delta/observation; other records are matched against `LabelKeys`. Exposes `.Handler()` for the `/metrics` endpoint.
- **`otlp`** — exports witness events as OpenTelemetry spans over OTLP (gRPC or HTTP) to a collector. Maintains a `sync.Map` registry of witness `span_id → otel trace.Span`; start events call `tracer.Start`, finish events call `span.End`, log/error events become `AddEvent` / `SetStatus(Error)` / `RecordError`. `traceID` is the raw 16 bytes of the root witness span_id; `spanID` is the last 8 bytes of the current witness span_id (no string formatting — `cleanTraceID`-style dash-stripping was a bug in the reference impl). Cross-process propagation lives in the root `propagation` package (`otlp.Inject`/`otlp.Extract` are deprecated shims). `NewTraceProvider` returns errors instead of `log.Fatal` — never use `log.Fatal` from inside an observer. The registry has no eviction: a witness span whose finish event never arrives keeps its otel span alive for the Observer's lifetime. That is intentional — witness spans have no timeout, and force-ending one would invent a duration the data model does not carry.

`observers/postgres/monitors/grafana` ships a Grafana dashboard plus SQL views (`span_starts`, `span_finishes`, `span_pairs`, `span_children`, `event_records_json`, `event_type_names`) for querying the Postgres tables. The `event_type_names` view mirrors the integer constants in `events.go` — if you add or change a built-in event type, update both.

### Adapters (`adapters/`)

`adapters/log` bridges the stdlib `log.Logger` into Witness: it constructs a `log.Logger` whose `io.Writer` parses the standard log header (`Llongfile|Lmicroseconds|Lmsgprefix`, so: time then `file:line:`, no date). The caller is the last whitespace-separated field of the header with its trailing `:` trimmed — do not index header fields positionally, the flags decide how many there are and re-emits each line as a witness event under a configurable `EventType`. The trick is a UUID prefix used as a delimiter to split header from body — don't change the `log.New` flags without preserving that split.

### Conventions to preserve

- `witness.Info/Error/Span/...` are variadic in `Record`. Their `Finish` counterparts are closures: `defer finish(record.Int("j", j))` evaluates its arguments *at the defer statement*, capturing pre-return values. Always write `defer func() { finish(record.Int("j", j)) }()` when the record depends on the result.

- Span identity is a uuid v7 — keep using `uuid.Must(uuid.NewV7())` so identifiers are time-sortable.
- A span is a *point* in the space dimension, not a duration. `span:*:start` / `span:*:finish` are just two events sharing a span_id; duration is reconstructed at query time. Don't bake duration into the data model.
- Propagation is transport-agnostic by design: senders put a span_id into whatever carrier they have (HTTP header, message envelope, etc.), the receiver attaches it via `SpanStart`/`SpanFinish` or by joining contexts. Nothing in `witness` should know about specific transports.
- Records are append-only key/value pairs where the value is rendered through `AppendValue([]byte)` rather than `String()` — this lets implementations stream into a caller-provided buffer with no allocation. New `Record` implementations should follow the same pattern.
