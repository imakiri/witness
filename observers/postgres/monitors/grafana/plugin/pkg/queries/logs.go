package queries

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LogsReq is a tighter, panel-friendly subset of Search aimed at the Logs
// panel — coarse event-type / caller filtering, no record key/value lookup.
type LogsReq struct {
	EventTypes []int64 `json:"eventTypes,omitempty"`
	Service    string  `json:"service,omitempty"`
	// RootSpanID narrows to one trace: the component walked from that root.
	RootSpanID string `json:"rootSpanID,omitempty"`
	// TraceID is the pre-v0.31 name of RootSpanID.
	TraceID string `json:"traceID,omitempty"`
	Caller  string `json:"caller,omitempty"`
	Message string `json:"message,omitempty"`
	// SpanID narrows to events whose *own* span this is — the span itself,
	// not everything nested under it.
	SpanID string `json:"spanID,omitempty"`
}

func (r *LogsReq) root() string {
	if r.RootSpanID != "" {
		return r.RootSpanID
	}
	return r.TraceID
}

// logsTraceFilter restricts to the component walked from one root. The walk
// lives in a CTE, so the query is prefixed rather than joined.
const logsTraceFilter = `e.event_id IN (
    SELECT s.event_id
      FROM trace_spans ts
      JOIN witness.spans s ON s.span_id = ts.span_id AND s.span_flags & 1 <> 0
)`

func RunLogs(ctx context.Context, pool *pgxpool.Pool, r *LogsReq, tr backend.TimeRange, limit int) backend.DataResponse {
	if r == nil {
		r = &LogsReq{}
	}

	clauses := []string{"e.event_date >= $1 AND e.event_date <= $2"}
	// event_date is `timestamp` without a zone; the observer writes UTC.
	args := []any{tr.From.UTC(), tr.To.UTC()}

	if len(r.EventTypes) > 0 {
		args = append(args, r.EventTypes)
		clauses = append(clauses, fmt.Sprintf("e.event_type = ANY($%d::int8[])", len(args)))
	} else {
		// log:* and error:* by default — exclude span lifecycle noise.
		clauses = append(clauses, "(e.event_type BETWEEN 10 AND 14 OR e.event_type BETWEEN 100 AND 104 OR e.event_type = 1)")
	}
	if svc := strings.TrimSpace(r.Service); svc != "" {
		args = append(args, svc)
		clauses = append(clauses, fmt.Sprintf("ei.service_name = $%d", len(args)))
	}
	if sid := strings.TrimSpace(r.SpanID); sid != "" {
		args = append(args, sid)
		clauses = append(clauses, fmt.Sprintf(`e.event_id IN (
        SELECT event_id FROM witness.spans
         WHERE span_id = $%d::uuid AND span_flags & 1 <> 0)`, len(args)))
	}
	if msg := strings.TrimSpace(r.Message); msg != "" {
		args = append(args, "%"+msg+"%")
		clauses = append(clauses, fmt.Sprintf("e.event_message ILIKE $%d", len(args)))
	}
	if c := strings.TrimSpace(r.Caller); c != "" {
		args = append(args, "%"+c+"%")
		clauses = append(clauses, fmt.Sprintf("e.event_caller ILIKE $%d", len(args)))
	}

	// The trace filter needs the recursive walk, whose seed is always $1, so
	// it has to be the first argument. Build that variant separately.
	prefix := ""
	if root := strings.TrimSpace(r.root()); root != "" {
		args = append([]any{[]string{root}}, args...)
		for i := range clauses {
			clauses[i] = shiftPlaceholders(clauses[i])
		}
		clauses = append(clauses, logsTraceFilter)
		prefix = traceWalkCTE
	}

	q := fmt.Sprintf(`%s
SELECT e.event_date, e.event_id, e.event_type, e.event_message, e.event_caller,
       ei.service_name, ei.instance_span_id,
       COALESCE(erj.records, '{}'::jsonb)
  FROM witness.events e
  LEFT JOIN witness.event_records_json erj USING (event_id)
  LEFT JOIN witness.event_instances    ei  ON ei.event_id = e.event_id
 WHERE %s
 ORDER BY e.event_date DESC
 LIMIT %d`, prefix, strings.Join(clauses, " AND "), limit)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "logs query: "+err.Error())
	}
	defer rows.Close()

	times := []time.Time{}
	bodies := []string{}
	severities := []string{}
	ids := []string{}
	callers := []string{}
	serviceNames := []string{}
	instanceIDs := []string{}
	labels := []json.RawMessage{}

	for rows.Next() {
		var (
			d    time.Time
			id   string
			et   int64
			m    string
			c    string
			svc  *string
			inst *string
			rs   []byte
		)
		if err := rows.Scan(&d, &id, &et, &m, &c, &svc, &inst, &rs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		times = append(times, d)
		bodies = append(bodies, m)
		severities = append(severities, severityOf(et))
		ids = append(ids, id)
		callers = append(callers, c)
		serviceNames = append(serviceNames, strOrEmpty(svc))
		instanceIDs = append(instanceIDs, strOrEmpty(inst))
		labels = append(labels, rs)
	}

	frame := data.NewFrame("logs",
		data.NewField("timestamp", nil, times),
		data.NewField("body", nil, bodies),
		data.NewField("severity", nil, severities),
		data.NewField("eventID", nil, ids),
		data.NewField("caller", nil, callers),
		data.NewField("service", nil, serviceNames),
		data.NewField("instanceSpanID", nil, instanceIDs),
		data.NewField("labels", nil, labels),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
	return backend.DataResponse{Frames: data.Frames{frame}}
}
