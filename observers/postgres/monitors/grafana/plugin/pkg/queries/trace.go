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

// Trace requests the reconstruction of a single distributed trace rooted at
// TraceID (the originating instance's root span_id).
type Trace struct {
	TraceID string `json:"traceID"`
}

// traceCTE walks the trace by alternating between (a) co-occurrence in
// witness.spans (other spans active inside an event that already includes
// a known span_id) and (b) cross_service_edges (a child instance hung off
// an upstream span). Postgres allows only one self-reference in a recursive
// term, so both expansions are unioned into a single `edges` view first.
//
// In the current witness design InstanceContinue adopts the upstream root
// span_id as its own root, so co-occurrence walking is usually enough — the
// cross-service edge is included for robustness against future variants and
// for the (parent_trace_id NOT IN trace_spans) edge case.
const traceCTE = `
WITH RECURSIVE
  edges AS (
      SELECT s1.span_id AS from_id, s2.span_id AS to_id
        FROM witness.spans s1
        JOIN witness.spans s2 ON s1.event_id = s2.event_id
    UNION ALL
      SELECT cse.parent_trace_id, cse.child_root_span_id
        FROM witness.cross_service_edges cse
  ),
  trace_spans AS (
      SELECT $1::uuid AS span_id
    UNION
      SELECT e.to_id
        FROM edges e
        JOIN trace_spans ts ON ts.span_id = e.from_id
  ),
  -- Direct parent: prefer cross-service edge if any, otherwise span_children.
  parents AS (
      SELECT child_root_span_id AS span_id, parent_span_id, 'cross'::text AS kind
        FROM witness.cross_service_edges
       WHERE child_root_span_id IN (SELECT span_id FROM trace_spans)
    UNION ALL
      SELECT child_span_id, parent_span_id, 'co'::text
        FROM witness.span_children
       WHERE child_span_id IN (SELECT span_id FROM trace_spans)
  ),
  parent_picked AS (
      SELECT DISTINCT ON (span_id) span_id, parent_span_id
        FROM parents
       ORDER BY span_id, CASE kind WHEN 'cross' THEN 0 ELSE 1 END
  ),
  -- Records aggregated for each span's start event become OTel-style tags.
  span_tags AS (
      SELECT sp.span_id, erj.records
        FROM witness.span_pairs sp
        LEFT JOIN witness.event_records_json erj
          ON erj.event_id = (SELECT start_event_id FROM witness.span_starts WHERE span_id = sp.span_id)
       WHERE sp.span_id IN (SELECT span_id FROM trace_spans)
  ),
  -- Logs attached to a span = events whose chain contains this span_id and
  -- whose type is in log:* range.
  span_logs AS (
      SELECT s.span_id,
             jsonb_agg(jsonb_build_object(
                 'timestamp', (extract(epoch FROM e.event_date) * 1000)::bigint,
                 'fields',    jsonb_build_array(
                                 jsonb_build_object('key', 'message', 'value', e.event_message),
                                 jsonb_build_object('key', 'caller',  'value', e.event_caller)
                              )
             ) ORDER BY e.event_date) AS logs_json
        FROM witness.events e
        JOIN witness.spans  s ON s.event_id = e.event_id
       WHERE s.span_id IN (SELECT span_id FROM trace_spans)
         AND e.event_type IN (1, 10, 11, 12, 13, 14, 100, 101, 102, 103, 104)
       GROUP BY s.span_id
  )
SELECT sp.span_id,
       pp.parent_span_id,
       sp.span_name,
       sp.service_name,
       sp.started_at,
       sp.duration,
       st.records,
       sl.logs_json
  FROM witness.span_pairs sp
  LEFT JOIN parent_picked pp ON pp.span_id = sp.span_id
  LEFT JOIN span_tags    st  ON st.span_id  = sp.span_id
  LEFT JOIN span_logs    sl  ON sl.span_id  = sp.span_id
 WHERE sp.span_id IN (SELECT span_id FROM trace_spans)
 ORDER BY sp.started_at ASC
`

// tagKV mirrors the Tempo / Grafana TraceView "tags" entries.
type tagKV struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

func RunTrace(ctx context.Context, pool *pgxpool.Pool, t *Trace) backend.DataResponse {
	if t == nil || t.TraceID == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "trace.traceID is required")
	}

	rows, err := pool.Query(ctx, traceCTE, t.TraceID)
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
			logsRaw  []byte
		)
		if err := rows.Scan(&spanID, &parentID, &name, &service, &startAt, &dur, &recsRaw, &logsRaw); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		traceIDs = append(traceIDs, t.TraceID)
		spanIDs = append(spanIDs, spanID)
		parentSpanIDs = append(parentSpanIDs, strOrEmpty(parentID))
		operations = append(operations, strOrEmpty(name))
		services = append(services, strOrEmpty(service))
		startTimes = append(startTimes, float64(startAt.UnixMilli()))
		if dur != nil {
			durations = append(durations, float64(*dur/time.Millisecond))
		} else {
			durations = append(durations, 0)
		}
		tagsJSON = append(tagsJSON, recordsToTags(recsRaw))
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
