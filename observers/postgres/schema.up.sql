-- Witness Postgres schema — consolidated v1+v2+v3+v4.
--
-- Apply this on fresh deployments instead of stacking migration.up.sql,
-- migration_v2.up.sql, migration_v3.up.sql and migration_v4.up.sql in
-- order. Existing installations should keep applying the incremental
-- migration_v*.up.sql files.
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
    event_caller    varchar   NOT NULL,
    -- trace_id is the logical request identifier — same across every span
    -- produced while handling one request, including cross-service hops.
    trace_id        uuid      NULL,
    -- parent_trace_id / parent_span_id pin a span:instance:online event to
    -- an externally-provided trace context (typically from a W3C
    -- traceparent header). Non-null only on InstanceContinue's online event.
    parent_trace_id uuid      NULL,
    parent_span_id  uuid      NULL,
    -- service_name is the local instance's name (the string passed to
    -- witness.Instance / witness.InstanceContinue). Every event emitted by
    -- one instance shares the same service_name; cross-service hops reset
    -- it on the receiver. NULL when an event was emitted from a Context
    -- that never went through an Instance constructor.
    service_name    varchar(127) NULL
);

CREATE INDEX events_event_lookup
    ON witness.events (event_date DESC, event_type, event_message);

-- Find every child instance whose upstream parent lives in trace X.
CREATE INDEX events_parent_trace_lookup
    ON witness.events (parent_trace_id, parent_span_id)
    WHERE parent_trace_id IS NOT NULL;

-- Full-text and trigram search over event_message. tsvector handles tokenised
-- search; pg_trgm is the fallback for short substrings (ILIKE).
CREATE INDEX events_message_fts
    ON witness.events USING GIN (to_tsvector('simple', event_message));

CREATE INDEX events_message_trgm
    ON witness.events USING GIN (event_message gin_trgm_ops);

-- Per-request lookup: WHERE trace_id = $1 ORDER BY event_date DESC.
CREATE INDEX events_trace_id_idx
    ON witness.events (trace_id, event_date DESC)
    WHERE trace_id IS NOT NULL;

-- Per-service lookup, optionally narrowed by trace_id via events_trace_id_idx.
CREATE INDEX events_service_lookup
    ON witness.events (service_name, event_date DESC)
    WHERE service_name IS NOT NULL;

CREATE TABLE witness.spans
(
    event_id uuid NOT NULL REFERENCES witness.events (event_id),
    span_id  uuid NOT NULL
);

CREATE UNIQUE INDEX spans_lookup ON witness.spans (event_id DESC, span_id DESC);

-- Reverse direction: pasted span_id → events that mention it.
CREATE INDEX spans_by_span ON witness.spans (span_id);

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
