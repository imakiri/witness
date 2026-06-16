package queries

import (
	"context"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ServiceMap renders the inter-service call graph for one trace as a
// Grafana NodeGraph (two frames: nodes + edges). Nodes are services that
// participated in the trace, edges are cross-service hops drawn from
// witness.cross_service_edges where both endpoints fall inside the trace.
type ServiceMap struct {
	TraceID string `json:"traceID"`
}

const serviceMapNodesQuery = `
SELECT ts.service_name,
       ts.event_count::float8        AS event_count,
       ts.error_count::float8        AS error_count,
       EXTRACT(epoch FROM (ts.last_event_at - ts.first_event_at)) * 1000 AS duration_ms
  FROM witness.trace_services ts
 WHERE ts.trace_id = $1::uuid
 ORDER BY ts.first_event_at ASC`

const serviceMapEdgesQuery = `
SELECT cse.parent_service_name,
       cse.child_service_name,
       count(*)::float8 AS call_count
  FROM witness.cross_service_edges cse
  JOIN witness.events child  ON child.event_id = cse.child_event_id
 WHERE child.trace_id = $1::uuid
   AND cse.parent_service_name IS NOT NULL
   AND cse.child_service_name  IS NOT NULL
 GROUP BY cse.parent_service_name, cse.child_service_name`

// RunServiceMap is the entrypoint registered by the QueryData router.
func RunServiceMap(ctx context.Context, pool *pgxpool.Pool, sm *ServiceMap) backend.DataResponse {
	if sm == nil || sm.TraceID == "" {
		return backend.ErrDataResponse(backend.StatusBadRequest, "service-map.traceID is required")
	}

	nodesRows, err := pool.Query(ctx, serviceMapNodesQuery, sm.TraceID)
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

	edgeRows, err := pool.Query(ctx, serviceMapEdgesQuery, sm.TraceID)
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
