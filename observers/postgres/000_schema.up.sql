-- Witness Postgres schema.
--
-- One migration, applied to an empty database. The incremental
-- migration.up.sql + migration_v2..v6 chain that used to live here was
-- collapsed into this file: witness is pre-1.0, v0.31 removed four columns
-- that every earlier version wrote, and carrying a rollout path for a shape
-- nothing produces any more only invited applying it.
--
-- Upgrading an installation created before v0.31: the columns witness no
-- longer writes are dropped and one is added. If the old rows are worth
-- keeping, run this instead of recreating the schema:
--
--   DROP INDEX IF EXISTS witness.events_parent_trace_lookup;
--   DROP INDEX IF EXISTS witness.events_trace_id_idx;
--   DROP INDEX IF EXISTS witness.events_service_lookup;
--   ALTER TABLE witness.events
--       DROP COLUMN IF EXISTS trace_id,
--       DROP COLUMN IF EXISTS parent_trace_id,
--       DROP COLUMN IF EXISTS parent_span_id,
--       DROP COLUMN IF EXISTS service_name;
--   ALTER TABLE witness.spans
--       ADD COLUMN IF NOT EXISTS span_flags int8 NOT NULL DEFAULT 0;
--
-- Upgrading an installation created before the denormalised event_date on
-- spans and records (v0.31 development): add the column, backfill it from
-- events, then make it NOT NULL and rebuild the two partial indexes below.
-- The backfill is one pass over the whole table, so do it in batches on a
-- large installation:
--
--   ALTER TABLE witness.spans   ADD COLUMN event_date timestamp;
--   ALTER TABLE witness.records ADD COLUMN event_date timestamp;
--   UPDATE witness.spans   s SET event_date = e.event_date
--     FROM witness.events e WHERE e.event_id = s.event_id AND s.event_date IS NULL;
--   UPDATE witness.records r SET event_date = e.event_date
--     FROM witness.events e WHERE e.event_id = r.event_id AND r.event_date IS NULL;
--   ALTER TABLE witness.spans   ALTER COLUMN event_date SET NOT NULL;
--   ALTER TABLE witness.records ALTER COLUMN event_date SET NOT NULL;
--
-- then create spans_instance_lookup and spans_own_lookup as below. Rows
-- written before that keep span_flags = 0, which reads as "the producer did
-- not report roles" — they are not backfilled, because the ordering the
-- roles are derived from was never stored.
--
-- After this file, apply monitors/grafana/views.up.sql for the Grafana views.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE SCHEMA witness;

CREATE TABLE witness.events
(
    event_id      uuid      NOT NULL PRIMARY KEY,
    event_date    timestamp NOT NULL,
    event_type    int8      NOT NULL,
    event_message varchar   NOT NULL,
    event_caller  varchar   NOT NULL
);

CREATE INDEX events_event_lookup
    ON witness.events (event_date DESC, event_type, event_message);

-- Full-text and trigram search over event_message. tsvector handles tokenised
-- search; pg_trgm is the fallback for short substrings (ILIKE).
CREATE INDEX events_message_fts
    ON witness.events USING GIN (to_tsvector('simple', event_message));

CREATE INDEX events_message_trgm
    ON witness.events USING GIN (event_message gin_trgm_ops);

CREATE TABLE witness.spans
(
    event_id   uuid      NOT NULL REFERENCES witness.events (event_id),
    -- The event's date, denormalised from witness.events. It buys three
    -- things and is the reason the redundancy is worth it: a query walking
    -- spans can bound itself in time without joining events (every walk we
    -- have is by id, and unbounded it reads the whole table), the partial
    -- indexes below can carry the date, and a Timescale deployment can
    -- partition this table at all — a hypertable needs its partitioning
    -- column here, and a unique index that does not include it is rejected.
    --
    -- Written from the same core.Event as the events row, in the same batch,
    -- so the two cannot disagree; nothing reads one and writes the other.
    event_date timestamp NOT NULL,
    span_id    uuid      NOT NULL,
    -- span_flags is a bitmask of the role this span_id plays in this event.
    -- Go holds the emitter's span chain ordered root -> leaf and the roles
    -- follow from position; the flags carry that structure into SQL, which
    -- otherwise sees an unordered bag.
    --   1  own       — the event happened directly in this span (exactly one)
    --   2  parent    — direct parent of the own span
    --   4  ancestor  — an enclosing scope above the parent
    --   8  instance  — root span of the process that emitted the event
    --   16 link      — a span this event references without being inside it
    --
    -- The chain's flags combine (an instance's own online event is 1|8); a
    -- link never does — it carries 16 and nothing else, because the process
    -- never entered it. 0 means the producer did not report roles.
    span_flags int8      NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX spans_lookup ON witness.spans (event_id DESC, span_id DESC);

-- Reverse direction: pasted span_id → events that mention it.
CREATE INDEX spans_by_span ON witness.spans (span_id, event_date DESC);

-- By date alone: a walk bounds itself in time before it knows which spans it
-- wants, so the edge sets it builds are read as a range over this index.
-- Without it the bound is a filter over the whole table and buys nothing.
CREATE INDEX spans_by_date ON witness.spans (event_date DESC);

-- Emitting instance of an event, and every event of one instance.
CREATE INDEX spans_instance_lookup
    ON witness.spans (span_id, event_date DESC, event_id)
    WHERE span_flags & 8 <> 0;

-- Events located directly in a span, excluding its descendants. The date
-- comes second so "this span, since then" is a range scan rather than a
-- filter over everything the span ever emitted.
CREATE INDEX spans_own_lookup
    ON witness.spans (span_id, event_date DESC, event_id)
    WHERE span_flags & 1 <> 0;

-- The event types the producer knows about, upserted by the observer at
-- start-up from core.Events(). SQL has no other way to name a type or to
-- know which ones are errors: both live in Go, and MustNewEventType lets a
-- program register its own at runtime, so a hardcoded list in this file
-- would be a copy that silently rots and could never cover custom types.
--
-- Written once per process start, never by the event path. A type
-- registered *after* the observer was built is not in here — register
-- custom types in init().
CREATE TABLE witness.event_types
(
    event_type      int8         NOT NULL PRIMARY KEY,
    event_type_name varchar(128) NOT NULL,
    -- Mirrors core.EventType.IsError(): the flag that routes an event to
    -- stdlog's error writer and trips test.WithFailOnError. Grafana's error
    -- counts read it instead of hardcoding a list of ids.
    is_error        boolean      NOT NULL
);

CREATE TABLE witness.records
(
    event_id     uuid      NOT NULL REFERENCES witness.events (event_id),
    -- Denormalised for the same reasons as witness.spans.event_date: a
    -- record search can bound itself in time without joining events, and a
    -- Timescale deployment needs the column to partition on.
    event_date   timestamp NOT NULL,
    record_key   varchar(127),
    record_value varchar(1022)
);

CREATE INDEX records_lookup ON witness.records (event_id DESC, record_key);

-- Search by record key/value, both exact and substring.
CREATE INDEX records_value_trgm
    ON witness.records USING GIN (record_value gin_trgm_ops);

CREATE INDEX records_key_value
    ON witness.records (record_key, record_value);

-- ---------------------------------------------------------------------------
-- Derived span cache
--
-- Everything above a raw event — what a span is, who opened it, who handed it
-- work — is a *fact about a span* that the writer already knows and that SQL
-- otherwise has to re-derive on every query. Re-deriving it is what made the
-- Grafana views expensive: reading one span through witness.span_starts costs
-- 223 ms on half a million events (it is a DISTINCT ON over the whole table
-- and the predicate does not push through it), while the same row from these
-- tables costs 0.072 ms. The event trail went from 3145 ms to 2.5 ms on the
-- same data, returning the same rows.
--
-- These tables are a **cache, not truth**. witness.events is the only truth;
-- everything here is derivable from it and can be dropped and rebuilt with
-- witness.rebuild_span_cache() at any time (5.6 s per 500k events). Nothing
-- but witness.merge_span_cache() may write to them.
--
-- They cost about 12% of the base tables: 570 B per span against 891 B per
-- event, so the ratio follows the shape of the traces — 12% at 5.4 events per
-- span, ~32% at 2, ~6% at 10.

-- One row per span.
CREATE TABLE witness.span_facts
(
    span_id          uuid      NOT NULL PRIMARY KEY,
    -- From the span's start event where it has one, from its finish where it
    -- has only that. A span with neither is legal and stays unnamed.
    span_name        varchar,
    started_at       timestamp,
    start_event_id   uuid,
    finished_at      timestamp,
    finish_event_id  uuid,
    instance_span_id uuid,
    -- First and last event the span owns. Unlike started_at/finished_at these
    -- are never NULL: a span exists here because it owns an event.
    first_at         timestamp NOT NULL,
    last_at          timestamp NOT NULL,
    -- The one field of this schema that is not idempotent: applying the same
    -- batch twice counts its events twice. The observer never replays a
    -- batch; a writer that does must rebuild instead.
    event_count      int8      NOT NULL
);

CREATE INDEX span_facts_by_instance ON witness.span_facts (instance_span_id, first_at DESC);
CREATE INDEX span_facts_by_time     ON witness.span_facts (first_at DESC);

-- One row per edge *backwards*: how a span came to exist or came to have work.
--
--   relation 'parent' — the span was opened by from_span_id. cut is the
--                       child's start event: the parent's events up to that
--                       point are its causes. A child with no start event has
--                       a NULL cut, which the walk reads as "keep my own".
--   relation 'link'   — the span took a hand-off from from_span_id. cut is
--                       the giving event, guard the receiving one, so a walk
--                       can tell whether the hand-off had already happened at
--                       the point it is asking about.
--
-- Written when the second half of a hand-off arrives, whichever half that is.
CREATE TABLE witness.span_edges
(
    to_span_id   uuid        NOT NULL,
    from_span_id uuid        NOT NULL,
    relation     varchar(16) NOT NULL,
    cut_date     timestamp,
    cut_id       uuid,
    guard_date   timestamp,
    guard_id     uuid,
    -- When this edge happened on the receiving side: what a walk bounds
    -- itself with. Never NULL, unlike cut_date.
    at           timestamp   NOT NULL,
    PRIMARY KEY (to_span_id, from_span_id, relation)
);

-- The walk that exists goes backwards, so only that direction is indexed.
-- A forward index (from_span_id) costs 13 MB per 180k edges and nothing reads
-- it yet; add it with the first query that walks downstream.
CREATE INDEX span_edges_back ON witness.span_edges (to_span_id, at DESC);

-- merge_span_cache folds a set of events into the cache. Called by the
-- observer once per batch with that batch's event ids, and with NULL by
-- rebuild_span_cache() to fold in everything.
--
-- **Every merge here is commutative**, and that is what makes the cache
-- correct under witness's model rather than merely fast: events are
-- independent and unordered, so a start may arrive after its finish, a finish
-- may never arrive, and a `sent` is legitimately written after the matching
-- `received`. first_at is a least, last_at a greatest, names and ids a
-- coalesce, the start the earliest and the finish the latest — none of them
-- depends on the order the events land in. Do not add a field whose merge
-- does.
--
-- uuid has no min()/max(); an id is picked with (array_agg(... ORDER BY ...))[1]
-- so the choice matches the ordering the dates were chosen with.
CREATE FUNCTION witness.merge_span_cache(event_ids uuid[] DEFAULT NULL) RETURNS void AS
$$
BEGIN
    INSERT INTO witness.span_facts AS f
        (span_id, span_name, started_at, start_event_id, finished_at, finish_event_id,
         instance_span_id, first_at, last_at, event_count)
    SELECT own.span_id,
           (array_agg(e.event_message ORDER BY e.event_date, e.event_id)
              FILTER (WHERE e.event_type BETWEEN 20 AND 21 OR e.event_type >= 1000))[1],
           min(e.event_date)
              FILTER (WHERE e.event_type BETWEEN 20 AND 21 OR e.event_type >= 1000),
           (array_agg(e.event_id ORDER BY e.event_date, e.event_id)
              FILTER (WHERE e.event_type BETWEEN 20 AND 21 OR e.event_type >= 1000))[1],
           max(e.event_date)
              FILTER (WHERE e.event_type BETWEEN -21 AND -20 OR e.event_type <= -1000),
           (array_agg(e.event_id ORDER BY e.event_date DESC, e.event_id DESC)
              FILTER (WHERE e.event_type BETWEEN -21 AND -20 OR e.event_type <= -1000))[1],
           (array_agg(inst.span_id) FILTER (WHERE inst.span_id IS NOT NULL))[1],
           min(e.event_date), max(e.event_date), count(*)
      FROM witness.events e
      JOIN witness.spans own       ON own.event_id  = e.event_id AND own.span_flags & 1 <> 0
      LEFT JOIN witness.spans inst ON inst.event_id = e.event_id AND inst.span_flags & 8 <> 0
     WHERE event_ids IS NULL OR e.event_id = ANY (event_ids)
     GROUP BY own.span_id
    ON CONFLICT (span_id) DO UPDATE SET
        span_name        = coalesce(f.span_name, excluded.span_name),
        started_at       = least(f.started_at, excluded.started_at),
        start_event_id   = coalesce(f.start_event_id, excluded.start_event_id),
        finished_at      = greatest(f.finished_at, excluded.finished_at),
        finish_event_id  = coalesce(f.finish_event_id, excluded.finish_event_id),
        instance_span_id = coalesce(f.instance_span_id, excluded.instance_span_id),
        first_at         = least(f.first_at, excluded.first_at),
        last_at          = greatest(f.last_at, excluded.last_at),
        event_count      = f.event_count + excluded.event_count;

    -- Parenthood is stated by every event of the child (span_flags & 2), so
    -- it does not wait for a start event; the cut does, and is NULL until one
    -- arrives.
    INSERT INTO witness.span_edges AS ed
        (to_span_id, from_span_id, relation, cut_date, cut_id, guard_date, guard_id, at)
    SELECT own.span_id, par.span_id, 'parent',
           min(e.event_date)
              FILTER (WHERE e.event_type BETWEEN 20 AND 21 OR e.event_type >= 1000),
           (array_agg(e.event_id ORDER BY e.event_date, e.event_id)
              FILTER (WHERE e.event_type BETWEEN 20 AND 21 OR e.event_type >= 1000))[1],
           NULL, NULL,
           min(e.event_date)
      FROM witness.events e
      JOIN witness.spans own ON own.event_id = e.event_id AND own.span_flags & 1 <> 0
      JOIN witness.spans par ON par.event_id = e.event_id AND par.span_flags & 2 <> 0
     WHERE event_ids IS NULL OR e.event_id = ANY (event_ids)
     GROUP BY own.span_id, par.span_id
    ON CONFLICT (to_span_id, from_span_id, relation) DO UPDATE SET
        cut_date = least(ed.cut_date, excluded.cut_date),
        cut_id   = coalesce(ed.cut_id, excluded.cut_id),
        at       = least(ed.at, excluded.at);

    -- The hand-off edge, resolved for every link this batch touched. Both
    -- halves are looked up in witness.spans by the link's span_id, which is
    -- an index hit: whichever half arrives second finds the first and writes
    -- the edge, and a hand-off whose sides are hours apart still produces one
    -- edge and no duplicate.
    --
    -- Which side is which is read off the event type, never off the dates —
    -- a `sent` emitted once the broker acked is legitimately written after
    -- the matching `received`. A retried send keeps the first attempt, so an
    -- edge measures the whole wait rather than the last try. Self-links are
    -- not edges.
    INSERT INTO witness.span_edges AS ed
        (to_span_id, from_span_id, relation, cut_date, cut_id, guard_date, guard_id, at)
    WITH touched AS (
        SELECT DISTINCT s.span_id AS link_span_id
          FROM witness.spans s
         WHERE s.span_flags & 16 <> 0
           AND (event_ids IS NULL OR s.event_id = ANY (event_ids))
    ),
    sides AS (
        SELECT own.span_id, l.span_id AS link_span_id,
               e.event_type, e.event_date, e.event_id
          FROM touched t
          JOIN witness.spans  l   ON l.span_id    = t.link_span_id AND l.span_flags & 16 <> 0
          JOIN witness.events e   ON e.event_id   = l.event_id
          JOIN witness.spans  own ON own.event_id = l.event_id AND own.span_flags & 1 <> 0
         WHERE e.event_type IN (2, 24, -24)
    ),
    give AS (
        SELECT DISTINCT ON (span_id, link_span_id) span_id, link_span_id, event_date, event_id
          FROM sides WHERE event_type IN (2, 24)
         ORDER BY span_id, link_span_id, event_date, event_id
    ),
    take AS (
        SELECT DISTINCT ON (span_id, link_span_id) span_id, link_span_id, event_date, event_id
          FROM sides WHERE event_type = -24
         ORDER BY span_id, link_span_id, event_date, event_id
    )
    SELECT tk.span_id, gv.span_id, 'link',
           gv.event_date, gv.event_id, tk.event_date, tk.event_id, tk.event_date
      FROM take tk
      JOIN give gv ON gv.link_span_id = tk.link_span_id
                  AND gv.span_id IS DISTINCT FROM tk.span_id
    ON CONFLICT (to_span_id, from_span_id, relation) DO UPDATE SET
        cut_date   = least(ed.cut_date, excluded.cut_date),
        cut_id     = coalesce(ed.cut_id, excluded.cut_id),
        guard_date = least(ed.guard_date, excluded.guard_date),
        guard_id   = coalesce(ed.guard_id, excluded.guard_id),
        at         = least(ed.at, excluded.at);
END;
$$ LANGUAGE plpgsql;

-- Rebuild the cache from scratch. Safe at any time: the cache holds nothing
-- witness.events does not. Truncates first because event_count sums.
CREATE FUNCTION witness.rebuild_span_cache() RETURNS void AS
$$
BEGIN
    TRUNCATE witness.span_facts, witness.span_edges;
    PERFORM witness.merge_span_cache(NULL);
END;
$$ LANGUAGE plpgsql;
