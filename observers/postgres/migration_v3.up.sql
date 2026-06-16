-- Witness Postgres schema v3.
--
-- Apply after migration.up.sql (v1) and migration_v2.up.sql (v2). Adds
-- `trace_id` to events — the logical request identifier set by
-- witness.Instance (= the originating service's root span_id) and adopted
-- unchanged by witness.InstanceContinue on receivers. Every event produced
-- while handling one request shares the same trace_id across services, so
-- filtering or grouping by trace_id in any panel just works.
--
-- Without this column you have to reconstruct request membership at query
-- time by walking witness.spans / witness.cross_service_edges. With it,
-- everything reduces to `WHERE trace_id = $1`.

ALTER TABLE witness.events
    ADD COLUMN IF NOT EXISTS trace_id uuid NULL;

CREATE INDEX IF NOT EXISTS events_trace_id_idx
    ON witness.events (trace_id, event_date DESC)
    WHERE trace_id IS NOT NULL;
