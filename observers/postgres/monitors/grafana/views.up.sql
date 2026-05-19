-- Witness Grafana view set.
--
-- These views project the normalized witness tables (events, spans, records)
-- into shapes that Grafana panels can query directly. They are idempotent —
-- safe to re-apply.
--
-- Span open/close semantics:
--   Postgres stores the per-event span chain denormalized in witness.spans
--   without order. We recover "open" semantics by treating the earliest event
--   in which a span_id appears with a positive span event_type (20..29) as
--   its opening event, and the latest event in which the same span_id
--   appears with a negative span event_type (-29..-20) as its close.
--   span:link (event_type = 2) is intentionally excluded — it's a cross
--   reference, not a lifecycle event.

-- Earliest "open" event per span_id.
CREATE OR REPLACE VIEW witness.span_starts AS
SELECT DISTINCT ON (s.span_id)
    s.span_id,
    e.event_id      AS start_event_id,
    e.event_date    AS started_at,
    e.event_type    AS start_event_type,
    e.event_message AS span_name,
    e.event_caller  AS start_caller
FROM witness.events e
JOIN witness.spans s ON s.event_id = e.event_id
WHERE e.event_type BETWEEN 20 AND 29
ORDER BY s.span_id, e.event_date ASC;

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
JOIN witness.spans s ON s.event_id = e.event_id
WHERE e.event_type BETWEEN -29 AND -20
ORDER BY s.span_id, e.event_date DESC;

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
    sf.finish_caller
FROM witness.span_starts ss
LEFT JOIN witness.span_finishes sf USING (span_id);

-- Parent/child relation. For each span, its parent is the most recently
-- started other span that was present in its open event. Because every
-- ancestor of a span appears in that span's open event chain, the
-- latest-started co-occurring span is the direct parent.
CREATE OR REPLACE VIEW witness.span_children AS
SELECT DISTINCT ON (child.span_id)
    parent.span_id   AS parent_span_id,
    parent.span_name AS parent_name,
    child.span_id    AS child_span_id,
    child.span_name  AS child_name,
    child.started_at AS child_started_at
FROM witness.span_starts child
JOIN witness.spans sibling
    ON sibling.event_id = child.start_event_id
   AND sibling.span_id <> child.span_id
JOIN witness.span_starts parent
    ON parent.span_id = sibling.span_id
ORDER BY child.span_id, parent.started_at DESC;

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
    (25,   'span:message_external:sent'),
    (-25,  'span:external_message:received'),
    (30,   'metric:gauge'),
    (31,   'metric:counter'),
    (32,   'metric:histogram'),
    (100,  'log:error:internal'),
    (101,  'log:error:external'),
    (102,  'log:error:device'),
    (103,  'log:error:storage'),
    (104,  'log:error:network');
