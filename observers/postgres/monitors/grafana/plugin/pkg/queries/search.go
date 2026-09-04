// Package queries holds Postgres → Grafana data frame builders, one per
// QueryType supported by the witness data source.
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

// Search describes the universal search form. All fields are optional; the
// backend assembles WHERE clauses only from the non-empty ones.
type Search struct {
	SpanID string `json:"spanID,omitempty"`
	// OwnOnly restricts a SpanID match to events that happened *in* that
	// span, excluding everything nested under it.
	OwnOnly bool   `json:"ownOnly,omitempty"`
	EventID string `json:"eventID,omitempty"`
	// RootSpanID narrows to one trace: the component walked from that root.
	RootSpanID string `json:"rootSpanID,omitempty"`
	// TraceID is the pre-v0.31 name of RootSpanID.
	TraceID    string         `json:"traceID,omitempty"`
	Service    string         `json:"service,omitempty"`
	Message    string         `json:"message,omitempty"`
	Caller     string         `json:"caller,omitempty"`
	EventTypes []int64        `json:"eventTypes,omitempty"`
	Records    []RecordFilter `json:"records,omitempty"`
}

func (s *Search) root() string {
	if s.RootSpanID != "" {
		return s.RootSpanID
	}
	return s.TraceID
}

type RecordFilter struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Op    string `json:"op"` // "eq" | "ilike"
}

// RunSearch returns a logs-shaped frame for the universal search panel.
func RunSearch(ctx context.Context, pool *pgxpool.Pool, s *Search, tr backend.TimeRange, limit int) backend.DataResponse {
	if s == nil {
		s = &Search{}
	}
	sb := newSearchBuilder()

	// The trace walk seeds from $1, so it must be added before anything else.
	prefix := ""
	if root := strings.TrimSpace(s.root()); root != "" {
		sb.seed([]string{root})
		sb.add(`e.event_id IN (SELECT sp.event_id
                  FROM trace_spans ts
                  JOIN witness.spans sp ON sp.span_id = ts.span_id AND sp.span_flags & 1 <> 0)`)
		prefix = traceWalkCTE
	}

	// event_date is `timestamp` without a zone; the observer writes UTC.
	sb.add("e.event_date >= $%d AND e.event_date <= $%d", tr.From.UTC(), tr.To.UTC())

	if s.EventID != "" {
		sb.add("e.event_id = $%d::uuid", s.EventID)
	}
	if s.SpanID != "" {
		if s.OwnOnly {
			sb.add(`e.event_id IN (SELECT event_id FROM witness.spans
			       WHERE span_id = $%d::uuid AND span_flags & 1 <> 0)`, s.SpanID)
		} else {
			sb.add("e.event_id IN (SELECT event_id FROM witness.spans WHERE span_id = $%d::uuid)", s.SpanID)
		}
	}
	if s.Service != "" {
		sb.add("ei.service_name = $%d", s.Service)
	}
	if s.Caller != "" {
		sb.add("e.event_caller ILIKE $%d", "%"+s.Caller+"%")
	}
	if msg := strings.TrimSpace(s.Message); msg != "" {
		if len(msg) < 3 {
			sb.add("e.event_message ILIKE $%d", "%"+msg+"%")
		} else {
			sb.add("to_tsvector('simple', e.event_message) @@ plainto_tsquery('simple', $%d)", msg)
		}
	}
	if len(s.EventTypes) > 0 {
		sb.add("e.event_type = ANY($%d::int8[])", s.EventTypes)
	}
	for _, r := range s.Records {
		switch r.Op {
		case "ilike":
			sb.add(`e.event_id IN (SELECT event_id FROM witness.records
			       WHERE record_key = $%d AND record_value ILIKE $%d)`, r.Key, "%"+r.Value+"%")
		default:
			sb.add(`e.event_id IN (SELECT event_id FROM witness.records
			       WHERE record_key = $%d AND record_value = $%d)`, r.Key, r.Value)
		}
	}

	q := fmt.Sprintf(`%s
SELECT e.event_date, e.event_id, e.event_type, etn.event_type_name,
       e.event_message, e.event_caller,
       ei.service_name, ei.instance_span_id, own.span_id,
       COALESCE(erj.records, '{}'::jsonb)
  FROM witness.events e
  LEFT JOIN witness.event_type_names   etn USING (event_type)
  LEFT JOIN witness.event_records_json erj USING (event_id)
  LEFT JOIN witness.event_instances    ei  ON ei.event_id = e.event_id
  LEFT JOIN witness.spans              own ON own.event_id = e.event_id AND own.span_flags & 1 <> 0
 WHERE %s
 ORDER BY e.event_date DESC
 LIMIT %d`, prefix, sb.where(), limit)

	rows, err := pool.Query(ctx, q, sb.args...)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "search query: "+err.Error())
	}
	defer rows.Close()

	times := []time.Time{}
	bodies := []string{}
	severities := []string{}
	eventIDs := []string{}
	typeNames := []string{}
	callers := []string{}
	serviceNames := []string{}
	instanceIDs := []string{}
	spanIDs := []string{}
	labels := []json.RawMessage{}

	for rows.Next() {
		var (
			d    time.Time
			id   string
			et   int64
			tn   *string
			m    string
			c    string
			svc  *string
			inst *string
			span *string
			rs   []byte
		)
		if err := rows.Scan(&d, &id, &et, &tn, &m, &c, &svc, &inst, &span, &rs); err != nil {
			return backend.ErrDataResponse(backend.StatusInternal, "scan: "+err.Error())
		}
		times = append(times, d)
		bodies = append(bodies, m)
		severities = append(severities, severityOf(et))
		eventIDs = append(eventIDs, id)
		typeNames = append(typeNames, strOrEmpty(tn))
		callers = append(callers, c)
		serviceNames = append(serviceNames, strOrEmpty(svc))
		instanceIDs = append(instanceIDs, strOrEmpty(inst))
		spanIDs = append(spanIDs, strOrEmpty(span))
		labels = append(labels, rs)
	}
	if rows.Err() != nil {
		return backend.ErrDataResponse(backend.StatusInternal, "rows: "+rows.Err().Error())
	}

	frame := data.NewFrame("events",
		data.NewField("timestamp", nil, times),
		data.NewField("body", nil, bodies),
		data.NewField("severity", nil, severities),
		data.NewField("eventID", nil, eventIDs),
		data.NewField("eventType", nil, typeNames),
		data.NewField("caller", nil, callers),
		data.NewField("service", nil, serviceNames),
		data.NewField("instanceSpanID", nil, instanceIDs),
		data.NewField("spanID", nil, spanIDs),
		data.NewField("labels", nil, labels),
	)
	frame.Meta = &data.FrameMeta{PreferredVisualization: data.VisTypeLogs}

	return backend.DataResponse{Frames: data.Frames{frame}}
}

// severityOf maps a witness event_type to a Grafana log severity tag.
// Codes follow witness/events.go: log:* in 10..14 / -; error:* 100..104;
// span lifecycle 20..29.
func severityOf(t int64) string {
	switch {
	case t == 14:
		return "critical"
	case t == 13 || (t >= 100 && t <= 104):
		return "error"
	case t == 12:
		return "warning"
	case t == 10:
		return "debug"
	case t == 11 || t == 1:
		return "info"
	case t >= 20 && t <= 29:
		return "info"
	case t >= -29 && t <= -20:
		return "info"
	default:
		return "info"
	}
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// searchBuilder accumulates WHERE clauses with positional placeholders. Each
// add() call substitutes $%d with the next argument indices, in order.
type searchBuilder struct {
	clauses []string
	args    []any
}

func newSearchBuilder() *searchBuilder { return &searchBuilder{} }

// seed registers an argument that is consumed by a CTE rather than by a WHERE
// clause — the trace walk's root array, which the query text references as
// $1. It must be called before any add(), so it lands first.
func (b *searchBuilder) seed(arg any) {
	if len(b.args) != 0 {
		panic("searchBuilder.seed must be called before any add()")
	}
	b.args = append(b.args, arg)
}

func (b *searchBuilder) add(tmpl string, args ...any) {
	// Count %d in tmpl, assign args sequentially.
	placeholders := strings.Count(tmpl, "$%d")
	if placeholders != len(args) {
		panic(fmt.Sprintf("searchBuilder.add: %d placeholders vs %d args in %q", placeholders, len(args), tmpl))
	}
	parts := make([]any, placeholders)
	for i := range parts {
		b.args = append(b.args, args[i])
		parts[i] = len(b.args)
	}
	b.clauses = append(b.clauses, fmt.Sprintf(tmpl, parts...))
}

func (b *searchBuilder) where() string {
	if len(b.clauses) == 0 {
		return "TRUE"
	}
	return strings.Join(b.clauses, " AND ")
}
