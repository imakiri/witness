-- Reverse of views.up.sql. Drop order matters: dependants first.
DROP VIEW IF EXISTS witness.event_type_names;
DROP VIEW IF EXISTS witness.event_records_json;
DROP VIEW IF EXISTS witness.trace_roots;
DROP VIEW IF EXISTS witness.link_edges;
DROP VIEW IF EXISTS witness.span_links;
DROP VIEW IF EXISTS witness.span_children;
DROP VIEW IF EXISTS witness.span_pairs;
DROP VIEW IF EXISTS witness.span_finishes;
DROP VIEW IF EXISTS witness.span_starts;
DROP VIEW IF EXISTS witness.event_instances;
DROP VIEW IF EXISTS witness.instances;

-- Views removed in v0.31, dropped here so a re-apply over an older install
-- does not leave them behind referencing columns that no longer exist.
DROP VIEW IF EXISTS witness.trace_services;
DROP VIEW IF EXISTS witness.cross_service_edges;
