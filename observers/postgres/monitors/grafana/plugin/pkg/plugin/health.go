package plugin

import (
	"context"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	var one int
	if err := d.pool.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Postgres unreachable: " + err.Error(),
		}, nil
	}
	if err := d.pool.QueryRow(ctx, "SELECT 1 FROM witness.events LIMIT 1").Scan(&one); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Connected, but witness schema not found. Apply migration.up.sql and migration_v2.up.sql.",
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Witness schema reachable.",
	}, nil
}
