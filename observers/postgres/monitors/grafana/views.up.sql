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

-- Nothing here may assume that events come in pairs. A span's start and
-- finish are two independent events sharing a span_id: either can be absent,
-- both can be, and a span can carry events with neither. Every view that
-- reduced a span to its start event has had to be rewritten once already —
-- span_pairs, span_children and span_instances all take the span universe
-- from witness.spans and treat lifecycle events as optional decoration.

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

-- span_id -> the instance that owns it, from *any* event the span emitted.
--
-- Not from its start event: a span need not have one. Every event carries the
-- instance (span_flags & 8), so one event is enough to place a span in a
-- process, and DISTINCT ON keeps a span to one row whatever it emitted.
CREATE OR REPLACE VIEW witness.span_instances AS
SELECT DISTINCT ON (own.span_id)
    own.span_id,
    ei.instance_span_id,
    ei.service_name
FROM witness.spans own
LEFT JOIN witness.event_instances ei ON ei.event_id = own.event_id
WHERE own.span_flags & 1 <> 0
ORDER BY own.span_id, (ei.instance_span_id IS NULL), ei.instance_span_id;

-- Earliest "open" event per span_id, matched on the span's *own* role so an
-- ancestor listed in the same event is not mistaken for a span starting.
--
-- Built-in span *lifecycle* types are 20..21 / -21..-20 (a span and an
-- instance; there is one kind of span, so 22 and 23 are retired); 24 / -24 are the
-- message hand-off, which happens **inside** a span and must never be read
-- as opening or closing one — a span whose only
-- positive event is a `sent` would otherwise be reported as starting there,
-- and a `received` arriving after a finish would be reported as closing it.
-- Custom span types registered with witness.MustNewEventType use |i| >= 1000
-- with the same sign convention.
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
WHERE e.event_type BETWEEN 20 AND 21
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
WHERE e.event_type BETWEEN -21 AND -20
   OR e.event_type <= -1000
ORDER BY s.span_id, e.event_date DESC, e.event_id DESC;

-- Span lifecycle: start and finish where they exist, duration where both do.
--
-- **Both halves are optional, and so is either of them.** Events are not tied
-- to each other — a start whose finish never arrived (the process died, the
-- span is still open), a finish whose start was never observed (ingestion
-- started mid-flight, the writer was down, the batch with it was dropped) and
-- a span with neither but with events of its own are all legal, and all three
-- appear here. The universe is therefore every span that owns an event, not
-- the set of spans that started; started_at, finished_at and duration are
-- NULL as the data allows. A finish-only span takes its name from the finish
-- event, which is the only name it has.
CREATE OR REPLACE VIEW witness.span_pairs AS
SELECT
    si.span_id,
    COALESCE(ss.span_name, sf.finish_message) AS span_name,
    ss.start_event_type,
    ss.started_at,
    sf.finished_at,
    (sf.finished_at - ss.started_at) AS duration,
    ss.start_caller,
    sf.finish_caller,
    si.instance_span_id,
    si.service_name
FROM witness.span_instances si
LEFT JOIN witness.span_starts   ss USING (span_id)
LEFT JOIN witness.span_finishes sf USING (span_id);

-- Parent/child relation, stated rather than guessed: an event names its
-- parent with span_flags & 2, alongside its own span with & 1.
--
-- Read off *every* event, not off the child's start event: a span need not
-- have one, and every event it emits carries the same chain, so any one of
-- them states the parent. DISTINCT collapses the repetition.
--
-- child_at is the earliest event of the child that states this parenthood.
-- Unlike child_started_at it is never NULL — every event carries the chain,
-- and a span need not have a start — so it is the column a walk bounds
-- itself in time with. It reads witness.spans.event_date directly: joining
-- events for the date would defeat the point.
CREATE OR REPLACE VIEW witness.span_children AS
SELECT
    p.span_id        AS parent_span_id,
    ps.span_name     AS parent_name,
    c.span_id        AS child_span_id,
    cs.span_name     AS child_name,
    cs.started_at    AS child_started_at,
    ci.service_name,
    min(c.event_date) AS child_at
FROM witness.spans c
JOIN witness.spans p ON p.event_id = c.event_id AND p.span_flags & 2 <> 0
LEFT JOIN witness.span_starts    ps ON ps.span_id = p.span_id
LEFT JOIN witness.span_starts    cs ON cs.span_id = c.span_id
LEFT JOIN witness.span_instances ci ON ci.span_id = c.span_id
WHERE c.span_flags & 1 <> 0
GROUP BY 1, 2, 3, 4, 5, 6;

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

-- One row per (span, link): which halves of a hand-off that span emitted on
-- that link, and when each first happened.
--
-- Which side of the hand-off a span is on is read off the **event type**, not
-- off the timestamps: giving (span:link = 2 before the send, message:sent =
-- 24 after it) vs taking (message:received = -24). Event order is not
-- guaranteed — a sender that emits its `sent` only once the broker acked can
-- be written after the receiver's `received`, and events are independent
-- anyway — so a rule of the form "whoever sent first is the caller" is a
-- guess that fails exactly on the shapes it is needed for.
--
-- A hand-off is one-directional and has no reply half. An answer travelling
-- back is another hand-off with its own id, and for a synchronous call it is
-- no event at all — the round trip is the calling span's own duration.
--
-- The dates are still useful (they are what link_edges reports as the wait)
-- and they are the earliest of each kind, so a retried send is dated from the
-- first attempt.
CREATE OR REPLACE VIEW witness.span_link_sides AS
SELECT
    sl.span_id,
    sl.link_span_id,
    min(sl.event_date) FILTER (WHERE sl.event_type IN (2, 24)) AS sent_at,
    min(sl.event_date) FILTER (WHERE sl.event_type = -24)      AS received_at
FROM witness.span_links sl
GROUP BY sl.span_id, sl.link_span_id;

-- Link edges, directed: the sender's side emits a positive event type
-- (span:link = 2, message:sent = 24), the receiver's a negative one (-24),
-- and the shared link_span_id joins them.
--
-- span:link counting as a *send* is deliberate: witness.Link emitted before a
-- hand-off is how a sender says "about to hand this id off", and it keeps the
-- edge to the receiver even when the process dies before it can emit Sent.
-- The receiving side must therefore use Received, never Link.
--
-- Built on span_link_sides, so a link carries **one edge per pair of spans**
-- however many events each side emitted. A send that had to be retried on the
-- same msgID — the broker never acked, the caller sent again — would
-- otherwise produce one edge row per attempt and inflate every count taken
-- over this view. from_at/to_at are the first send and the first receive,
-- which is the whole wait, not the last attempt's.
--
-- Deliberately NOT restricted to two different instances. A hand-off inside
-- one process is an ordinary edge: a request span enqueues work and a worker
-- picks it up, which is the same relation as a cross-process hop and must be
-- walkable the same way. Direction comes from the event_type sign alone; the
-- old `dst.instance_span_id IS DISTINCT FROM src.instance_span_id` predicate
-- added no direction and silently disconnected every in-process queue.
-- A span linking to itself is excluded, being a self-loop rather than an edge.
--
-- A hand-off has one direction: giver (24, or a bare span:link = 2) ->
-- taker (-24). Both halves of an answer travelling back belong to a second
-- link with its own id, so a request/response never draws a cycle here.
--
-- from_at/to_at are the first send and the first receive, but they are not a
-- guaranteed interval: events are independent, and a `sent` emitted only
-- after the broker acked can be written after the receiver's `received`, so
-- to_at < from_at is legal and means "we cannot say", not "negative wait".
-- Nothing here filters on their order — the edge exists because the two
-- halves exist.
--
-- A link nobody received yields no row at all: a message that was sent and
-- never arrived is exactly a send with no receiving side, and the walk stops
-- there because nothing on the far end ever happened.
CREATE OR REPLACE VIEW witness.link_edges AS
SELECT
    src.link_span_id,
    src.span_id           AS from_span_id,
    ss.instance_span_id   AS from_instance_span_id,
    ss.service_name       AS from_service_name,
    src.sent_at           AS from_at,
    dst.span_id           AS to_span_id,
    ds.instance_span_id   AS to_instance_span_id,
    ds.service_name       AS to_service_name,
    dst.received_at       AS to_at
FROM witness.span_link_sides src
JOIN witness.span_link_sides dst
  ON dst.link_span_id = src.link_span_id
 AND dst.span_id IS DISTINCT FROM src.span_id
LEFT JOIN witness.span_instances ss ON ss.span_id = src.span_id
LEFT JOIN witness.span_instances ds ON ds.span_id = dst.span_id
WHERE src.sent_at     IS NOT NULL
  AND dst.received_at IS NOT NULL;

-- Trace roots: the entry point of a distributed request.
--
-- A trace has no id of its own — it is a connected component of the
-- event<->span graph — so the UI needs a stable handle to walk from. A root
-- is a span opened directly under an instance (its parent is the process
-- root) that no one else *triggered*.
--
-- "Triggered" is an inbound message (`span:message:received`, -24): someone
-- handed this span work. A span that only ever gave work away (span:link,
-- message:sent) is a root, and so is one that never touched a link at all.
--
-- A synchronous call therefore costs its caller nothing here: the round trip
-- is the calling span's duration, no event comes back, and the caller stays
-- the root of its trace. Emitting a `received` on the reply would say the
-- caller was triggered by its own callee — which is why the reply half was
-- dropped from the model rather than given its own event types.
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
    SELECT 1 FROM witness.span_link_sides s
     WHERE s.span_id = sp.span_id
       AND s.received_at IS NOT NULL
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

-- Event type name lookup, over the table the observer upserts at start-up.
-- Kept as a view under its old name so existing queries and dashboards do
-- not care that the names stopped being a hardcoded list in this file: they
-- come from core.Events() now, custom types included.
CREATE OR REPLACE VIEW witness.event_type_names AS
SELECT event_type, event_type_name FROM witness.event_types;
