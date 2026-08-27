// Package plugin wires the Grafana data-source backend: query routing,
// health checks, and resource endpoints.
package plugin

import (
	"context"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/imakiri/witness/observers/postgres/monitors/grafana/plugin/pkg/pgpool"
)

// Datasource implements the four standard Grafana backend handlers.
type Datasource struct {
	pool *pgxpool.Pool
}

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// NewDatasource is wired to datasource.Manage in pkg/main.go.
func NewDatasource(ctx context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	pool, err := pgpool.Open(ctx, settings)
	if err != nil {
		return nil, err
	}
	return &Datasource{pool: pool}, nil
}

func (d *Datasource) Dispose() {
	if d.pool != nil {
		d.pool.Close()
	}
}

// Pool exposes the underlying pgxpool for query handlers.
func (d *Datasource) Pool() *pgxpool.Pool { return d.pool }
