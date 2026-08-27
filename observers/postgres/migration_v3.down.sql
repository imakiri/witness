-- Reverse of migration_v3.up.sql.

DROP INDEX IF EXISTS witness.events_trace_id_idx;

ALTER TABLE witness.events
    DROP COLUMN IF EXISTS trace_id;
