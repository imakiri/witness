-- Witness Postgres schema v2.
--
-- Apply after migration.up.sql (v1). Adds cross-service trace continuity
-- (parent_trace_id / parent_span_id on events) and the indexes required by
-- the Grafana plugin's search / trace-reconstruction queries.

ALTER TABLE witness.events
    ADD COLUMN IF NOT EXISTS parent_trace_id uuid NULL,
    ADD COLUMN IF NOT EXISTS parent_span_id  uuid NULL;

-- Find every child instance whose upstream parent lives in trace X.
CREATE INDEX IF NOT EXISTS events_parent_trace_lookup
    ON witness.events (parent_trace_id, parent_span_id)
    WHERE parent_trace_id IS NOT NULL;

-- Full-text and trigram search over event_message. tsvector handles tokenised
-- search; pg_trgm is the fallback for short substrings (ILIKE).
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS events_message_fts
    ON witness.events USING GIN (to_tsvector('simple', event_message));

CREATE INDEX IF NOT EXISTS events_message_trgm
    ON witness.events USING GIN (event_message gin_trgm_ops);

-- The v1 spans index is keyed (event_id DESC, span_id DESC) — fine for
-- finding spans of a known event, useless for the reverse direction we need
-- when the user pastes a span_id into the search box.
CREATE INDEX IF NOT EXISTS spans_by_span ON witness.spans (span_id);

-- Search by record key/value, both exact and substring.
CREATE INDEX IF NOT EXISTS records_value_trgm
    ON witness.records USING GIN (record_value gin_trgm_ops);

CREATE INDEX IF NOT EXISTS records_key_value
    ON witness.records (record_key, record_value);
