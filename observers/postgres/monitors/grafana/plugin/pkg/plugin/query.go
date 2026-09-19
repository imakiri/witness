package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"

	"github.com/imakiri/witness/observers/postgres/monitors/grafana/plugin/pkg/queries"
)

// QueryModel is the JSON sent by the frontend QueryEditor for every refId.
type QueryModel struct {
	QueryType  string              `json:"queryType"` // search | trace | logs | table | traces | service-map | event-trail
	Search     *queries.Search     `json:"search,omitempty"`
	Trace      *queries.Trace      `json:"trace,omitempty"`
	Logs       *queries.LogsReq    `json:"logs,omitempty"`
	Table      *queries.TableReq   `json:"table,omitempty"`
	Traces     *queries.TracesReq  `json:"traces,omitempty"`
	ServiceMap *queries.ServiceMap `json:"serviceMap,omitempty"`
	EventTrail *queries.EventTrail `json:"eventTrail,omitempty"`
	Limit      int                 `json:"limit,omitempty"`
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		resp.Responses[q.RefID] = d.runOne(ctx, q)
	}
	return resp, nil
}

func (d *Datasource) runOne(ctx context.Context, q backend.DataQuery) backend.DataResponse {
	var model QueryModel
	if len(q.JSON) > 0 {
		if err := json.Unmarshal(q.JSON, &model); err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, "decode query: "+err.Error())
		}
	}
	if model.Limit <= 0 || model.Limit > 10000 {
		model.Limit = 500
	}

	switch model.QueryType {
	case "trace":
		return queries.RunTrace(ctx, d.pool, model.Trace)
	case "logs":
		return queries.RunLogs(ctx, d.pool, model.Logs, q.TimeRange, model.Limit)
	case "table":
		return queries.RunTable(ctx, d.pool, model.Table, q.TimeRange, model.Limit)
	case "traces":
		return queries.RunTraces(ctx, d.pool, model.Traces, q.TimeRange, model.Limit)
	case "event-trail":
		return queries.RunEventTrail(ctx, d.pool, model.EventTrail, model.Limit)
	case "service-map":
		return queries.RunServiceMap(ctx, d.pool, model.ServiceMap)
	case "search", "":
		return queries.RunSearch(ctx, d.pool, model.Search, q.TimeRange, model.Limit)
	default:
		return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("unknown queryType %q", model.QueryType))
	}
}
