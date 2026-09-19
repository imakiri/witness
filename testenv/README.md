# testenv — the whole stack, running

One `go test` brings up Postgres (schema + Grafana views), Kafka, and Grafana
with the witness backend plugin and the trace dashboard provisioned, then runs
two services against it that keep emitting events until you stop them.

```sh
WITNESS_TESTENV=1 go test -count=1 -v -timeout 0 -run TestEnv ./testenv
```

Every flag is load-bearing:

| flag | why |
|---|---|
| `WITNESS_TESTENV=1` | without it the test skips, so a workspace-wide `go test ./...` does not hang forever |
| `-count=1` | a hang-until-Ctrl-C test still caches its PASS; without this the second run prints the *first* run's output and starts nothing |
| `-timeout 0` | otherwise `go test` panics after ten minutes |
| `-v` | otherwise output is buffered and you never see the Grafana URL |

It prints the Grafana URL (a random host port — the containers are
testcontainers', not fixed ports), the Postgres DSN and the Kafka broker. Open
Grafana; the trace waterfall is the home dashboard. Ctrl-C stops the services
and removes the containers.

The plugin backend is rebuilt from the working copy on every start, and the
provisioning directory, the views and the dashboard are mounted from it — edit
one and restart, that is the whole loop.

## What the services do

**ingest** accepts 140 requests a second. Each is a `receive-order` span:
two log events around 50–100 ms of work, then the order is produced to Kafka
and `Sent` is emitted with the id that travelled in the message header — after
the produce succeeded, because a failed produce handed nothing off.

**processor** reads that topic one message at a time and hands each to a pool
of workers. A worker opens `process order` with `Handle`, which records the
received half inside the span that took the work, spends 70–150 ms on it, then
hands the order to an in-process batching sub-service under a **fresh** id.

The batcher collects for 200 ms and settles the batch: one span, one
`HandleAll` naming every id in it, 80–120 ms of work, then a child span
standing in for a call to something outside the system (100–300 ms).

Two things there are deliberate and worth not "fixing":

- **The consumer pool is one reader feeding N workers**, not N readers. A
  consumer group over a one-partition topic gives the assignment to a single
  member, so N readers would silently be one and the demo would run an order
  of magnitude behind its producer while looking healthy.
- **Nothing opens a long-lived span** for the consumer loop or the batcher.
  A span under an instance that nobody triggered is a trace root, and the
  traces list would fill with two roots whose walk drags in the entire history
  of the process. Every span here hangs off the instance ctx per unit of work.

The processor→batcher hop is in-process, which makes this the live exercise of
`link_edges` not requiring the two sides to be different instances.

## Expected shape

After a minute, with `psql "$DSN"`:

```
 span_name           | count | avg ms
 receive-order       |  1777 |     83     <- trace roots, one per request
 process order       |  1764 |    109
 settle batch        |    38 |    290
 settlement-api call |    37 |    188
```

`select count(*) from witness.trace_roots` should track `receive-order`: if it
starts tracking `process order` too, something opened a span that nothing
received into.

`events dropped so far: 0` on the heartbeat is the other number to watch — it
counts events the Postgres observer's channel could not take, and drops show
up in Grafana as holes that read like witness losing data.
