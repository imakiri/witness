package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventTrail asks what led to one event: its causal cone, walked backwards.
type EventTrail struct {
	EventID string `json:"eventID"`

	// SinceMinutes bounds how far back the cone is allowed to reach, from
	// the seed event's own date. It is a real cut, not an optimisation: a
	// cause older than the window is not reported, and the events of a
	// long-lived span above the seed (a worker, an instance) are cut with
	// everything else. Zero means the default.
	//
	// Unbounded, the walk reads the whole database — the edge sets and the
	// per-span event lists are global — which is what witness.spans.event_date
	// exists to fix. A trace lives in minutes, so an hour is a generous
	// default and the caller can widen it.
	SinceMinutes int `json:"sinceMinutes,omitempty"`
}

const defaultTrailWindowMinutes = 60

// eventTrailQuery collects everything that causally precedes one event.
//
// It walks the derived cache — witness.span_edges and witness.span_facts,
// maintained per batch by witness.merge_span_cache (see 000_schema.up.sql).
// Deriving the same edges from the raw tables at query time cost 3145 ms on
// half a million events against 2.5 ms here, for the same rows: the walk
// touches about a hundred edges, and rebuilding the edge set to find them was
// the entire cost.
//
// The unit of the walk is a pair (span, cut): a span, and the point on that
// span's timeline up to which everything belongs to the cone. The cut is
// (event_date, event_id), not a bare timestamp — event_date is microseconds
// while time.Now() is nanoseconds, so two events of one span share a
// timestamp often enough to matter, and event_id is uuid v7, which orders
// the tie the way it happened.
//
// Two edges take the walk backwards, and both are read off structure, never
// off timestamps:
//
//   - *parent*: a span was opened by its parent (span_flags & 2), so the
//     parent's events up to the moment this span began are its causes. The
//     cut is the child's start event. A child with no start event is legal —
//     the walk then keeps the child's own cut, which over-collects rather
//     than guesses a beginning the data does not carry.
//   - *link*: a span that took a hand-off (-24) was caused by whoever gave it
//     (2 or 24) up to the giving event. The cut is the *first* giving event
//     on that link, matching link_edges: a retried send is one hand-off, and
//     it is the first attempt that dates it.
//
// Time enters in exactly one place: an inbound hand-off counts only if its
// receiving event lies at or before the current cut — a message this span
// took *after* our event did not cause it. Both sides of that comparison are
// events of the same span, so it never compares clocks across processes,
// which is the comparison the model forbids: a `sent` written after the
// matching `received` cannot mislead it.
//
// Where a span is reached by more than one path, the *earliest* cut wins.
// The cone is what provably preceded the event; a later cut would claim work
// the span did after it handed ours off.
//
// $1 is the event_id, $2 the window in minutes.
const eventTrailQuery = `
WITH RECURSIVE
  seed AS (
      SELECT own.span_id, e.event_date AS cut_date, e.event_id AS cut_id,
             e.event_date - make_interval(mins => $2::int) AS since
        FROM witness.events e
        JOIN witness.spans own ON own.event_id = e.event_id AND own.span_flags & 1 <> 0
       WHERE e.event_id = $1::uuid
  ),
  -- The window, as a scalar every other part bounds itself with. Taken from
  -- the seed event rather than from now(): the cone of an event that
  -- happened yesterday is yesterday's.
  window_start AS (SELECT since FROM seed),
  cone AS (
      SELECT span_id, cut_date, cut_id, 'seed'::text AS relation, 0 AS hop,
             NULL::uuid AS via_span_id,
             NULL::uuid AS edge_from_event_id,
             NULL::uuid AS edge_to_event_id
        FROM seed
    UNION ALL
      SELECT be.from_span_id,
             COALESCE(be.cut_date, c.cut_date),
             COALESCE(be.cut_id,   c.cut_id),
             be.relation, c.hop + 1,
             c.span_id,
             CASE WHEN be.relation = 'link' THEN be.cut_id END,
             COALESCE(be.guard_id, be.cut_id)
        FROM cone c
        JOIN witness.span_edges be ON be.to_span_id = c.span_id
       -- A hand-off cycle between two spans is not expressible in the model,
       -- but the walk must terminate whatever is in the table.
       WHERE c.hop < 32
         -- The window applies to hand-offs, not to parenthood. A hand-off
         -- older than the window did not cause the event we are asking
         -- about; belonging to a parent is not an event and does not age —
         -- the parent's own events are cut by the window further down.
         AND (be.relation <> 'link' OR be.at >= (SELECT since FROM window_start))
         AND (be.guard_date IS NULL
              OR (be.guard_date, be.guard_id) <= (c.cut_date, c.cut_id))
  ),
  cut AS (
      SELECT DISTINCT ON (span_id) span_id, cut_date, cut_id, relation, hop,
             via_span_id, edge_from_event_id, edge_to_event_id
        FROM cone
       ORDER BY span_id, cut_date, cut_id, hop
  )
SELECT e.event_date, e.event_id, e.event_type,
       coalesce(n.event_type_name, e.event_type::text) AS event_type_name,
       e.event_message, e.event_caller,
       cut.span_id, f.span_name, inf.span_name AS service_name,
       cut.relation, cut.hop,
       cut.via_span_id, cut.edge_from_event_id, cut.edge_to_event_id,
       f.instance_span_id,
       rj.records
  FROM cut
  JOIN window_start w ON true
  JOIN witness.spans  s ON s.span_id = cut.span_id AND s.span_flags & 1 <> 0
                       AND s.event_date >= w.since
  JOIN witness.events e ON e.event_id = s.event_id
                       AND (e.event_date, e.event_id) <= (cut.cut_date, cut.cut_id)
  -- The span and its process, from the cache: a primary-key lookup each. The
  -- instance's own span_facts row carries the service name, because an
  -- instance is a span and its span:instance:online event names it.
  LEFT JOIN witness.span_facts       f   ON f.span_id    = cut.span_id
  LEFT JOIN witness.span_facts       inf ON inf.span_id  = f.instance_span_id
  LEFT JOIN witness.event_type_names n   ON n.event_type = e.event_type
  LEFT JOIN LATERAL (
      SELECT coalesce(jsonb_object_agg(r.record_key, r.record_value)
                      FILTER (WHERE r.record_key IS NOT NULL), '{}'::jsonb) AS records
        FROM witness.records r
       WHERE r.event_id = e.event_id
  ) rj ON true
 ORDER BY e.event_date DESC, e.event_id DESC
 LIMIT %d`

// RunEventTrail answers "what led to this event". The frame is a plain
// table: the columns that make a trail readable are spanID, relation and
// hop, which the logs visualisation hides.
func RunEventTrail(ctx context.Context, pool *pgxpool.Pool, r *EventTrail, limit int) backend.DataResponse {
	id := ""
	if r != nil {
		id = r.EventID
	}
	if !isUUID(id) {
		f := emptyTrailFrame()
		msg := "no event selected"
		if id != "" {
			msg = fmt.Sprintf("not an event id: %q", id)
		}
		f.Meta = &data.FrameMeta{Notices: []data.Notice{{Severity: data.NoticeSeverityInfo, Text: msg}}}
		return backend.DataResponse{Frames: data.Frames{f}}
	}

	mins := r.SinceMinutes
	if mins <= 0 {
		mins = defaultTrailWindowMinutes
	}
	rows, err := pool.Query(ctx, fmt.Sprintf(eventTrailQuery, limit), id, mins)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "event trail query: "+err.Error())
	}
	defer rows.Close()

	times := []time.Time{}
	ids := []string{}
	types := []string{}
	messages := []string{}
	callers := []string{}
	spanIDs := []string{}
	spanNames := []string{}
	services := []string{}
	relations := []string{}
	hops := []int64{}
	viaSpans := []string{}
	edgeFroms := []string{}
	edgeTos := []string{}
	instances := []string{}
	records := []json.RawMessage{}

	for rows.Next() {
		var (
			d        time.Time
			id       string
			et       int64
			etName   string
			msg      string
			caller   string
			spanID   string
			spanName *string
			service  *string
			relation string
			hop      int64
			viaSpan  *string
			edgeFrom *string
			edgeTo   *string
			instance *string
			recs     []byte
		)
		if err := rows.Scan(&d, &id, &et, &etName, &msg, &caller,
			&spanID, &spanName, &service, &relation, &hop,
			&viaSpan, &edgeFrom, &edgeTo, &instance, &recs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		times = append(times, d)
		ids = append(ids, id)
		types = append(types, etName)
		messages = append(messages, msg)
		callers = append(callers, caller)
		spanIDs = append(spanIDs, spanID)
		spanNames = append(spanNames, strOrEmpty(spanName))
		services = append(services, strOrEmpty(service))
		relations = append(relations, relation)
		hops = append(hops, hop)
		viaSpans = append(viaSpans, strOrEmpty(viaSpan))
		edgeFroms = append(edgeFroms, strOrEmpty(edgeFrom))
		edgeTos = append(edgeTos, strOrEmpty(edgeTo))
		instances = append(instances, strOrEmpty(instance))
		records = append(records, recs)
	}
	if rows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows: "+rows.Err().Error())
	}

	frame := data.NewFrame("event-trail",
		data.NewField("timestamp", nil, times),
		data.NewField("eventID", nil, ids),
		data.NewField("type", nil, types),
		data.NewField("message", nil, messages),
		data.NewField("caller", nil, callers),
		data.NewField("spanID", nil, spanIDs),
		data.NewField("spanName", nil, spanNames),
		data.NewField("service", nil, services),
		// How this event's span was reached from the seed: it is its span
		// ("seed"), the span above it ("parent"), or the span that handed it
		// the work ("link"). hop is the distance in those steps.
		data.NewField("relation", nil, relations),
		data.NewField("hop", nil, hops),
		// The edge this span was reached by: the span on the other end and
		// the two events the hand-off runs between. Empty on the seed span
		// and, for a parent edge, on viaEventID — a parent emits no event of
		// its own when it opens a child.
		data.NewField("viaSpanID", nil, viaSpans),
		data.NewField("edgeFromEventID", nil, edgeFroms),
		data.NewField("edgeToEventID", nil, edgeTos),
		// The process, by id rather than by name: a view lays lanes out per
		// instance, and two instances of one service share a name.
		data.NewField("instanceSpanID", nil, instances),
		data.NewField("records", nil, records),
	)
	return backend.DataResponse{Frames: data.Frames{frame}}
}

func emptyTrailFrame() *data.Frame {
	return data.NewFrame("event-trail",
		data.NewField("timestamp", nil, []time.Time{}),
		data.NewField("eventID", nil, []string{}),
		data.NewField("type", nil, []string{}),
		data.NewField("message", nil, []string{}),
		data.NewField("caller", nil, []string{}),
		data.NewField("spanID", nil, []string{}),
		data.NewField("spanName", nil, []string{}),
		data.NewField("service", nil, []string{}),
		data.NewField("relation", nil, []string{}),
		data.NewField("hop", nil, []int64{}),
		data.NewField("viaSpanID", nil, []string{}),
		data.NewField("edgeFromEventID", nil, []string{}),
		data.NewField("edgeToEventID", nil, []string{}),
		data.NewField("instanceSpanID", nil, []string{}),
		data.NewField("records", nil, []json.RawMessage{}),
	)
}
