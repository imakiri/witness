package queries

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TracesReq drives the L1 panel: recent traces in the selected time
// range, optionally filtered by participating service or substring match
// on the originating instance name.
type TracesReq struct {
	Service string `json:"service,omitempty"`
	Search  string `json:"search,omitempty"`
}

func RunTraces(ctx context.Context, pool *pgxpool.Pool, r *TracesReq, tr backend.TimeRange, limit int) backend.DataResponse {
	if r == nil {
		r = &TracesReq{}
	}

	clauses := []string{
		"e.trace_id IS NOT NULL",
		"e.event_date >= $1 AND e.event_date <= $2",
	}
	args := []any{tr.From, tr.To}
	if svc := strings.TrimSpace(r.Service); svc != "" {
		args = append(args, svc)
		clauses = append(clauses,
			fmt.Sprintf(`e.trace_id IN (SELECT trace_id FROM witness.events
				WHERE service_name = $%d AND trace_id IS NOT NULL)`, len(args)))
	}
	if sub := strings.TrimSpace(r.Search); sub != "" {
		args = append(args, "%"+sub+"%")
		clauses = append(clauses,
			fmt.Sprintf(`e.trace_id IN (SELECT trace_id FROM witness.events
				WHERE event_type = 21 AND event_message ILIKE $%d AND trace_id IS NOT NULL)`, len(args)))
	}

	q := fmt.Sprintf(`
SELECT e.trace_id,
       min(e.event_date)                                                   AS started_at,
       EXTRACT(epoch FROM (max(e.event_date) - min(e.event_date))) * 1000  AS duration_ms,
       count(*)::bigint                                                    AS event_count,
       count(*) FILTER (
           WHERE e.event_type IN (13, 14, 100, 101, 102, 103, 104)
       )::bigint                                                           AS error_count,
       coalesce(
           array_to_string(
               array_agg(DISTINCT e.service_name)
                 FILTER (WHERE e.service_name IS NOT NULL),
               ', '),
           ''
       )                                                                   AS services
  FROM witness.events e
 WHERE %s
 GROUP BY e.trace_id
 ORDER BY started_at DESC
 LIMIT %d`, strings.Join(clauses, " AND "), limit)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "traces query: "+err.Error())
	}
	defer rows.Close()

	traceIDs := []string{}
	startedAt := []time.Time{}
	durations := []float64{}
	eventCounts := []int64{}
	errorCounts := []int64{}
	servicesAgg := []string{}

	for rows.Next() {
		var (
			tid    string
			start  time.Time
			durMs  float64
			ec     int64
			errc   int64
			svcStr string
		)
		if err := rows.Scan(&tid, &start, &durMs, &ec, &errc, &svcStr); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		traceIDs = append(traceIDs, tid)
		startedAt = append(startedAt, start)
		durations = append(durations, durMs)
		eventCounts = append(eventCounts, ec)
		errorCounts = append(errorCounts, errc)
		servicesAgg = append(servicesAgg, svcStr)
	}
	if rows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows: "+rows.Err().Error())
	}

	frame := data.NewFrame("traces",
		data.NewField("trace_id", nil, traceIDs),
		data.NewField("started_at", nil, startedAt),
		data.NewField("duration_ms", nil, durations),
		data.NewField("event_count", nil, eventCounts),
		data.NewField("error_count", nil, errorCounts),
		data.NewField("services", nil, servicesAgg),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}

	return backend.DataResponse{Frames: data.Frames{frame}}
}
