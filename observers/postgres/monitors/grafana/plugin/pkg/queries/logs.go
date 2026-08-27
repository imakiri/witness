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
	TraceID    string  `json:"traceID,omitempty"`
	Caller     string  `json:"caller,omitempty"`
	Message    string  `json:"message,omitempty"`
}

func RunLogs(ctx context.Context, pool *pgxpool.Pool, r *LogsReq, tr backend.TimeRange, limit int) backend.DataResponse {
	if r == nil {
		r = &LogsReq{}
	}

	clauses := []string{"e.event_date >= $1 AND e.event_date <= $2"}
	args := []any{tr.From, tr.To}

	if len(r.EventTypes) > 0 {
		args = append(args, r.EventTypes)
		clauses = append(clauses, fmt.Sprintf("e.event_type = ANY($%d::int8[])", len(args)))
	} else {
		// log:* and error:* by default — exclude span lifecycle noise.
		clauses = append(clauses, "(e.event_type BETWEEN 10 AND 14 OR e.event_type BETWEEN 100 AND 104 OR e.event_type = 1)")
	}
	if svc := strings.TrimSpace(r.Service); svc != "" {
		args = append(args, svc)
		clauses = append(clauses, fmt.Sprintf("e.service_name = $%d", len(args)))
	}
	if tid := strings.TrimSpace(r.TraceID); tid != "" {
		args = append(args, tid)
		clauses = append(clauses, fmt.Sprintf("e.trace_id = $%d::uuid", len(args)))
	}
	if msg := strings.TrimSpace(r.Message); msg != "" {
		args = append(args, "%"+msg+"%")
		clauses = append(clauses, fmt.Sprintf("e.event_message ILIKE $%d", len(args)))
	}
	if c := strings.TrimSpace(r.Caller); c != "" {
		args = append(args, "%"+c+"%")
		clauses = append(clauses, fmt.Sprintf("e.event_caller ILIKE $%d", len(args)))
	}

	q := fmt.Sprintf(`
SELECT e.event_date, e.event_id, e.event_type, e.event_message, e.event_caller,
       e.service_name, e.trace_id,
       COALESCE(erj.records, '{}'::jsonb)
  FROM witness.events e
  LEFT JOIN witness.event_records_json erj USING (event_id)
 WHERE %s
 ORDER BY e.event_date DESC
 LIMIT %d`, strings.Join(clauses, " AND "), limit)

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
	traceIDs := []string{}
	labels := []json.RawMessage{}

	for rows.Next() {
		var (
			d   time.Time
			id  string
			et  int64
			m   string
			c   string
			svc *string
			tid *string
			rs  []byte
		)
		if err := rows.Scan(&d, &id, &et, &m, &c, &svc, &tid, &rs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		times = append(times, d)
		bodies = append(bodies, m)
		severities = append(severities, severityOf(et))
		ids = append(ids, id)
		callers = append(callers, c)
		serviceNames = append(serviceNames, strOrEmpty(svc))
		traceIDs = append(traceIDs, strOrEmpty(tid))
		labels = append(labels, rs)
	}

	frame := data.NewFrame("logs",
		data.NewField("timestamp", nil, times),
		data.NewField("body", nil, bodies),
		data.NewField("severity", nil, severities),
		data.NewField("eventID", nil, ids),
		data.NewField("caller", nil, callers),
		data.NewField("service", nil, serviceNames),
		data.NewField("traceID", nil, traceIDs),
		data.NewField("labels", nil, labels),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}
	return backend.DataResponse{Frames: data.Frames{frame}}
}
