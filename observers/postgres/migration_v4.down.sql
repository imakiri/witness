-- Reverse of migration_v4.up.sql.

DROP INDEX IF EXISTS witness.events_service_lookup;

ALTER TABLE witness.events
    DROP COLUMN IF EXISTS service_name;
