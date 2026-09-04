-- Witness Grafana view set.
--
-- These views project the normalized witness tables (events, spans, records)
-- into shapes that Grafana panels can query directly. They are idempotent —
-- safe to re-apply.
--
-- Everything here reads witness.spans.span_flags, the per-event role of each
-- span_id:
--
--   1  own       the event happened directly in this span (exactly one)
--   2  parent    direct parent of the own span
--   4  ancestor  an enclosing scope above the parent
--   8  instance  root span of the process that emitted the event
--   16 link      a span the event references without being inside it
--
-- Rows written before span_flags existed carry 0 and are invisible to these
-- views: the roles are derived from a chain order that was never stored, so
-- there is nothing to backfill them from.
--
-- Two things the flags replaced:
--
--   * The old parent/child heuristic ("the latest-started span co-occurring
--     in the child's open event") is gone — the parent is now stated
--     outright by span_flags & 2 on the child's own start event.
--   * cross_service_edges is gone. A cross-process hop is a link row in
--     witness.spans like any other, so plain co-occurrence walks through it;
--     see witness.span_links.

-- Instances: one row per process, from its span:instance:online event.
-- Replaces the dropped events.service_name column — the service *is* this
-- span, and its online event carries the name.
CREATE OR REPLACE VIEW witness.instances AS
SELECT s.span_id        AS instance_span_id,
       e.event_message  AS service_name,
       e.event_date     AS online_at,
       e.event_caller   AS online_caller
FROM witness.events e
JOIN witness.spans  s ON s.event_id = e.event_id AND s.span_flags & 8 <> 0
WHERE e.event_type = 21; -- span:instance:online

-- event_id -> the instance that emitted it. Every event carries exactly one
-- span flagged `instance`, so this is a 1:1 join, served by
-- spans_instance_lookup.
CREATE OR REPLACE VIEW witness.event_instances AS
SELECT e.event_id,
       s.span_id AS instance_span_id,
       i.service_name
FROM witness.events e
JOIN witness.spans     s ON s.event_id = e.event_id AND s.span_flags & 8 <> 0
LEFT JOIN witness.instances i ON i.instance_span_id = s.span_id;

-- Earliest "open" event per span_id, matched on the span's *own* role so an
-- ancestor listed in the same event is not mistaken for a span starting.
-- Built-in span types live in 20..29 / -29..-20; custom ones registered with
-- witness.MustNewEventType use |i| >= 1000 with the same sign convention.
CREATE OR REPLACE VIEW witness.span_starts AS
SELECT DISTINCT ON (s.span_id)
    s.span_id,
    e.event_id      AS start_event_id,
    e.event_date    AS started_at,
    e.event_type    AS start_event_type,
    e.event_message AS span_name,
    e.event_caller  AS start_caller,
    ei.instance_span_id,
    ei.service_name
FROM witness.events e
JOIN witness.spans s ON s.event_id = e.event_id AND s.span_flags & 1 <> 0
LEFT JOIN witness.event_instances ei ON ei.event_id = e.event_id
WHERE e.event_type BETWEEN 20 AND 29
   OR e.event_type >= 1000
ORDER BY s.span_id, e.event_date ASC, e.event_id ASC;

-- Latest "close" event per span_id.
CREATE OR REPLACE VIEW witness.span_finishes AS
SELECT DISTINCT ON (s.span_id)
    s.span_id,
    e.event_id      AS finish_event_id,
    e.event_date    AS finished_at,
    e.event_type    AS finish_event_type,
    e.event_message AS finish_message,
    e.event_caller  AS finish_caller
FROM witness.events e
JOIN witness.spans s ON s.event_id = e.event_id AND s.span_flags & 1 <> 0
WHERE e.event_type BETWEEN -29 AND -20
   OR e.event_type <= -1000
ORDER BY s.span_id, e.event_date DESC, e.event_id DESC;

-- Span lifecycle pair with duration. Spans whose finish event has not yet
-- arrived appear with finished_at = NULL and duration = NULL.
CREATE OR REPLACE VIEW witness.span_pairs AS
SELECT
    ss.span_id,
    ss.span_name,
    ss.start_event_type,
    ss.started_at,
    sf.finished_at,
    (sf.finished_at - ss.started_at) AS duration,
    ss.start_caller,
    sf.finish_caller,
    ss.instance_span_id,
    ss.service_name
FROM witness.span_starts ss
LEFT JOIN witness.span_finishes sf USING (span_id);

-- Parent/child relation, stated rather than guessed: a span's start event
-- names its parent with span_flags & 2.
CREATE OR REPLACE VIEW witness.span_children AS
SELECT
    p.span_id        AS parent_span_id,
    ps.span_name     AS parent_name,
    child.span_id    AS child_span_id,
    child.span_name  AS child_name,
    child.started_at AS child_started_at,
    child.service_name
FROM witness.span_starts child
JOIN witness.spans p
    ON p.event_id = child.start_event_id AND p.span_flags & 2 <> 0
LEFT JOIN witness.span_starts ps ON ps.span_id = p.span_id;

-- Links: an event referencing a span it is not inside. Both halves of a
-- hand-off produce one of these against the same link_span_id, which is what
-- reconnects two processes.
CREATE OR REPLACE VIEW witness.span_links AS
SELECT
    e.event_id,
    e.event_type,
    e.event_date,
    e.event_message,
    own.span_id      AS span_id,
    l.span_id        AS link_span_id,
    ei.instance_span_id,
    ei.service_name
FROM witness.events e
JOIN witness.spans own ON own.event_id = e.event_id AND own.span_flags & 1 <> 0
JOIN witness.spans l   ON l.event_id   = e.event_id AND l.span_flags & 16 <> 0
LEFT JOIN witness.event_instances ei ON ei.event_id = e.event_id;

-- Cross-process edges: one link_span_id referenced by two different
-- instances. The sender's side emits a positive event type (span:link = 2,
-- *_message:sent = 24/25), the receiver's a negative one (-24/-25).
CREATE OR REPLACE VIEW witness.link_edges AS
SELECT
    src.link_span_id,
    src.span_id           AS from_span_id,
    src.instance_span_id  AS from_instance_span_id,
    src.service_name      AS from_service_name,
    src.event_date        AS from_at,
    dst.span_id           AS to_span_id,
    dst.instance_span_id  AS to_instance_span_id,
    dst.service_name      AS to_service_name,
    dst.event_date        AS to_at
FROM witness.span_links src
JOIN witness.span_links dst
  ON dst.link_span_id = src.link_span_id
 AND dst.instance_span_id IS DISTINCT FROM src.instance_span_id
 AND src.event_type >= 0
 AND dst.event_type <  0;

-- Trace roots: the entry point of a distributed request.
--
-- A trace has no id of its own — it is a connected component of the
-- event<->span graph — so the UI needs a stable handle to walk from. A root
-- is a span opened directly under an instance (its parent is the process
-- root) that was *not* triggered by someone else, i.e. carries no inbound
-- link event. That makes exactly one root per distributed request: the
-- receiving side of a hop is excluded because its span references an
-- inbound link.
CREATE OR REPLACE VIEW witness.trace_roots AS
SELECT sp.span_id AS root_span_id,
       sp.span_name,
       sp.started_at,
       sp.finished_at,
       sp.duration,
       sp.instance_span_id,
       sp.service_name
FROM witness.span_pairs sp
JOIN witness.span_children sc ON sc.child_span_id = sp.span_id
JOIN witness.instances    ins ON ins.instance_span_id = sc.parent_span_id
WHERE NOT EXISTS (
    SELECT 1 FROM witness.span_links sl
     WHERE sl.span_id = sp.span_id
       AND sl.event_type < 0     -- *_message:received
);

-- Records aggregated to a single JSONB column per event.
CREATE OR REPLACE VIEW witness.event_records_json AS
SELECT e.event_id,
       COALESCE(
           jsonb_object_agg(r.record_key, r.record_value)
               FILTER (WHERE r.record_key IS NOT NULL),
           '{}'::jsonb
       ) AS records
FROM witness.events e
LEFT JOIN witness.records r ON r.event_id = e.event_id
GROUP BY e.event_id;

-- Event type name lookup. Mirrors witness/events.go; extend if you add custom
-- event types via witness.MustNewEventType.
CREATE OR REPLACE VIEW witness.event_type_names (event_type, event_type_name) AS
VALUES
    (1,    'log'),
    (2,    'span:link'),
    (3,    'metric'),
    (10,   'log:debug'),
    (11,   'log:info'),
    (12,   'log:warn'),
    (13,   'log:error'),
    (14,   'log:fatal'),
    (20,   'span:general:start'),
    (-20,  'span:general:finish'),
    (21,   'span:instance:online'),
    (-21,  'span:instance:offline'),
    (22,   'span:service:start'),
    (-22,  'span:service:finish'),
    (23,   'span:wait_group:start'),
    (-23,  'span:wait_group:finish'),
    (24,   'span:internal_message:sent'),
    (-24,  'span:internal_message:received'),
    (25,   'span:external_message:sent'),
    (-25,  'span:external_message:received'),
    (30,   'metric:gauge'),
    (31,   'metric:counter'),
    (32,   'metric:histogram'),
    (100,  'log:error:internal'),
    (101,  'log:error:external'),
    (102,  'log:error:device'),
    (103,  'log:error:storage'),
    (104,  'log:error:network');
