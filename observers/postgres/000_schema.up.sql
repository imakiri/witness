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
    event_id   uuid NOT NULL REFERENCES witness.events (event_id),
    span_id    uuid NOT NULL,
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
    span_flags int8 NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX spans_lookup ON witness.spans (event_id DESC, span_id DESC);

-- Reverse direction: pasted span_id → events that mention it.
CREATE INDEX spans_by_span ON witness.spans (span_id);

-- Emitting instance of an event, and every event of one instance.
CREATE INDEX spans_instance_lookup
    ON witness.spans (span_id, event_id)
    WHERE span_flags & 8 <> 0;

-- Events located directly in a span, excluding its descendants.
CREATE INDEX spans_own_lookup
    ON witness.spans (span_id, event_id)
    WHERE span_flags & 1 <> 0;

CREATE TABLE witness.records
(
    event_id     uuid NOT NULL REFERENCES witness.events (event_id),
    record_key   varchar(127),
    record_value varchar(1022)
);

CREATE INDEX records_lookup ON witness.records (event_id DESC, record_key);

-- Search by record key/value, both exact and substring.
CREATE INDEX records_value_trgm
    ON witness.records USING GIN (record_value gin_trgm_ops);

CREATE INDEX records_key_value
    ON witness.records (record_key, record_value);
