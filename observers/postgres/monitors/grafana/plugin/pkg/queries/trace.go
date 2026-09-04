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
  -- Records on a span's start event become OTel-style tags.
  span_tags AS (
      SELECT ss.span_id, erj.records
        FROM witness.span_starts ss
        JOIN witness.event_records_json erj ON erj.event_id = ss.start_event_id
       WHERE ss.span_id IN (SELECT span_id FROM trace_spans)
  ),
  -- Logs of a span = log events whose *own* span it is. Ancestors are
  -- excluded: span_flags tells "in this span" from "under it".
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
        JOIN witness.spans  s ON s.event_id = e.event_id AND s.span_flags & 1 <> 0
       WHERE s.span_id IN (SELECT span_id FROM trace_spans)
         AND e.event_type IN (` + logEventTypes + `)
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
	root := t.root()
	if root == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "trace.rootSpanID is required")
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
		traceIDs = append(traceIDs, root)
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
