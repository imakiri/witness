-- Witness Postgres schema — consolidated v1..v6.
--
-- Apply this on fresh deployments instead of stacking migration.up.sql,
-- migration_v2.up.sql ... migration_v6.up.sql in order. Existing
-- installations should keep applying the incremental migration_v*.up.sql
-- files.
--
-- After this file, apply observers/postgres/monitors/grafana/views.up.sql to
-- materialize the Grafana views.

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE SCHEMA witness;

CREATE TABLE witness.events
(
    event_id        uuid      NOT NULL PRIMARY KEY,
    event_date      timestamp NOT NULL DEFAULT NOW(),
    event_type      int8      NOT NULL,
    event_message   varchar   NOT NULL,
    event_caller    varchar   NOT NULL
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
    -- span_flags is a bitmask of the roles this span_id plays in this
    -- event. Go holds the span chain ordered root -> leaf; the flags carry
    -- that structure into SQL, which otherwise sees an unordered bag.
    --   1  own       — the event happened directly in this span (exactly one)
    --   2  parent    — direct parent of the own span
    --   4  ancestor  — an enclosing scope above the parent
    --   8  instance  — root span of the process that emitted the event
    --   16 link      — a span the local process did not mint (wire span_id,
    --                  or a msgID shared with the peer of a hand-off)
    -- The roles combine: an instance's own online event is 1|8, a message
    -- hand-off's msgID is 1|16. 0 means the producer did not report roles.
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
