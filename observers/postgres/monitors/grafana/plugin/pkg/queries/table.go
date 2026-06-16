package queries

import (
	"context"
	"fmt"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TableReq surfaces span_pairs as a generic table — useful for the "recent
// root spans" overview and similar list panels.
type TableReq struct {
	OnlyRoots bool   `json:"onlyRoots,omitempty"`
	NameLike  string `json:"nameLike,omitempty"`
}

func RunTable(ctx context.Context, pool *pgxpool.Pool, r *TableReq, tr backend.TimeRange, limit int) backend.DataResponse {
	if r == nil {
		r = &TableReq{}
	}

	args := []any{tr.From, tr.To}
	where := "sp.started_at >= $1 AND sp.started_at <= $2"
	if r.OnlyRoots {
		where += ` AND NOT EXISTS (
		    SELECT 1 FROM witness.span_children sc WHERE sc.child_span_id = sp.span_id
		) AND NOT EXISTS (
		    SELECT 1 FROM witness.cross_service_edges cse WHERE cse.child_root_span_id = sp.span_id
		)`
	}
	if r.NameLike != "" {
		args = append(args, "%"+r.NameLike+"%")
		where += fmt.Sprintf(" AND sp.span_name ILIKE $%d", len(args))
	}

	q := fmt.Sprintf(`
SELECT sp.span_id, sp.span_name, sp.started_at, sp.finished_at, sp.duration,
       sp.start_caller
  FROM witness.span_pairs sp
 WHERE %s
 ORDER BY sp.started_at DESC
 LIMIT %d`, where, limit)

	rows, err := pool.Query(ctx, q, args...)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "table query: "+err.Error())
	}
	defer rows.Close()

	spanIDs := []string{}
	names := []string{}
	startedAt := []time.Time{}
	finishedAt := []*time.Time{}
	durations := []*float64{}
	callers := []string{}

	for rows.Next() {
		var (
			id   string
			n    string
			sa   time.Time
			fa   *time.Time
			d    *time.Duration
			cstr string
		)
		if err := rows.Scan(&id, &n, &sa, &fa, &d, &cstr); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		spanIDs = append(spanIDs, id)
		names = append(names, n)
		startedAt = append(startedAt, sa)
		finishedAt = append(finishedAt, fa)
		if d != nil {
			ms := float64(*d / time.Millisecond)
			durations = append(durations, &ms)
		} else {
			durations = append(durations, nil)
		}
		callers = append(callers, cstr)
	}

	frame := data.NewFrame("spans",
		data.NewField("spanID", nil, spanIDs),
		data.NewField("name", nil, names),
		data.NewField("startedAt", nil, startedAt),
		data.NewField("finishedAt", nil, finishedAt),
		data.NewField("durationMs", nil, durations),
		data.NewField("caller", nil, callers),
	)
	return backend.DataResponse{Frames: data.Frames{frame}}
}
