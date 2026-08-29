# Upgrading

Witness is pre-1.0. Breaking changes are called out here, newest first;
everything not listed is additive.

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
  `witness.InstanceContinue`.
- `observers/otlp` — exports witness traces over OTLP (gRPC or HTTP) into
  Jaeger / Tempo / any OTLP collector.
- `observers/multi` (replaces the deprecated `tee`) and `observers/test`.
- Postgres observer fixes: clean shutdown (the worker loop was inverted)
  and no more unbounded goroutine spawn under load.
