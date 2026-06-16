// Package pgpool builds a pgxpool from Grafana datasource settings.
package pgpool

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Settings struct {
	URL      string `json:"url"`
	MaxConns int32  `json:"maxConns"`
}

type Secrets struct {
	Password string `json:"password"`
}

// Open parses Grafana DataSourceInstanceSettings, applies secrets, and opens
// a pgxpool. The connection is pinged before returning.
func Open(ctx context.Context, dsis backend.DataSourceInstanceSettings) (*pgxpool.Pool, error) {
	var s Settings
	if len(dsis.JSONData) > 0 {
		if err := json.Unmarshal(dsis.JSONData, &s); err != nil {
			return nil, fmt.Errorf("decode jsonData: %w", err)
		}
	}
	if s.URL == "" {
		return nil, fmt.Errorf("witness datasource: url is required")
	}
	if s.MaxConns <= 0 {
		s.MaxConns = 4
	}

	cfg, err := pgxpool.ParseConfig(s.URL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	cfg.MaxConns = s.MaxConns

	if pwd := dsis.DecryptedSecureJSONData["password"]; pwd != "" {
		cfg.ConnConfig.Password = pwd
	}

	openCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(openCtx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgxpool: %w", err)
	}
	if err := pool.Ping(openCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	return pool, nil
}
