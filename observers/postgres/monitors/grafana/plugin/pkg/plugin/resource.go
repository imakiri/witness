package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

type eventTypeRow struct {
	EventType int64  `json:"event_type"`
	Name      string `json:"name"`
}

type nameRow struct {
	Name string `json:"name"`
}

// CallResource handles HTTP-style requests from the frontend that aren't
// regular data queries — used to populate dropdowns in the QueryEditor
// (event types, services, span names).
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	switch {
	case req.Path == "event-types":
		return d.serveEventTypes(ctx, sender)
	case req.Path == "services":
		return d.serveServices(ctx, sender)
	case req.Path == "operations" || strings.HasPrefix(req.Path, "operations/"):
		// Optional ?service= filters to spans seen inside a single service.
		service := ""
		if i := strings.IndexByte(req.URL, '?'); i >= 0 {
			vals := parseQuery(req.URL[i+1:])
			service = vals["service"]
		}
		return d.serveOperations(ctx, sender, service)
	default:
		return sendJSON(sender, http.StatusNotFound, map[string]string{"error": "unknown path: " + req.Path})
	}
}

func (d *Datasource) serveEventTypes(ctx context.Context, sender backend.CallResourceResponseSender) error {
	rows, err := d.pool.Query(ctx, `SELECT event_type, event_type_name FROM witness.event_type_names ORDER BY event_type`)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	defer rows.Close()
	out := []eventTypeRow{}
	for rows.Next() {
		var r eventTypeRow
		if err := rows.Scan(&r.EventType, &r.Name); err != nil {
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		out = append(out, r)
	}
	return sendJSON(sender, http.StatusOK, out)
}

func (d *Datasource) serveServices(ctx context.Context, sender backend.CallResourceResponseSender) error {
	rows, err := d.pool.Query(ctx, `
		SELECT DISTINCT service_name
		  FROM witness.events
		 WHERE service_name IS NOT NULL
		 ORDER BY service_name`)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	defer rows.Close()
	out := []nameRow{}
	for rows.Next() {
		var r nameRow
		if err := rows.Scan(&r.Name); err != nil {
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		out = append(out, r)
	}
	return sendJSON(sender, http.StatusOK, out)
}

// serveOperations returns distinct span names. When service is non-empty,
// scoped to spans whose start event was emitted by that service.
func (d *Datasource) serveOperations(ctx context.Context, sender backend.CallResourceResponseSender, service string) error {
	q := `
		SELECT DISTINCT span_name
		  FROM witness.span_starts
		 WHERE span_name IS NOT NULL`
	args := []any{}
	if service != "" {
		q += ` AND service_name = $1`
		args = append(args, service)
	}
	q += ` ORDER BY span_name`
	rows, err := d.pool.Query(ctx, q, args...)
	if err != nil {
		return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	defer rows.Close()
	out := []nameRow{}
	for rows.Next() {
		var r nameRow
		if err := rows.Scan(&r.Name); err != nil {
			return sendJSON(sender, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		}
		out = append(out, r)
	}
	return sendJSON(sender, http.StatusOK, out)
}

// parseQuery is a stripped-down query-string parser — we only need single
// key=value pairs, no repeats, no escaping.
func parseQuery(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		if i := strings.IndexByte(part, '='); i >= 0 {
			out[part[:i]] = part[i+1:]
		} else {
			out[part] = ""
		}
	}
	return out
}

func sendJSON(sender backend.CallResourceResponseSender, status int, body any) error {
	raw, _ := json.Marshal(body)
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    raw,
	})
}
