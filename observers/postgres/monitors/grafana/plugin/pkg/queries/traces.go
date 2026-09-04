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

// TracesReq drives the L1 panel: recent traces in the selected time range,
// optionally filtered by participating service or substring match on the
// root span's name.
type TracesReq struct {
	Service string `json:"service,omitempty"`
	Search  string `json:"search,omitempty"`
}

// rootsQuery lists candidate trace roots. A root is an entry-point span —
// opened directly under an instance and not triggered by an inbound link —
// so there is exactly one per distributed request, on the originating side.
const rootsQuery = `
SELECT tr.root_span_id,
       tr.span_name,
       coalesce(tr.service_name, '')                        AS service_name,
       tr.started_at,
       coalesce(EXTRACT(epoch FROM tr.duration) * 1000, 0)  AS duration_ms
  FROM witness.trace_roots tr
 WHERE tr.started_at >= $1 AND tr.started_at <= $2
   %s
 ORDER BY tr.started_at DESC
 LIMIT %d`

// traceAggQuery walks every listed root's component once and reports what
// the L1 table shows for it.
const traceAggQuery = traceWalkCTE + `
SELECT ts.root,
       count(*)::bigint                                        AS event_count,
       count(*) FILTER (WHERE e.event_type IN (` + errorEventTypes + `))::bigint AS error_count,
       coalesce(
           array_to_string(
               array_agg(DISTINCT ei.service_name)
                 FILTER (WHERE ei.service_name IS NOT NULL),
               ', '),
           ''
       )                                                       AS services
  FROM trace_spans ts
  JOIN witness.spans  s  ON s.span_id  = ts.span_id AND s.span_flags & 1 <> 0
  JOIN witness.events e  ON e.event_id = s.event_id
  LEFT JOIN witness.event_instances ei ON ei.event_id = e.event_id
 GROUP BY ts.root`

type traceAgg struct {
	events   int64
	errors   int64
	services string
}

func RunTraces(ctx context.Context, pool *pgxpool.Pool, r *TracesReq, tr backend.TimeRange, limit int) backend.DataResponse {
	if r == nil {
		r = &TracesReq{}
	}

	// event_date is `timestamp` without a zone and the observer writes UTC;
	// Grafana hands us a time.Time in the browser/server zone. Normalize, or
	// every filter is off by the offset.
	args := []any{tr.From.UTC(), tr.To.UTC()}
	var extra []string
	if svc := strings.TrimSpace(r.Service); svc != "" {
		args = append(args, svc)
		extra = append(extra, fmt.Sprintf("AND tr.service_name = $%d", len(args)))
	}
	if sub := strings.TrimSpace(r.Search); sub != "" {
		args = append(args, "%"+sub+"%")
		extra = append(extra, fmt.Sprintf("AND tr.span_name ILIKE $%d", len(args)))
	}

	rows, err := pool.Query(ctx, fmt.Sprintf(rootsQuery, strings.Join(extra, "\n   "), limit), args...)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "traces query: "+err.Error())
	}
	defer rows.Close()

	rootIDs := []string{}
	names := []string{}
	services := []string{}
	startedAt := []time.Time{}
	durations := []float64{}

	for rows.Next() {
		var (
			id    string
			name  string
			svc   string
			start time.Time
			durMs float64
		)
		if err := rows.Scan(&id, &name, &svc, &start, &durMs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		rootIDs = append(rootIDs, id)
		names = append(names, name)
		services = append(services, svc)
		startedAt = append(startedAt, start)
		durations = append(durations, durMs)
	}
	if rows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows: "+rows.Err().Error())
	}

	aggs := map[string]traceAgg{}
	if len(rootIDs) > 0 {
		aggRows, err := pool.Query(ctx, traceAggQuery, rootIDs)
		if err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "traces aggregate: "+err.Error())
		}
		defer aggRows.Close()
		for aggRows.Next() {
			var (
				root string
				a    traceAgg
			)
			if err := aggRows.Scan(&root, &a.events, &a.errors, &a.services); err != nil {
				return backend.ErrDataResponse(backend.StatusInternal, "scan aggregate: "+err.Error())
			}
			aggs[root] = a
		}
		if aggRows.Err() != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "rows aggregate: "+aggRows.Err().Error())
		}
	}

	eventCounts := make([]int64, len(rootIDs))
	errorCounts := make([]int64, len(rootIDs))
	servicesAgg := make([]string, len(rootIDs))
	for i, id := range rootIDs {
		a := aggs[id]
		eventCounts[i] = a.events
		errorCounts[i] = a.errors
		servicesAgg[i] = a.services
		if servicesAgg[i] == "" {
			servicesAgg[i] = services[i]
		}
	}

	frame := data.NewFrame("traces",
		data.NewField("root_span_id", nil, rootIDs),
		data.NewField("name", nil, names),
		data.NewField("service", nil, services),
		data.NewField("started_at", nil, startedAt),
		data.NewField("duration_ms", nil, durations),
		data.NewField("event_count", nil, eventCounts),
		data.NewField("error_count", nil, errorCounts),
		data.NewField("services", nil, servicesAgg),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeTable}

	return backend.DataResponse{Frames: data.Frames{frame}}
}
