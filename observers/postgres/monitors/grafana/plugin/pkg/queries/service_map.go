package queries

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ServiceMap renders the inter-service call graph for one trace as a
// Grafana NodeGraph (two frames: nodes + edges). Nodes are the instances
// that participated, edges are the links between them — a link_span_id
// referenced from two different instances.
type ServiceMap struct {
	RootSpanID string `json:"rootSpanID"`

	// TraceID is the pre-v0.31 name of the same field.
	TraceID string `json:"traceID,omitempty"`
}

func (sm *ServiceMap) root() string {
	if sm == nil {
		return ""
	}
	if sm.RootSpanID != "" {
		return sm.RootSpanID
	}
	return sm.TraceID
}

const serviceMapNodesQuery = traceWalkCTE + `,
  trace_events AS (
      SELECT e.event_id, e.event_type, e.event_date, ei.service_name
        FROM trace_spans ts
        JOIN witness.spans  s  ON s.span_id  = ts.span_id AND s.span_flags & 1 <> 0
        JOIN witness.events e  ON e.event_id = s.event_id
        LEFT JOIN witness.event_instances ei ON ei.event_id = e.event_id
  )
SELECT coalesce(service_name, '<unknown>')                          AS service_name,
       count(*)::float8                                             AS event_count,
       count(*) FILTER (WHERE event_type IN (` + errorEventTypes + `))::float8 AS error_count,
       EXTRACT(epoch FROM (max(event_date) - min(event_date))) * 1000 AS duration_ms
  FROM trace_events
 GROUP BY service_name
 ORDER BY min(event_date) ASC`

const serviceMapEdgesQuery = traceWalkCTE + `
SELECT le.from_service_name,
       le.to_service_name,
       count(*)::float8 AS call_count
  FROM witness.link_edges le
 WHERE le.from_span_id IN (SELECT span_id FROM trace_spans)
   AND le.from_service_name IS NOT NULL
   AND le.to_service_name   IS NOT NULL
   AND le.from_service_name IS DISTINCT FROM le.to_service_name
 GROUP BY le.from_service_name, le.to_service_name`

// RunServiceMap is the entrypoint registered by the QueryData router.
func RunServiceMap(ctx context.Context, pool *pgxpool.Pool, sm *ServiceMap) backend.DataResponse {
	root := sm.root()
	if root == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "service-map.rootSpanID is required")
	}

	nodesRows, err := pool.Query(ctx, serviceMapNodesQuery, []string{root})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "service-map nodes: "+err.Error())
	}
	defer nodesRows.Close()

	ids := []string{}
	titles := []string{}
	subTitles := []string{}
	mainStats := []string{}
	secondaryStats := []string{}
	for nodesRows.Next() {
		var (
			svc        string
			eventCount float64
			errorCount float64
			durationMs float64
		)
		if err := nodesRows.Scan(&svc, &eventCount, &errorCount, &durationMs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan nodes: "+err.Error())
		}
		ids = append(ids, svc)
		titles = append(titles, svc)
		subTitles = append(subTitles, "service")
		mainStats = append(mainStats, fmt.Sprintf("%.0f events", eventCount))
		if errorCount > 0 {
			secondaryStats = append(secondaryStats, fmt.Sprintf("%.0f errors / %.0f ms", errorCount, durationMs))
		} else {
			secondaryStats = append(secondaryStats, fmt.Sprintf("%.0f ms", durationMs))
		}
	}
	if nodesRows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows nodes: "+nodesRows.Err().Error())
	}

	nodes := data.NewFrame("nodes",
		data.NewField("id", nil, ids),
		data.NewField("title", nil, titles),
		data.NewField("subTitle", nil, subTitles),
		data.NewField("mainStat", nil, mainStats),
		data.NewField("secondaryStat", nil, secondaryStats),
	)
	nodes.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}

	edgeRows, err := pool.Query(ctx, serviceMapEdgesQuery, []string{root})
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "service-map edges: "+err.Error())
	}
	defer edgeRows.Close()

	edgeIDs := []string{}
	sources := []string{}
	targets := []string{}
	edgeMain := []string{}
	for edgeRows.Next() {
		var (
			parent string
			child  string
			calls  float64
		)
		if err := edgeRows.Scan(&parent, &child, &calls); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan edges: "+err.Error())
		}
		edgeIDs = append(edgeIDs, parent+"->"+child)
		sources = append(sources, parent)
		targets = append(targets, child)
		edgeMain = append(edgeMain, fmt.Sprintf("%.0f calls", calls))
	}
	if edgeRows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows edges: "+edgeRows.Err().Error())
	}

	edges := data.NewFrame("edges",
		data.NewField("id", nil, edgeIDs),
		data.NewField("source", nil, sources),
		data.NewField("target", nil, targets),
		data.NewField("mainStat", nil, edgeMain),
	)
	edges.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeNodeGraph}

	return backend.DataResponse{Frames: data.Frames{nodes, edges}}
}
