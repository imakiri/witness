-- Witness Postgres schema v4.
--
-- Apply after migration.up.sql (v1), migration_v2.up.sql (v2) and
-- migration_v3.up.sql (v3). Adds `service_name` to events — the local
-- instance's name (the string passed as instanceName to witness.Instance /
-- witness.InstanceContinue). Every event emitted by one instance carries
-- the same service_name, so dashboards can filter and group by service
-- without reconstructing the span chain at query time.
--
-- We do NOT backfill historical rows: events written before this migration
-- will have service_name IS NULL. Dashboards that filter by service should
-- treat NULL as "unknown".

ALTER TABLE witness.events
    ADD COLUMN IF NOT EXISTS service_name varchar(127) NULL;

-- Per-service lookup (recent events of service X, optionally scoped to a
-- trace_id via the existing events_trace_id_idx).
CREATE INDEX IF NOT EXISTS events_service_lookup
    ON witness.events (service_name, event_date DESC)
    WHERE service_name IS NOT NULL;
