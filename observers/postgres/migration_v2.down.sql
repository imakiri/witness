-- Reverse of migration_v2.up.sql. pg_trgm extension is intentionally not
-- dropped — it may be in use by other tenants of the database.

DROP INDEX IF EXISTS witness.records_key_value;
DROP INDEX IF EXISTS witness.records_value_trgm;
DROP INDEX IF EXISTS witness.spans_by_span;
DROP INDEX IF EXISTS witness.events_message_trgm;
DROP INDEX IF EXISTS witness.events_message_fts;
DROP INDEX IF EXISTS witness.events_parent_trace_lookup;

ALTER TABLE witness.events
    DROP COLUMN IF EXISTS parent_span_id,
    DROP COLUMN IF EXISTS parent_trace_id;
