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

// Trace requests the reconstruction of a single distributed trace. RootSpanID
// is the entry-point span listed by the L1 traces panel; the trace is its
// connected component, walked at query time — witness has no trace_id.
type Trace struct {
	RootSpanID string `json:"rootSpanID"`

	// TraceID is the pre-v0.31 name of the same field, kept so dashboards
	// and saved links that predate the rename keep working.
	TraceID string `json:"traceID,omitempty"`
}

func (t *Trace) root() string {
	if t == nil {
		return ""
	}
	if t.RootSpanID != "" {
		return t.RootSpanID
	}
	return t.TraceID
}

// traceQuery reconstructs the span tree of one trace.
//
// The parent of each span is stated outright by span_flags & 2 on its start
// event (witness.span_children); a span reached across a process boundary is
// re-parented onto the sending span, so the OTel-style tree stays connected
// where witness records a link.
const traceQuery = traceWalkCTE + `,
  parents AS (
      SELECT to_span_id AS span_id, from_span_id AS parent_span_id, 0 AS pref
        FROM witness.link_edges
       WHERE to_span_id IN (SELECT span_id FROM trace_spans)
    UNION ALL
      SELECT child_span_id, parent_span_id, 1
        FROM witness.span_children
       WHERE child_span_id IN (SELECT span_id FROM trace_spans)
  ),
  parent_picked AS (
      SELECT DISTINCT ON (span_id) span_id, parent_span_id
        FROM parents
       ORDER BY span_id, pref
  ),
  -- The two events that *are* the bar. Kept apart from span_tag_events
  -- because span_logs excludes exactly these from the marks, and nothing
  -- else.
  span_lifecycle_events AS (
      SELECT span_id, start_event_id AS event_id FROM witness.span_starts
       WHERE span_id IN (SELECT span_id FROM trace_spans)
    UNION ALL
      SELECT span_id, finish_event_id FROM witness.span_finishes
       WHERE span_id IN (SELECT span_id FROM trace_spans)
  ),
  -- Span attributes: the records of *both* lifecycle events, plus those of
  -- the span's own span:message:received (-24). A span is independent
  -- events, not one object with a payload: what a caller attaches on the way
  -- out (a result, a row count) is as much an attribute as what it attached
  -- on the way in, and the records a handler passes to witness.Handle /
  -- HandleAll — the order id, the batch size — land on the received event,
  -- which is the only place they exist. Reading the start alone showed an
  -- empty attributes panel for every span opened by Handle.
  --
  -- Only the *receiving* half: a sent (24) or a link (2) is about a message
  -- this span handed to someone else, and its records describe that message,
  -- not this span. They stay marks on the bar.
  span_tag_events AS (
      SELECT span_id, event_id FROM span_lifecycle_events
    UNION ALL
      SELECT s.span_id, e.event_id
        FROM witness.events e
        JOIN witness.spans s ON s.event_id = e.event_id AND s.span_flags & 1 <> 0
       WHERE s.span_id IN (SELECT span_id FROM trace_spans)
         AND e.event_type = -24
  ),
  span_tags AS (
      SELECT ste.span_id,
             coalesce(jsonb_object_agg(r.record_key, r.record_value)
                      FILTER (WHERE r.record_key IS NOT NULL), '{}'::jsonb) AS records
        FROM span_tag_events ste
        LEFT JOIN witness.records r ON r.event_id = ste.event_id
       GROUP BY ste.span_id
  ),
  -- What Grafana labels "Resource attributes": the process the span belongs
  -- to. A witness instance *is* the resource — its span:instance:online and
  -- :offline events carry the name, the version and whatever else the process
  -- announced itself with.
  instance_tags AS (
      SELECT si.span_id,
             jsonb_build_object(
                 'instance.name',    coalesce(i.service_name, ''),
                 'instance.span_id', si.instance_span_id::text,
                 'instance.online',  to_char(i.online_at, 'YYYY-MM-DD HH24:MI:SS.MS')
             ) || coalesce(ir.records, '{}'::jsonb) AS records
        FROM witness.span_instances si
        LEFT JOIN witness.instances i ON i.instance_span_id = si.instance_span_id
        LEFT JOIN LATERAL (
            SELECT coalesce(jsonb_object_agg(r.record_key, r.record_value)
                            FILTER (WHERE r.record_key IS NOT NULL), '{}'::jsonb) AS records
              FROM witness.spans own
              JOIN witness.records r ON r.event_id = own.event_id
             WHERE own.span_id = si.instance_span_id AND own.span_flags & 1 <> 0
        ) ir ON true
       WHERE si.span_id IN (SELECT span_id FROM trace_spans)
  ),
  -- The marks on a span's bar: every event whose *own* span it is, except
  -- the two that are the bar itself. Not just log:* — a hand-off, a metric
  -- and a custom event are moments in the span too, and the event type is
  -- carried as a field so the tooltip says which.
  --
  -- Ancestors are excluded: span_flags tells "in this span" from "under it".
  -- The timestamp is milliseconds with a fraction, deliberately: rounded to
  -- whole milliseconds it drifts up to 1ms away from a bar drawn from
  -- fractional values, which on a 6ms span reads as an event outside its own
  -- span.
  span_logs AS (
      SELECT s.span_id,
             jsonb_agg(jsonb_build_object(
                 'timestamp', extract(epoch FROM e.event_date) * 1000,
                 'fields',    jsonb_build_array(
                                 jsonb_build_object('key', 'message', 'value', e.event_message),
                                 jsonb_build_object('key', 'type',    'value', coalesce(n.event_type_name, e.event_type::text)),
                                 jsonb_build_object('key', 'caller',  'value', e.event_caller)
                              )
             ) ORDER BY e.event_date) AS logs_json
        FROM witness.events e
        JOIN witness.spans  s ON s.event_id = e.event_id AND s.span_flags & 1 <> 0
        LEFT JOIN witness.event_type_names n ON n.event_type = e.event_type
        LEFT JOIN span_lifecycle_events sle ON sle.event_id = e.event_id
       WHERE s.span_id IN (SELECT span_id FROM trace_spans)
         AND sle.event_id IS NULL
       GROUP BY s.span_id
  )
SELECT sp.span_id,
       pp.parent_span_id,
       sp.span_name,
       sp.service_name,
       sp.started_at,
       sp.duration,
       st.records,
       it.records AS instance_records,
       sl.logs_json
  FROM witness.span_pairs sp
  LEFT JOIN parent_picked pp ON pp.span_id = sp.span_id
  LEFT JOIN span_tags     st ON st.span_id = sp.span_id
  LEFT JOIN instance_tags it ON it.span_id = sp.span_id
  LEFT JOIN span_logs     sl ON sl.span_id = sp.span_id
 WHERE sp.span_id IN (SELECT span_id FROM trace_spans)
 ORDER BY sp.started_at ASC
`

// isUUID reports whether s has the shape of a span id. It is a shape test,
// not a parse: the query casts to uuid itself, and this only has to keep a
// dashboard variable that was never filled in from reaching it.
func isUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// emptyTraceFrame is the trace frame with no rows: same fields, same
// preferred visualisation, nothing in it.
func emptyTraceFrame() *data.Frame {
	frame := data.NewFrame("trace",
		data.NewField("traceID", nil, []string{}),
		data.NewField("spanID", nil, []string{}),
		data.NewField("parentSpanID", nil, []string{}),
		data.NewField("operationName", nil, []string{}),
		data.NewField("serviceName", nil, []string{}),
		data.NewField("startTime", nil, []float64{}),
		data.NewField("duration", nil, []float64{}),
		data.NewField("tags", nil, []json.RawMessage{}),
		data.NewField("serviceTags", nil, []json.RawMessage{}),
		data.NewField("logs", nil, []json.RawMessage{}),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTrace}
	return frame
}

// tagKV mirrors the Tempo / Grafana TraceView "tags" entries.
type tagKV struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func RunTrace(ctx context.Context, pool *pgxpool.Pool, t *Trace) backend.DataResponse {
	root := t.root()
	if !isUUID(root) {
		// Nothing to draw: either no trace is selected yet, or the dashboard
		// variable did not get interpolated and the literal "$root" arrived.
		// Both are answered with an empty frame carrying a notice — a panel
		// that is blank and says why beats one that is red, and the string
		// that arrived is exactly what tells the two cases apart.
		f := emptyTraceFrame()
		msg := "no trace selected"
		if root != "" {
			msg = fmt.Sprintf("not a span id: %q", root)
		}
		f.Meta.Notices = []data.Notice{{Severity: data.NoticeSeverityInfo, Text: msg}}
		return backend.DataResponse{Frames: data.Frames{f}}
	}

	rows, err := pool.Query(ctx, traceQuery, []string{root})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "trace query: "+err.Error())
	}
	defer rows.Close()

	traceIDs := []string{}
	spanIDs := []string{}
	parentSpanIDs := []string{}
	operations := []string{}
	services := []string{}
	startTimes := []float64{}
	durations := []float64{}
	tagsJSON := []json.RawMessage{}
	serviceTagsJSON := []json.RawMessage{}
	logsJSON := []json.RawMessage{}

	for rows.Next() {
		var (
			spanID   string
			parentID *string
			name     *string
			service  *string
			startAt  time.Time
			dur      *time.Duration
			recsRaw  []byte
			instRaw  []byte
			logsRaw  []byte
		)
		if err := rows.Scan(&spanID, &parentID, &name, &service, &startAt, &dur, &recsRaw, &instRaw, &logsRaw); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		traceIDs = append(traceIDs, root)
		spanIDs = append(spanIDs, spanID)
		parentSpanIDs = append(parentSpanIDs, strOrEmpty(parentID))
		operations = append(operations, strOrEmpty(name))
		services = append(services, strOrEmpty(service))
		// Milliseconds with a fraction. Rounding either of these to whole
		// milliseconds shifts a bar against the marks drawn on it, which on a
		// span of a few milliseconds reads as an event outside its own span.
		startTimes = append(startTimes, float64(startAt.UnixNano())/float64(time.Millisecond))
		if dur != nil {
			durations = append(durations, float64(*dur)/float64(time.Millisecond))
		} else {
			durations = append(durations, 0)
		}
		tagsJSON = append(tagsJSON, recordsToTags(recsRaw))
		serviceTagsJSON = append(serviceTagsJSON, recordsToTags(instRaw))
		if len(logsRaw) == 0 {
			logsJSON = append(logsJSON, json.RawMessage("[]"))
		} else {
			logsJSON = append(logsJSON, logsRaw)
		}
	}
	if rows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows: "+rows.Err().Error())
	}

	frame := data.NewFrame("trace",
		data.NewField("traceID", nil, traceIDs),
		data.NewField("spanID", nil, spanIDs),
		data.NewField("parentSpanID", nil, parentSpanIDs),
		data.NewField("operationName", nil, operations),
		data.NewField("serviceName", nil, services),
		data.NewField("startTime", nil, startTimes),
		data.NewField("duration", nil, durations),
		data.NewField("tags", nil, tagsJSON),
		// Grafana labels this one "Resource attributes"; the label is its own,
		// not ours. What goes in it is the process the span belongs to — the
		// witness instance.
		data.NewField("serviceTags", nil, serviceTagsJSON),
		data.NewField("logs", nil, logsJSON),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTrace}

	return backend.DataResponse{Frames: data.Frames{frame}}
}

// recordsToTags converts the jsonb_object_agg blob into a Tempo-style array
// of {key,value} pairs.
func recordsToTags(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("[]")
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return json.RawMessage("[]")
	}
	tags := make([]tagKV, 0, len(m))
	for k, v := range m {
		tags = append(tags, tagKV{Key: k, Value: v})
	}
	out, err := json.Marshal(tags)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

// hide unused-import warning in stripped builds
var _ = fmt.Sprintf
