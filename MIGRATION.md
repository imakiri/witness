# Upgrading to v1.0

Two things broke between v0.x and v1.0. Everything else is additive.

## Drop the standalone `record` module from go.mod

The `record/` sub-package no longer lives in its own Go module — it's
folded into the root `witness` module. Import paths are unchanged, so the
code keeps compiling, but the explicit `require` line has to go:

```
require (
-   github.com/imakiri/witness        v0.20.0
-   github.com/imakiri/witness/record v0.11.0
+   github.com/imakiri/witness v1.0.0
)
```

Then `go mod tidy`. Done.

## `Observer.Observe` takes an `Event` struct

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

## Smaller things

- `postgres.Event` is gone; the observer now uses `witness.Event`. Replace
  any explicit references.
- The OTLP observer requires Go 1.25 (transitive OTel constraint). The root
  module still targets Go 1.22, so this only matters if you import
  `observers/otlp`.

## Single-require alternative: `witness/all`

If you'd rather depend on the whole bundle than list each observer
separately in `go.mod`, replace your individual requires with one:

```
require github.com/imakiri/witness/all v1.0.0-dev
```

That transitively pulls every observer/adapter shipped in this repo.
Code-level imports stay the same — `witness/all` is a dependency
aggregator, not a re-export.

## What's new

- `witness.Service`, `witness.Worker` — named sub-spans for long-running
  services and concurrent workers.
- `witness.InternalMessageSent` / `InternalMessageReceived` and the
  matching `ExternalMessage*` pair for message-passing events.
- `observers/otlp` — exports witness traces over OTLP (gRPC or HTTP) into
  Jaeger / Tempo / any OTLP collector. Includes W3C `traceparent`
  inject/extract.
- Postgres observer fixes: clean shutdown (the worker loop was inverted)
  and no more unbounded goroutine spawn under load.
